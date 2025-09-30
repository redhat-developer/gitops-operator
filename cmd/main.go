/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"reflect"
	"strings"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	"go.uber.org/zap/zapcore"
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	rolloutManagerApi "github.com/argoproj-labs/argo-rollouts-manager/api/v1alpha1"
	rolloutManagerProvisioner "github.com/argoproj-labs/argo-rollouts-manager/controllers"
	argov1alpha1api "github.com/argoproj-labs/argocd-operator/api/v1alpha1"
	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	argocdcommon "github.com/argoproj-labs/argocd-operator/common"
	argocdprovisioner "github.com/argoproj-labs/argocd-operator/controllers/argocd"
	notificationsprovisioner "github.com/argoproj-labs/argocd-operator/controllers/notificationsconfiguration"
	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	appsv1 "github.com/openshift/api/apps/v1"
	configv1 "github.com/openshift/api/config/v1"
	console "github.com/openshift/api/console/v1"
	oauthv1 "github.com/openshift/api/oauth/v1"
	routev1 "github.com/openshift/api/route/v1"
	templatev1 "github.com/openshift/api/template/v1"
	operatorsv1 "github.com/operator-framework/api/pkg/operators/v1"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	crdv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/argoproj-labs/argocd-operator/controllers/argocd"

	pipelinesv1alpha1 "github.com/redhat-developer/gitops-operator/api/v1alpha1"
	"github.com/redhat-developer/gitops-operator/common"
	"github.com/redhat-developer/gitops-operator/controllers"
	"github.com/redhat-developer/gitops-operator/controllers/argocd/openshift"
	"github.com/redhat-developer/gitops-operator/controllers/util"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	controllerconfig "sigs.k8s.io/controller-runtime/pkg/config"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(pipelinesv1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string

	var enableHTTP2 = false
	var skipControllerNameValidation = true

	var labelSelectorFlag string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.StringVar(&labelSelectorFlag, "label-selector", common.StringFromEnv(argocdcommon.ArgoCDLabelSelectorKey, argocdcommon.ArgoCDDefaultLabelSelector), "The label selector is used to map to a subset of ArgoCD instances to reconcile")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&enableHTTP2, "enable-http2", enableHTTP2, "If HTTP/2 should be enabled for the metrics and webhook servers.")

	//Configure log level
	logLevelStr := strings.ToLower(os.Getenv("LOG_LEVEL"))
	logLevel := zapcore.InfoLevel
	switch logLevelStr {
	case "debug":
		logLevel = zapcore.DebugLevel
	case "info":
		logLevel = zapcore.InfoLevel
	case "warn":
		logLevel = zapcore.WarnLevel
	case "error":
		logLevel = zapcore.ErrorLevel
	case "panic":
		logLevel = zapcore.PanicLevel
	case "fatal":
		logLevel = zapcore.FatalLevel
	}

	opts := zap.Options{
		Development: true,
		Level:       logLevel,
		TimeEncoder: zapcore.RFC3339TimeEncoder,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	if err := util.InspectCluster(); err != nil {
		setupLog.Info("unable to inspect cluster")
	}

	disableHTTP2 := func(c *tls.Config) {
		if enableHTTP2 {
			return
		}
		c.NextProtos = []string{"http/1.1"}
	}
	webhookServerOptions := webhook.Options{
		TLSOpts: []func(config *tls.Config){disableHTTP2},
		Port:    9443,
	}
	webhookServer := webhook.NewServer(webhookServerOptions)

	metricsServerOptions := metricsserver.Options{
		BindAddress: metricsAddr,
		TLSOpts:     []func(*tls.Config){disableHTTP2},
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "2b63967d.openshift.io",
		// With controller-runtime v0.19.0, unique controller name validation is
		// enforced. The operator may fail to start due to this as we don't have unique
		// names. Use SkipNameValidation to ingnore the uniquness check and prevent panic.
		Controller: controllerconfig.Controller{
			SkipNameValidation: &skipControllerNameValidation,
		},
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	registerComponentOrExit(mgr, console.AddToScheme)
	registerComponentOrExit(mgr, routev1.AddToScheme) // Adding the routev1 api
	registerComponentOrExit(mgr, operatorsv1.AddToScheme)
	registerComponentOrExit(mgr, operatorsv1alpha1.AddToScheme)
	registerComponentOrExit(mgr, argov1alpha1api.AddToScheme)
	registerComponentOrExit(mgr, argov1beta1api.AddToScheme)
	registerComponentOrExit(mgr, configv1.AddToScheme)
	registerComponentOrExit(mgr, monitoringv1.AddToScheme)
	registerComponentOrExit(mgr, rolloutManagerApi.AddToScheme)
	registerComponentOrExit(mgr, templatev1.AddToScheme)
	registerComponentOrExit(mgr, appsv1.AddToScheme)
	registerComponentOrExit(mgr, oauthv1.AddToScheme)
	registerComponentOrExit(mgr, crdv1.AddToScheme)

	// Start webhook only if ENABLE_CONVERSION_WEBHOOK is set
	if strings.EqualFold(os.Getenv("ENABLE_CONVERSION_WEBHOOK"), "true") {
		if err = (&argov1beta1api.ArgoCD{}).SetupWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "ArgoCD")
			os.Exit(1)
		}
	}

	if err = (&controllers.ReconcileGitopsService{
		Client:                mgr.GetClient(),
		Scheme:                mgr.GetScheme(),
		DisableDefaultInstall: strings.ToLower(os.Getenv(common.DisableDefaultInstallEnvVar)) == "true",
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "GitopsService")
		os.Exit(1)
	}

	if err = (&controllers.ReconcileArgoCDRoute{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Argo CD route")
		os.Exit(1)
	}

	if err = (&controllers.ArgoCDMetricsReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Argo CD metrics")
		os.Exit(1)
	}
	// Check the label selector format eg. "foo=bar"
	if _, err := labels.Parse(labelSelectorFlag); err != nil {
		setupLog.Error(err, "error parsing the labelSelector '%s'.", labelSelectorFlag)
		os.Exit(1)
	}
	setupLog.Info(fmt.Sprintf("Watching label-selector \"%s\"", labelSelectorFlag))

	if err = (&argocdprovisioner.ReconcileArgoCD{
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		LabelSelector: labelSelectorFlag,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Argo CD")
		os.Exit(1)
	}

	isNamespaceScoped := strings.ToLower(os.Getenv(rolloutManagerProvisioner.NamespaceScopedArgoRolloutsController)) == "true"

	if isNamespaceScoped {
		setupLog.Info("Argo Rollouts manager running in namespaced-scoped mode")
	} else {
		setupLog.Info("Argo Rollouts manager running in cluster-scoped mode")
	}

	if err = (&rolloutManagerProvisioner.RolloutManagerReconciler{
		Client:                                mgr.GetClient(),
		Scheme:                                mgr.GetScheme(),
		OpenShiftRoutePluginLocation:          getArgoRolloutsOpenshiftRouteTrafficManagerPath(),
		NamespaceScopedArgoRolloutsController: isNamespaceScoped,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Argo Rollouts")
		os.Exit(1)
	}

	if err = (&notificationsprovisioner.NotificationsConfigurationReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Notifications Configuration")
		os.Exit(1)
	}

	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	argocd.Register(openshift.ReconcilerHook)

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

// getArgoRolloutsOpenshiftRouteTrafficManagerPath returns the location of the Argo Rollouts OpenShift Route Traffic Management plugin. The location of the plugin is different based on whether we are running as part of OpenShift GitOps, or gitops-operator.
func getArgoRolloutsOpenshiftRouteTrafficManagerPath() string {

	// First, allow the user to change the plugin location via env var
	openShiftRoutePluginLocation := os.Getenv("OPENSHIFT_ROUTE_PLUGIN_LOCATION")
	if openShiftRoutePluginLocation != "" {
		return openShiftRoutePluginLocation
	}

	// Next, if we are running on an image built by CPaaS, then we can assume that the openshift-route-plugin has been installed to '/plugins/rollouts-trafficrouter-openshift/openshift-route-plugin' within the 'registry.redhat.io/openshift-gitops-1/argo-rollouts-rhel*' container image.
	// However, if we are not running an image built by CPaaS, for example, because we are running the gitops-operator upstream E2E tests, then we default to retrieving Route plugin from the upstream dependency: https://github.com/argoproj-labs/argo-rollouts-manager/blob/1f89f7a53b712f83c7051503d571ae2758fed9d6/main.go#L53

	argoRolloutsImage := os.Getenv("ARGO_ROLLOUTS_IMAGE")
	if argoRolloutsImage != "" && strings.HasPrefix(argoRolloutsImage, "registry.redhat.io/openshift-gitops") {
		openShiftRoutePluginLocation = "file:/plugins/rollouts-trafficrouter-openshift/openshift-route-plugin"
		return openShiftRoutePluginLocation
	}

	// Otherwise, if ARGO_ROLLOUTS_IMAGE is not set, or is not an OpenShift GitOps image, then we return the default from the dependency.
	return rolloutManagerProvisioner.DefaultOpenShiftRoutePluginURL

}

func registerComponentOrExit(mgr manager.Manager, f func(*k8sruntime.Scheme) error) {
	// Setup Scheme for all resources
	if err := f(mgr.GetScheme()); err != nil {
		setupLog.Error(err, "")
		os.Exit(1)
	}
	setupLog.Info(fmt.Sprintf("Component registered: %v", reflect.ValueOf(f)))
}
