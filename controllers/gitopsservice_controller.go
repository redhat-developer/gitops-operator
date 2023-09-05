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

package controllers

import (
	"context"
	"fmt"
	"log"
	"os"
	"reflect"
	"strings"

	argoapp "github.com/argoproj-labs/argocd-operator/api/v1alpha1"
	argocommon "github.com/argoproj-labs/argocd-operator/common"
	argocdcontroller "github.com/argoproj-labs/argocd-operator/controllers/argocd"
	argocdutil "github.com/argoproj-labs/argocd-operator/controllers/argoutil"
	"github.com/go-logr/logr"
	version "github.com/hashicorp/go-version"
	routev1 "github.com/openshift/api/route/v1"
	pipelinesv1alpha1 "github.com/redhat-developer/gitops-operator/api/v1alpha1"
	"github.com/redhat-developer/gitops-operator/common"
	argocd "github.com/redhat-developer/gitops-operator/controllers/argocd"
	"github.com/redhat-developer/gitops-operator/controllers/util"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	resourcev1 "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var logs = logf.Log.WithName("controller_gitopsservice")

// defaults must some somewhere else..
var (
	port                            int32  = 8080
	portTLS                         int32  = 8443
	backendImage                    string = "quay.io/redhat-developer/gitops-backend:v0.0.1"
	backendImageEnvName                    = "BACKEND_IMAGE"
	serviceName                            = "cluster"
	insecureEnvVar                         = "INSECURE"
	insecureEnvVarValue                    = "true"
	serviceNamespace                       = "openshift-gitops"
	deprecatedServiceNamespace             = "openshift-pipelines-app-delivery"
	dynamicPluginStartOCPVersionEnv        = "DYNAMIC_PLUGIN_START_OCP_VERSION"
)

const (
	gitopsServicePrefix = "gitops-service-"
)

// SetupWithManager sets up the controller with the Manager.
func (r *ReconcileGitopsService) SetupWithManager(mgr ctrl.Manager) error {
	reqLogger := logs.WithValues()
	reqLogger.Info("Watching GitopsService")

	pred := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			// Ignore updates to CR status in which case metadata.Generation does not change
			return e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration()
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Evaluates to false if the object has been confirmed deleted.
			return !e.DeleteStateUnknown
		},
	}

	gitopsServiceRef := newGitopsService()
	err := r.Client.Create(context.TODO(), gitopsServiceRef)
	if err != nil {
		reqLogger.Error(err, "Failed to create GitOps service instance")
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&pipelinesv1alpha1.GitopsService{}, builder.WithPredicates(pred)).
		Owns(&rbacv1.ClusterRoleBinding{}).
		Owns(&rbacv1.ClusterRole{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&appsv1.Deployment{}, builder.WithPredicates(pred)).
		Owns(&corev1.Service{}, builder.WithPredicates(pred)).
		Owns(&routev1.Route{}, builder.WithPredicates(pred)).
		Complete(r)
}

// blank assignment to verify that ReconcileGitopsService implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileGitopsService{}

// ReconcileGitopsService reconciles a GitopsService object
type ReconcileGitopsService struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	Client client.Client
	Scheme *runtime.Scheme

	// disableDefaultInstall, if true, will ensure that the default ArgoCD instance is not instantiated in the openshift-gitops namespace.
	DisableDefaultInstall bool
}

//+kubebuilder:rbac:groups=pipelines.openshift.io,resources=gitopsservices,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=pipelines.openshift.io,resources=gitopsservices/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=pipelines.openshift.io,resources=gitopsservices/finalizers,verbs=update

//+kubebuilder:rbac:groups=pipelines.openshift.io,resources=*,verbs=create;delete;get;list;patch;update;watch

//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings,verbs=get;list;watch;create;delete;patch;update;deletecollection;escalate;bind
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=*,verbs=get;list;watch;create;delete;patch;update;deletecollection;escalate;bind

//+kubebuilder:rbac:groups="",resources=configmaps;endpoints;events;persistentvolumeclaims;pods;secrets;serviceaccounts;services;services/finalizers,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups="",resources=pods/log,verbs=get
//+kubebuilder:rbac:groups="",resources=namespaces;resourcequotas,verbs=get;list;watch;create;delete;update
//+kubebuilder:rbac:groups="oauth.openshift.io",resources=oauthclients,verbs=get;list;watch;create;delete;patch;update

//+kubebuilder:rbac:groups=apps,resources=deployments;daemonsets;replicasets;statefulsets,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups=apps,resourceNames=gitops-operator,resources=deployments/finalizers,verbs=update

//+kubebuilder:rbac:groups=apps.openshift.io,resources=*,verbs=get;list;watch;create;delete;patch;update

//+kubebuilder:rbac:groups=route.openshift.io,resources=routes;routes/custom-host,verbs=get;list;watch;create;delete;patch;update
//+kubebuilder:rbac:groups=route.openshift.io,resources=*,verbs=get;list;watch;create;delete;patch;update

//+kubebuilder:rbac:groups=config.openshift.io,resources=clusterversions,verbs=get;list;watch

//+kubebuilder:rbac:groups=console.openshift.io,resources=consoleclidownloads,verbs=create;get;list;patch;update;watch
//+kubebuilder:rbac:groups=console.openshift.io,resources=consolelinks,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups=console.openshift.io,resources=consoleplugins,verbs=create;delete;get;list;patch;update;watch

//+kubebuilder:rbac:groups=argoproj.io,resources=argocds;argocds/finalizers;argocds/status;applications;appprojects,verbs=get;list;watch;create;delete;patch;update

//+kubebuilder:rbac:groups=autoscaling,resources=horizontalpodautoscalers,verbs=get;list;watch;create;delete;patch;update
//+kubebuilder:rbac:groups=batch,resources=cronjobs;jobs,verbs=get;list;watch;create;delete;patch;update
//+kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;delete;patch;update

//+kubebuilder:rbac:groups=operators.coreos.com,resources=operatorgroups;subscriptions;clusterserviceversions,verbs=create;get;list;watch

//+kubebuilder:rbac:groups=template.openshift.io,resources=templates;templateinstances;templateconfigs,verbs=get;list;watch;create;delete;patch;update

//+kubebuilder:rbac:groups=argoproj.io,resources=rolloutmanagers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=argoproj.io,resources=rolloutmanagers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=argoproj.io,resources=rolloutmanagers/finalizers,verbs=update
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=configmaps;endpoints;events;pods;namespaces;secrets;serviceaccounts;services;services/finalizers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=podtemplates;deployments;replicasets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=deployments/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=deployments,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=pods/eviction,verbs=create
//+kubebuilder:rbac:groups="",resources=podtemplates,verbs=get;list;watch
//+kubebuilder:rbac:groups="appmesh.k8s.aws",resources=virtualnodes;virtualrouters,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups="appmesh.k8s.aws",resources=virtualservices,verbs=get;list;watch
//+kubebuilder:rbac:groups="argoproj.io",resources=analysisruns;analysisruns/finalizers;experiments;experiments/finalizers,verbs=create;get;list;watch;update;patch;delete;deletecollection
//+kubebuilder:rbac:groups="argoproj.io",resources=analysistemplates,verbs=create;get;list;watch;update;patch;delete;deletecollection
//+kubebuilder:rbac:groups="argoproj.io",resources=clusteranalysistemplates,verbs=create;get;list;watch;update;patch;delete;deletecollection
//+kubebuilder:rbac:groups="argoproj.io",resources=rollouts;rollouts/finalizers;rollouts/status;rollouts/scale,verbs=create;get;list;watch;update;patch;delete;deletecollection
//+kubebuilder:rbac:groups="batch",resources=jobs,verbs=create;get;list;watch;update;patch;delete
//+kubebuilder:rbac:groups="coordination.k8s.io",resources=leases,verbs=create;get;update
//+kubebuilder:rbac:groups="elbv2.k8s.aws",resources=targetgroupbindings,verbs=list;get
//+kubebuilder:rbac:groups="extensions",resources=ingresses,verbs=create;get;list;watch;patch
//+kubebuilder:rbac:groups="getambassador.io",resources=ambassadormappings;mappings,verbs=create;watch;get;update;list;delete
//+kubebuilder:rbac:groups="networking.istio.io",resources=destinationrules;virtualservices,verbs=watch;get;update;patch;list
//+kubebuilder:rbac:groups="networking.k8s.io",resources=ingresses,verbs=create;watch;get;update;patch;list
//+kubebuilder:rbac:groups="split.smi-spec.io",resources=trafficsplits,verbs=create;watch;get;update;patch
//+kubebuilder:rbac:groups="traefik.containo.us",resources=traefikservices,verbs=watch;get;update
//+kubebuilder:rbac:groups="x.getambassador.io",resources=ambassadormappings;mappings,verbs=create;watch;get;update;list;delete

// Reconcile reads that state of the cluster for a GitopsService object and makes changes based on the state read
// and what is in the GitopsService.Spec
func (r *ReconcileGitopsService) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	reqLogger := logs.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling GitopsService")

	// Fetch the GitopsService instance
	instance := &pipelinesv1alpha1.GitopsService{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: serviceName}, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	namespace, err := GetBackendNamespace(r.Client)
	if err != nil {
		return reconcile.Result{}, err
	}

	// Create namespace if it doesn't already exist
	namespaceRef := newNamespace(namespace)
	err = r.Client.Get(ctx, types.NamespacedName{Name: namespace}, &corev1.Namespace{})
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("Creating a new Namespace", "Name", namespace)
			err = r.Client.Create(ctx, namespaceRef)
			if err != nil {
				return reconcile.Result{}, err
			}
		} else {
			return reconcile.Result{}, err
		}
	}

	gitopsserviceNamespacedName := types.NamespacedName{
		Name:      serviceName,
		Namespace: namespace,
	}

	if !r.DisableDefaultInstall {
		// Create/reconcile the default Argo CD instance, unless default install is disabled
		if result, err := r.reconcileDefaultArgoCDInstance(instance, reqLogger); err != nil {
			return result, fmt.Errorf("unable to reconcile default Argo CD instance: %v", err)
		}
	} else {
		// If installation of default Argo CD instance is disabled, make sure it doesn't exist,
		// deleting it if necessary
		if err := r.ensureDefaultArgoCDInstanceDoesntExist(instance, reqLogger); err != nil {
			return reconcile.Result{}, fmt.Errorf("unable to ensure non-existence of default Argo CD instance: %v", err)
		}
	}

	if result, err := r.reconcileBackend(gitopsserviceNamespacedName, instance, reqLogger); err != nil {
		return result, err
	}

	if result, err := r.reconcileCLIServer(instance, request); err != nil {
		return result, err
	}

	dynamicPluginStartOCPVersion := os.Getenv(dynamicPluginStartOCPVersionEnv)
	if dynamicPluginStartOCPVersion == "" {
		dynamicPluginStartOCPVersion = common.DefaultDynamicPluginStartOCPVersion
	}

	OCPVersion, err := util.GetClusterVersion(r.Client)
	if err != nil {
		log.Printf("Unable to get cluster version: %v", err)
		return reconcile.Result{}, nil
	}

	v1, err := version.NewVersion(OCPVersion)
	if err != nil {
		log.Printf("Unable to retrieve current OCP version: %v", err)
		return reconcile.Result{}, nil
	}
	realVersion := v1.Segments()
	realMajorVersion := realVersion[0]
	realMinorVersion := realVersion[1]

	v2, err := version.NewVersion(dynamicPluginStartOCPVersion)
	if err != nil {
		return reconcile.Result{}, nil
	}
	startVersion := v2.Segments()
	startMajorVersion := startVersion[0]
	startMinorVersion := startVersion[1]

	if realMajorVersion < startMajorVersion || (realMajorVersion == startMajorVersion && realMinorVersion < startMinorVersion) {
		// Skip plugin reconciliation if real OCP version is less than dynamic plugin start OCP version
		return reconcile.Result{}, nil
	} else {
		return r.reconcilePlugin(instance, request)
	}
}

func (r *ReconcileGitopsService) ensureDefaultArgoCDInstanceDoesntExist(instance *pipelinesv1alpha1.GitopsService, reqLogger logr.Logger) error {

	defaultArgoCDInstance, err := argocd.NewCR(common.ArgoCDInstanceName, serviceNamespace)
	if err != nil {
		return err
	}

	argocdNS := newNamespace(defaultArgoCDInstance.Namespace)
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: argocdNS.Name}, &corev1.Namespace{})
	if err != nil {

		if errors.IsNotFound(err) {
			// If the namespace doesn't exit, then the instance necessarily doesn't exist, so just return
			return nil
		} else {
			return err
		}
	}

	// Delete the existing Argo CD instance, if it exists
	existingArgoCD := &argoapp.ArgoCD{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: defaultArgoCDInstance.Name, Namespace: defaultArgoCDInstance.Namespace}, existingArgoCD)
	if err == nil {
		// The default Argo CD instance exists, delete it.
		if err := r.Client.Delete(context.TODO(), existingArgoCD); err != nil {
			return err
		}

	} else if !errors.IsNotFound(err) {
		// If an unexpected error occurred (eg not the 'not found' error, which is expected) then just return it
		return err
	}

	return nil
}

func (r *ReconcileGitopsService) reconcileDefaultArgoCDInstance(instance *pipelinesv1alpha1.GitopsService, reqLogger logr.Logger) (reconcile.Result, error) {

	defaultArgoCDInstance, err := argocd.NewCR(common.ArgoCDInstanceName, serviceNamespace)
	if err != nil {
		return reconcile.Result{}, err
	}

	// The operator decides the namespace based on the version of the cluster it is installed in
	// 4.6 Cluster: Backend in openshift-pipelines-app-delivery namespace and argocd in openshift-gitops namespace
	// 4.7 Cluster: Both backend and argocd instance in openshift-gitops namespace
	argocdNS := newNamespace(defaultArgoCDInstance.Namespace)
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: argocdNS.Name}, &corev1.Namespace{})
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("Creating a new Namespace", "Name", argocdNS.Name)
			err = r.Client.Create(context.TODO(), argocdNS)
			if err != nil {
				return reconcile.Result{}, err
			}
		} else {
			return reconcile.Result{}, err
		}
	} else {
		// Delete if resource quota is set on the default ArgoCD instance namespace.
		// Fix for v1.2 - https://github.com/redhat-developer/gitops-operator/issues/206
		resourceQuotaName := argocdNS.Name + "-compute-resources"
		resourceQuotaObj := &corev1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceQuotaName,
				Namespace: argocdNS.Name,
			},
		}
		err = r.Client.Get(context.TODO(), types.NamespacedName{Name: resourceQuotaName, Namespace: argocdNS.Name}, resourceQuotaObj)
		if err != nil {
			if errors.IsNotFound(err) {
				reqLogger.Info("No ResourceQuota set for namespace", "Name", argocdNS.Name)
			}
		} else {
			err = r.Client.Delete(context.TODO(), resourceQuotaObj)
			if err != nil {
				return reconcile.Result{}, err
			}
		}
	}

	// Set GitopsService instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, defaultArgoCDInstance, r.Scheme); err != nil {
		return reconcile.Result{}, err
	}

	//to add infra nodeselector to default argocd pods
	if instance.Spec.RunOnInfra {
		if defaultArgoCDInstance.Spec.NodePlacement == nil {
			defaultArgoCDInstance.Spec.NodePlacement = &argoapp.ArgoCDNodePlacementSpec{
				NodeSelector: common.InfraNodeSelector(),
			}
		} else {
			defaultArgoCDInstance.Spec.NodePlacement.NodeSelector = argocdutil.AppendStringMap(defaultArgoCDInstance.Spec.NodePlacement.NodeSelector, common.InfraNodeSelector())
		}
	}
	if len(instance.Spec.NodeSelector) > 0 {
		if defaultArgoCDInstance.Spec.NodePlacement == nil {
			defaultArgoCDInstance.Spec.NodePlacement = &argoapp.ArgoCDNodePlacementSpec{
				NodeSelector: instance.Spec.NodeSelector,
			}
		} else {
			defaultArgoCDInstance.Spec.NodePlacement.NodeSelector = argocdutil.AppendStringMap(defaultArgoCDInstance.Spec.NodePlacement.NodeSelector, instance.Spec.NodeSelector)
		}
	}
	if len(instance.Spec.Tolerations) > 0 {
		if defaultArgoCDInstance.Spec.NodePlacement == nil {
			defaultArgoCDInstance.Spec.NodePlacement = &argoapp.ArgoCDNodePlacementSpec{
				Tolerations: instance.Spec.Tolerations,
			}
		} else {
			defaultArgoCDInstance.Spec.NodePlacement.Tolerations = instance.Spec.Tolerations
		}
	}

	// Get or create ArgoCD instance in default namespace
	existingArgoCD := &argoapp.ArgoCD{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: defaultArgoCDInstance.Name, Namespace: defaultArgoCDInstance.Namespace}, existingArgoCD)
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("Creating a new ArgoCD instance", "Namespace", defaultArgoCDInstance.Namespace, "Name", defaultArgoCDInstance.Name)
			err = r.Client.Create(context.TODO(), defaultArgoCDInstance)
			if err != nil {
				return reconcile.Result{}, err
			}
		} else {
			return reconcile.Result{}, err
		}
	} else {
		changed := false

		if existingArgoCD.Spec.ApplicationSet != nil {
			if existingArgoCD.Spec.ApplicationSet.Resources == nil {
				existingArgoCD.Spec.ApplicationSet.Resources = defaultArgoCDInstance.Spec.ApplicationSet.Resources
				changed = true
			}
		}

		if existingArgoCD.Spec.Controller.Resources == nil {
			existingArgoCD.Spec.Controller.Resources = defaultArgoCDInstance.Spec.Controller.Resources
			changed = true
		}

		if argocdcontroller.UseDex(existingArgoCD) {
			if existingArgoCD.Spec.SSO != nil && existingArgoCD.Spec.SSO.Provider == argoapp.SSOProviderTypeDex {
				if existingArgoCD.Spec.SSO.Dex != nil {
					if existingArgoCD.Spec.SSO.Dex.Resources == nil {
						existingArgoCD.Spec.SSO.Dex.Resources = defaultArgoCDInstance.Spec.SSO.Dex.Resources
					}
				}
			}
			changed = true
		}

		if existingArgoCD.Spec.Grafana.Resources == nil {
			existingArgoCD.Spec.Grafana.Resources = defaultArgoCDInstance.Spec.Grafana.Resources
			changed = true
		}

		if existingArgoCD.Spec.HA.Resources == nil {
			existingArgoCD.Spec.HA.Resources = defaultArgoCDInstance.Spec.HA.Resources
			changed = true
		}

		if existingArgoCD.Spec.Redis.Resources == nil {
			existingArgoCD.Spec.Redis.Resources = defaultArgoCDInstance.Spec.Redis.Resources
			changed = true
		}

		if existingArgoCD.Spec.Repo.Resources == nil {
			existingArgoCD.Spec.Repo.Resources = defaultArgoCDInstance.Spec.Repo.Resources
			changed = true
		}

		if existingArgoCD.Spec.Server.Resources == nil {
			existingArgoCD.Spec.Server.Resources = defaultArgoCDInstance.Spec.Server.Resources
			changed = true
		}

		if !reflect.DeepEqual(existingArgoCD.Spec.NodePlacement, defaultArgoCDInstance.Spec.NodePlacement) {
			existingArgoCD.Spec.NodePlacement = defaultArgoCDInstance.Spec.NodePlacement
			changed = true
		}

		if changed {
			reqLogger.Info("Reconciling ArgoCD", "Namespace", existingArgoCD.Namespace, "Name", existingArgoCD.Name)
			err = r.Client.Update(context.TODO(), existingArgoCD)
			if err != nil {
				return reconcile.Result{}, err
			}
		}
	}

	return reconcile.Result{}, nil
}

func (r *ReconcileGitopsService) reconcileBackend(gitopsserviceNamespacedName types.NamespacedName, instance *pipelinesv1alpha1.GitopsService,
	reqLogger logr.Logger) (reconcile.Result, error) {

	// Define Service account for backend Service
	{
		serviceAccountObj := newServiceAccount(gitopsserviceNamespacedName)

		// Set GitopsService instance as the owner and controller
		if err := controllerutil.SetControllerReference(instance, serviceAccountObj, r.Scheme); err != nil {
			return reconcile.Result{}, err
		}

		existingServiceAccount := &corev1.ServiceAccount{}
		err := r.Client.Get(context.TODO(), types.NamespacedName{Namespace: serviceAccountObj.Namespace, Name: serviceAccountObj.Name}, existingServiceAccount)
		if err != nil {
			if errors.IsNotFound(err) {
				reqLogger.Info("Creating a new ServiceAccount", "Namespace", serviceAccountObj.Namespace, "Name", serviceAccountObj.Name)
				err = r.Client.Create(context.TODO(), serviceAccountObj)
				if err != nil {
					return reconcile.Result{}, err
				}
			} else {
				return reconcile.Result{}, err
			}
		}
	}

	// Define a new cluster role for backend service
	{
		clusterRoleObj := newClusterRole(gitopsserviceNamespacedName)

		// Set GitopsService instance as the owner and controller
		if err := controllerutil.SetControllerReference(instance, clusterRoleObj, r.Scheme); err != nil {
			return reconcile.Result{}, err
		}

		existingClusterRole := &rbacv1.ClusterRole{}
		err := r.Client.Get(context.TODO(), types.NamespacedName{Name: clusterRoleObj.Name}, existingClusterRole)
		if err != nil {
			if errors.IsNotFound(err) {
				reqLogger.Info("Creating a new Cluster Role", "Name", clusterRoleObj.Name)
				err = r.Client.Create(context.TODO(), clusterRoleObj)
				if err != nil {
					return reconcile.Result{}, err
				}
			} else {
				return reconcile.Result{}, err
			}
		} else if !reflect.DeepEqual(existingClusterRole.Rules, clusterRoleObj.Rules) {
			reqLogger.Info("Reconciling existing Cluster Role", "Name", clusterRoleObj.Name)
			existingClusterRole.Rules = clusterRoleObj.Rules
			err = r.Client.Update(context.TODO(), existingClusterRole)
			if err != nil {
				return reconcile.Result{}, err
			}
		}
	}

	// Define Cluster Role Binding for backend service
	{
		clusterRoleBinding := newClusterRoleBinding(gitopsserviceNamespacedName)

		// Set GitopsService instance as the owner and controller
		if err := controllerutil.SetControllerReference(instance, clusterRoleBinding, r.Scheme); err != nil {
			return reconcile.Result{}, err
		}

		existingClusterRoleBinding := &rbacv1.ClusterRoleBinding{}
		err := r.Client.Get(context.TODO(), types.NamespacedName{Name: clusterRoleBinding.Name}, existingClusterRoleBinding)
		if err != nil {
			if errors.IsNotFound(err) {
				reqLogger.Info("Creating a new Cluster Role Binding", "Name", clusterRoleBinding.Name)
				err = r.Client.Create(context.TODO(), clusterRoleBinding)
				if err != nil {
					return reconcile.Result{}, err
				}
			} else {
				return reconcile.Result{}, err
			}
		}
	}

	// Define a new backend Deployment
	{
		deploymentObj := newBackendDeployment(gitopsserviceNamespacedName)

		// Add SeccompProfile based on cluster version
		util.AddSeccompProfileForOpenShift(r.Client, &deploymentObj.Spec.Template.Spec)

		deploymentObj.Spec.Template.Spec.NodeSelector = argocommon.DefaultNodeSelector()
		// Set GitopsService instance as the owner and controller
		if err := controllerutil.SetControllerReference(instance, deploymentObj, r.Scheme); err != nil {
			return reconcile.Result{}, err
		}
		if instance.Spec.RunOnInfra {
			deploymentObj.Spec.Template.Spec.NodeSelector[common.InfraNodeLabelSelector] = ""
		}
		if len(instance.Spec.NodeSelector) > 0 {
			deploymentObj.Spec.Template.Spec.NodeSelector = argocdutil.AppendStringMap(deploymentObj.Spec.Template.Spec.NodeSelector, instance.Spec.NodeSelector)
		}
		if len(instance.Spec.Tolerations) > 0 {
			deploymentObj.Spec.Template.Spec.Tolerations = instance.Spec.Tolerations
		}
		// Check if this Deployment already exists
		found := &appsv1.Deployment{}
		if err := r.Client.Get(context.TODO(), types.NamespacedName{Name: deploymentObj.Name, Namespace: deploymentObj.Namespace},
			found); err != nil {

			if errors.IsNotFound(err) {
				reqLogger.Info("Creating a new Deployment", "Namespace", deploymentObj.Namespace, "Name", deploymentObj.Name)
				err = r.Client.Create(context.TODO(), deploymentObj)
				if err != nil {
					return reconcile.Result{}, err
				}
			} else {
				return reconcile.Result{}, err
			}
		} else {
			changed := false
			desiredImage := deploymentObj.Spec.Template.Spec.Containers[0].Image
			if found.Spec.Template.Spec.Containers[0].Image != desiredImage {
				found.Spec.Template.Spec.Containers[0].Image = desiredImage
				changed = true
			}
			if !reflect.DeepEqual(found.Spec.Template.Spec.Containers[0].Env, deploymentObj.Spec.Template.Spec.Containers[0].Env) {
				found.Spec.Template.Spec.Containers[0].Env = deploymentObj.Spec.Template.Spec.Containers[0].Env
				changed = true
			}
			if !reflect.DeepEqual(found.Spec.Template.Spec.Containers[0].Args, deploymentObj.Spec.Template.Spec.Containers[0].Args) {
				found.Spec.Template.Spec.Containers[0].Args = deploymentObj.Spec.Template.Spec.Containers[0].Args
				changed = true
			}
			if !reflect.DeepEqual(found.Spec.Template.Spec.Containers[0].Resources, deploymentObj.Spec.Template.Spec.Containers[0].Resources) {
				found.Spec.Template.Spec.Containers[0].Resources = deploymentObj.Spec.Template.Spec.Containers[0].Resources
				changed = true
			}
			if !reflect.DeepEqual(found.Spec.Template.Spec.Containers[0].SecurityContext, deploymentObj.Spec.Template.Spec.Containers[0].SecurityContext) {
				found.Spec.Template.Spec.Containers[0].SecurityContext = deploymentObj.Spec.Template.Spec.Containers[0].SecurityContext
				changed = true
			}
			if !reflect.DeepEqual(found.Spec.Template.Spec.NodeSelector, deploymentObj.Spec.Template.Spec.NodeSelector) {
				found.Spec.Template.Spec.NodeSelector = deploymentObj.Spec.Template.Spec.NodeSelector
				changed = true
			}
			if !reflect.DeepEqual(found.Spec.Template.Spec.Tolerations, deploymentObj.Spec.Template.Spec.Tolerations) {
				found.Spec.Template.Spec.Tolerations = deploymentObj.Spec.Template.Spec.Tolerations
				changed = true
			}
			if !reflect.DeepEqual(found.Spec.Template.Spec.SecurityContext, deploymentObj.Spec.Template.Spec.SecurityContext) {
				found.Spec.Template.Spec.SecurityContext = deploymentObj.Spec.Template.Spec.SecurityContext
				changed = true
			}

			if changed {
				reqLogger.Info("Reconciling existing backend Deployment", "Namespace", deploymentObj.Namespace, "Name", deploymentObj.Name)
				err = r.Client.Update(context.TODO(), found)
				if err != nil {
					return reconcile.Result{}, err
				}
			}
		}
	}

	// Create backend Service
	{
		serviceRef := newBackendService(gitopsserviceNamespacedName)
		// Set GitopsService instance as the owner and controller
		if err := controllerutil.SetControllerReference(instance, serviceRef, r.Scheme); err != nil {
			return reconcile.Result{}, err
		}

		// Check if this Service already exists
		existingServiceRef := &corev1.Service{}
		if err := r.Client.Get(context.TODO(), types.NamespacedName{Name: serviceRef.Name, Namespace: serviceRef.Namespace},
			existingServiceRef); err != nil {

			if errors.IsNotFound(err) {
				reqLogger.Info("Creating a new Service", "Namespace", serviceRef.Namespace, "Name", serviceRef.Name)
				err = r.Client.Create(context.TODO(), serviceRef)
				if err != nil {
					return reconcile.Result{}, err
				}
			} else {
				return reconcile.Result{}, err
			}
		}
	}

	return reconcile.Result{}, nil
}

// GetBackendNamespace returns the backend service namespace based on OpenShift Cluster version
func GetBackendNamespace(client client.Client) (string, error) {
	version, err := util.GetClusterVersion(client)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(version, "4.6") {
		return deprecatedServiceNamespace, nil
	}
	return serviceNamespace, nil
}

func objectMeta(resourceName string, namespace string, opts ...func(*metav1.ObjectMeta)) metav1.ObjectMeta {
	objectMeta := metav1.ObjectMeta{
		Name:      resourceName,
		Namespace: namespace,
	}
	for _, o := range opts {
		o(&objectMeta)
	}
	return objectMeta
}

func newBackendDeployment(ns types.NamespacedName) *appsv1.Deployment {
	image := os.Getenv(backendImageEnvName)
	if image == "" {
		image = backendImage
	}
	podSpec := corev1.PodSpec{
		Containers: []corev1.Container{
			{
				Name:  ns.Name,
				Image: image,
				Ports: []corev1.ContainerPort{
					{
						Name:          "http",
						Protocol:      corev1.ProtocolTCP,
						ContainerPort: port, // should come from flag
					},
				},
				Env: []corev1.EnvVar{
					{
						Name:  insecureEnvVar,
						Value: insecureEnvVarValue,
					},
				},
				VolumeMounts: []corev1.VolumeMount{
					{
						MountPath: "/etc/gitops/ssl",
						Name:      "backend-ssl",
						ReadOnly:  true,
					},
				},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceMemory: resourcev1.MustParse("128Mi"),
						corev1.ResourceCPU:    resourcev1.MustParse("250m"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceMemory: resourcev1.MustParse("256Mi"),
						corev1.ResourceCPU:    resourcev1.MustParse("500m"),
					},
				},
			},
		},
		Volumes: []corev1.Volume{
			{
				Name: "backend-ssl",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: ns.Name,
					},
				},
			},
		},
		ServiceAccountName: gitopsServicePrefix + ns.Name,
	}

	template := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"app.kubernetes.io/name": ns.Name,
			},
		},
		Spec: podSpec,
	}

	var replicas int32 = 1
	deploymentSpec := appsv1.DeploymentSpec{
		Replicas: &replicas,
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"app.kubernetes.io/name": ns.Name,
			},
		},
		Template: template,
	}

	deploymentObj := &appsv1.Deployment{
		ObjectMeta: objectMeta(ns.Name, ns.Namespace),
		Spec:       deploymentSpec,
	}

	return deploymentObj
}

func newBackendService(ns types.NamespacedName) *corev1.Service {

	spec := corev1.ServiceSpec{
		Ports: []corev1.ServicePort{
			{
				Port:       port,
				Protocol:   corev1.ProtocolTCP,
				TargetPort: intstr.FromInt(int(port)),
			},
		},
		Selector: map[string]string{
			"app.kubernetes.io/name": ns.Name,
		},
	}
	svc := &corev1.Service{
		ObjectMeta: objectMeta(ns.Name, ns.Namespace, func(o *metav1.ObjectMeta) {
			o.Annotations = map[string]string{
				"service.beta.openshift.io/serving-cert-secret-name": ns.Name,
			}
		}),
		Spec: spec,
	}
	return svc
}

func newNamespace(ns string) *corev1.Namespace {
	objectMeta := metav1.ObjectMeta{
		Name: ns,
		Labels: map[string]string{
			// Enable full-fledged support for integration with cluster monitoring.
			"openshift.io/cluster-monitoring": "true",
		},
	}
	return &corev1.Namespace{
		ObjectMeta: objectMeta,
	}
}

func newGitopsService() *pipelinesv1alpha1.GitopsService {
	return &pipelinesv1alpha1.GitopsService{
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceName,
		},
		Spec: pipelinesv1alpha1.GitopsServiceSpec{},
	}
}

func newServiceAccount(meta types.NamespacedName) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gitopsServicePrefix + meta.Name,
			Namespace: meta.Namespace,
		},
	}
}

func newClusterRole(meta types.NamespacedName) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: gitopsServicePrefix + meta.Name,
		},
		Rules: policyRuleForBackendServiceClusterRole(),
	}
}

func newClusterRoleBinding(meta types.NamespacedName) *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: gitopsServicePrefix + meta.Name,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      gitopsServicePrefix + meta.Name,
				Namespace: meta.Namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     gitopsServicePrefix + meta.Name,
		},
	}
}

func policyRuleForBackendServiceClusterRole() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{
				"argoproj.io",
			},
			Resources: []string{
				"applications",
			},
			Verbs: []string{
				"get",
				"list",
				"watch",
			},
		},
		{
			APIGroups: []string{
				"",
			},
			Resources: []string{
				"secrets",
			},
			Verbs: []string{
				"get",
				"list",
				"watch",
			},
		},
	}
}
