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
	"flag"
	"fmt"
	"os"
	"reflect"
	"strings"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	"go.uber.org/zap/zapcore"
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	rolloutManagerApi "github.com/argoproj-labs/argo-rollouts-manager/api/v1alpha1"
	rolloutManagerProvisioner "github.com/argoproj-labs/argo-rollouts-manager/controllers"
	argov1alpha1api "github.com/argoproj-labs/argocd-operator/api/v1alpha1"
	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	argocdprovisioner "github.com/argoproj-labs/argocd-operator/controllers/argocd"
	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	appsv1 "github.com/openshift/api/apps/v1"
	configv1 "github.com/openshift/api/config/v1"
	console "github.com/openshift/api/console/v1"
	oauthv1 "github.com/openshift/api/oauth/v1"
	routev1 "github.com/openshift/api/route/v1"
	templatev1 "github.com/openshift/api/template/v1"
	operatorsv1 "github.com/operator-framework/api/pkg/operators/v1"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/argoproj-labs/argocd-operator/controllers/argocd"

	pipelinesv1alpha1 "github.com/redhat-developer/gitops-operator/api/v1alpha1"
	"github.com/redhat-developer/gitops-operator/common"
	"github.com/redhat-developer/gitops-operator/controllers"
	"github.com/redhat-developer/gitops-operator/controllers/argocd/openshift"
	"github.com/redhat-developer/gitops-operator/controllers/util"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
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
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	opts := zap.Options{
		Development: true,
		TimeEncoder: zapcore.RFC3339TimeEncoder,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	if err := util.InspectCluster(); err != nil {
		setupLog.Info("unable to inspect cluster")
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "2b63967d.openshift.io",
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

	if err = (&argocdprovisioner.ReconcileArgoCD{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Argo CD")
		os.Exit(1)
	}

	if err = (&rolloutManagerProvisioner.RolloutManagerReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Argo Rollouts")
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

func registerComponentOrExit(mgr manager.Manager, f func(*k8sruntime.Scheme) error) {
	// Setup Scheme for all resources
	if err := f(mgr.GetScheme()); err != nil {
		setupLog.Error(err, "")
		os.Exit(1)
	}
	setupLog.Info(fmt.Sprintf("Component registered: %v", reflect.ValueOf(f)))
}
