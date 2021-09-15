package gitopsservice

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"

	argoapp "github.com/argoproj-labs/argocd-operator/pkg/apis/argoproj/v1alpha1"
	"github.com/go-logr/logr"
	routev1 "github.com/openshift/api/route/v1"

	"github.com/redhat-developer/gitops-operator/common"
	pipelinesv1alpha1 "github.com/redhat-developer/gitops-operator/pkg/apis/pipelines/v1alpha1"
	argocd "github.com/redhat-developer/gitops-operator/pkg/controller/argocd"
	"github.com/redhat-developer/gitops-operator/pkg/controller/util"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	resourcev1 "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_gitopsservice")

// defaults must some somewhere else..
var (
	port                       int32  = 8080
	portTLS                    int32  = 8443
	backendImage               string = "quay.io/redhat-developer/gitops-backend:v0.0.1"
	backendImageEnvName               = "BACKEND_IMAGE"
	serviceName                       = "cluster"
	insecureEnvVar                    = "INSECURE"
	insecureEnvVarValue               = "true"
	serviceNamespace                  = "openshift-gitops"
	deprecatedServiceNamespace        = "openshift-pipelines-app-delivery"
)

const (
	gitopsServicePrefix = "gitops-service-"
)

// Add creates a new GitopsService Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {

	disableDefaultInstall := strings.ToLower(os.Getenv(common.DisableDefaultInstallEnvVar)) == "true"

	return &ReconcileGitopsService{client: mgr.GetClient(), scheme: mgr.GetScheme(), disableDefaultInstall: disableDefaultInstall}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {

	reqLogger := log.WithValues()
	reqLogger.Info("Watching GitopsService")

	pred := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			// Ignore updates to CR status in which case metadata.Generation does not change
			return e.MetaOld.GetGeneration() != e.MetaNew.GetGeneration()
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Evaluates to false if the object has been confirmed deleted.
			return !e.DeleteStateUnknown
		},
	}

	// Create a new controller
	c, err := controller.New("gitopsservice-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource GitopsService
	err = c.Watch(&source.Kind{Type: &pipelinesv1alpha1.GitopsService{}}, &handler.EnqueueRequestForObject{}, pred)
	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: &routev1.Route{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &pipelinesv1alpha1.GitopsService{},
	}, pred)

	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: &corev1.Service{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &pipelinesv1alpha1.GitopsService{},
	}, pred)
	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: &appsv1.Deployment{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &pipelinesv1alpha1.GitopsService{},
	}, pred)
	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: &corev1.ConfigMap{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &pipelinesv1alpha1.GitopsService{},
	})
	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: &corev1.ServiceAccount{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &pipelinesv1alpha1.GitopsService{},
	})
	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: &rbacv1.ClusterRole{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &pipelinesv1alpha1.GitopsService{},
	})
	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: &rbacv1.ClusterRoleBinding{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &pipelinesv1alpha1.GitopsService{},
	})
	if err != nil {
		return err
	}

	client := mgr.GetClient()

	gitopsServiceRef := newGitopsService()
	err = client.Create(context.TODO(), gitopsServiceRef)
	if err != nil {
		reqLogger.Error(err, "Failed to create GitOps service instance")
	}
	return nil
}

// blank assignment to verify that ReconcileGitopsService implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileGitopsService{}

// ReconcileGitopsService reconciles a GitopsService object
type ReconcileGitopsService struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme

	// disableDefaultInstall, if true, will ensure that the default ArgoCD instance is not instantiated in the openshift-gitops namespace.
	// see 'newReconciler'
	disableDefaultInstall bool
}

// Reconcile reads that state of the cluster for a GitopsService object and makes changes based on the state read
// and what is in the GitopsService.Spec
func (r *ReconcileGitopsService) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling GitopsService")

	// Fetch the GitopsService instance
	instance := &pipelinesv1alpha1.GitopsService{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: serviceName}, instance)
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

	namespace, err := GetBackendNamespace(r.client)
	if err != nil {
		return reconcile.Result{}, err
	}

	// Create namespace if it doesn't already exist
	namespaceRef := newNamespace(namespace)
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: namespace}, &corev1.Namespace{})
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("Creating a new Namespace", "Name", namespace)
			err = r.client.Create(context.TODO(), namespaceRef)
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

	if !r.disableDefaultInstall {
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

	return r.reconcileCLIServer(instance, request)
}

func (r *ReconcileGitopsService) ensureDefaultArgoCDInstanceDoesntExist(instance *pipelinesv1alpha1.GitopsService, reqLogger logr.Logger) error {

	defaultArgoCDInstance, err := argocd.NewCR(common.ArgoCDInstanceName, serviceNamespace)
	if err != nil {
		return err
	}

	argocdNS := newNamespace(defaultArgoCDInstance.Namespace)
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: argocdNS.Name}, &corev1.Namespace{})
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
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: defaultArgoCDInstance.Name, Namespace: defaultArgoCDInstance.Namespace}, existingArgoCD)
	if err == nil {
		// The Argo CD instance exists, so update, then delete it

		reqLogger.Info("Patching ArgoCD finalizer for " + existingArgoCD.Name)

		// Remove the finalizer, so it can be deleted
		existingArgoCD.Finalizers = []string{}
		if err := r.client.Update(context.TODO(), existingArgoCD); err != nil {
			return err
		}

		reqLogger.Info("Deleting ArgoCD finalizer for " + existingArgoCD.Name)

		// Delete the existing ArgoCD instance
		if err := r.client.Delete(context.TODO(), existingArgoCD); err != nil {
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
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: argocdNS.Name}, &corev1.Namespace{})
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("Creating a new Namespace", "Name", argocdNS.Name)
			err = r.client.Create(context.TODO(), argocdNS)
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
		err = r.client.Get(context.TODO(), types.NamespacedName{Name: resourceQuotaName, Namespace: argocdNS.Name}, resourceQuotaObj)
		if err != nil {
			if errors.IsNotFound(err) {
				reqLogger.Info("No ResourceQuota set for namespace", "Name", argocdNS.Name)
			}
		} else {
			err = r.client.Delete(context.TODO(), resourceQuotaObj)
			if err != nil {
				return reconcile.Result{}, err
			}
		}
	}

	// Set GitopsService instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, defaultArgoCDInstance, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	// Get or create ArgoCD instance in default namespace
	existingArgoCD := &argoapp.ArgoCD{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: defaultArgoCDInstance.Name, Namespace: defaultArgoCDInstance.Namespace}, existingArgoCD)
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("Creating a new ArgoCD instance", "Namespace", defaultArgoCDInstance.Namespace, "Name", defaultArgoCDInstance.Name)
			err = r.client.Create(context.TODO(), defaultArgoCDInstance)
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

		if existingArgoCD.Spec.Dex.Resources == nil {
			existingArgoCD.Spec.Dex.Resources = defaultArgoCDInstance.Spec.Dex.Resources
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

		if changed {
			reqLogger.Info("Reconciling ArgoCD", "Namespace", existingArgoCD.Namespace, "Name", existingArgoCD.Name)
			err = r.client.Update(context.TODO(), existingArgoCD)
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
		if err := controllerutil.SetControllerReference(instance, serviceAccountObj, r.scheme); err != nil {
			return reconcile.Result{}, err
		}

		existingServiceAccount := &corev1.ServiceAccount{}
		err := r.client.Get(context.TODO(), types.NamespacedName{Namespace: serviceAccountObj.Namespace, Name: serviceAccountObj.Name}, existingServiceAccount)
		if err != nil {
			if errors.IsNotFound(err) {
				reqLogger.Info("Creating a new ServiceAccount", "Namespace", serviceAccountObj.Namespace, "Name", serviceAccountObj.Name)
				err = r.client.Create(context.TODO(), serviceAccountObj)
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
		if err := controllerutil.SetControllerReference(instance, clusterRoleObj, r.scheme); err != nil {
			return reconcile.Result{}, err
		}

		existingClusterRole := &rbacv1.ClusterRole{}
		err := r.client.Get(context.TODO(), types.NamespacedName{Name: clusterRoleObj.Name}, existingClusterRole)
		if err != nil {
			if errors.IsNotFound(err) {
				reqLogger.Info("Creating a new Cluster Role", "Name", clusterRoleObj.Name)
				err = r.client.Create(context.TODO(), clusterRoleObj)
				if err != nil {
					return reconcile.Result{}, err
				}
			} else {
				return reconcile.Result{}, err
			}
		} else if !reflect.DeepEqual(existingClusterRole.Rules, clusterRoleObj.Rules) {
			reqLogger.Info("Reconciling existing Cluster Role", "Name", clusterRoleObj.Name)
			existingClusterRole.Rules = clusterRoleObj.Rules
			err = r.client.Update(context.TODO(), existingClusterRole)
			if err != nil {
				return reconcile.Result{}, err
			}
		}
	}

	// Define Cluster Role Binding for backend service
	{
		clusterRoleBinding := newClusterRoleBinding(gitopsserviceNamespacedName)

		// Set GitopsService instance as the owner and controller
		if err := controllerutil.SetControllerReference(instance, clusterRoleBinding, r.scheme); err != nil {
			return reconcile.Result{}, err
		}

		existingClusterRoleBinding := &rbacv1.ClusterRoleBinding{}
		err := r.client.Get(context.TODO(), types.NamespacedName{Name: clusterRoleBinding.Name}, existingClusterRoleBinding)
		if err != nil {
			if errors.IsNotFound(err) {
				reqLogger.Info("Creating a new Cluster Role Binding", "Name", clusterRoleBinding.Name)
				err = r.client.Create(context.TODO(), clusterRoleBinding)
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

		// Set GitopsService instance as the owner and controller
		if err := controllerutil.SetControllerReference(instance, deploymentObj, r.scheme); err != nil {
			return reconcile.Result{}, err
		}

		// Check if this Deployment already exists
		found := &appsv1.Deployment{}
		if err := r.client.Get(context.TODO(), types.NamespacedName{Name: deploymentObj.Name, Namespace: deploymentObj.Namespace},
			found); err != nil {

			if errors.IsNotFound(err) {
				reqLogger.Info("Creating a new Deployment", "Namespace", deploymentObj.Namespace, "Name", deploymentObj.Name)
				err = r.client.Create(context.TODO(), deploymentObj)
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

			if changed {
				reqLogger.Info("Reconciling existing backend Deployment", "Namespace", deploymentObj.Namespace, "Name", deploymentObj.Name)
				err = r.client.Update(context.TODO(), found)
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
		if err := controllerutil.SetControllerReference(instance, serviceRef, r.scheme); err != nil {
			return reconcile.Result{}, err
		}

		// Check if this Service already exists
		existingServiceRef := &corev1.Service{}
		if err := r.client.Get(context.TODO(), types.NamespacedName{Name: serviceRef.Name, Namespace: serviceRef.Namespace},
			existingServiceRef); err != nil {

			if errors.IsNotFound(err) {
				reqLogger.Info("Creating a new Service", "Namespace", serviceRef.Namespace, "Name", serviceRef.Name)
				err = r.client.Create(context.TODO(), serviceRef)
				if err != nil {
					return reconcile.Result{}, err
				}
			} else {
				return reconcile.Result{}, err
			}
		}
	}

	// Create backend Route
	{
		routeRef := newBackendRoute(gitopsserviceNamespacedName)
		// Set GitopsService instance as the owner and controller
		if err := controllerutil.SetControllerReference(instance, routeRef, r.scheme); err != nil {
			return reconcile.Result{}, err
		}

		existingRoute := &routev1.Route{}

		if err := r.client.Get(context.TODO(), types.NamespacedName{Name: routeRef.Name, Namespace: routeRef.Namespace},
			existingRoute); err != nil {

			if errors.IsNotFound(err) {
				reqLogger.Info("Creating a new Route", "Namespace", routeRef.Namespace, "Name", routeRef.Name)
				err = r.client.Create(context.TODO(), routeRef)
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

func newBackendRoute(ns types.NamespacedName) *routev1.Route {
	routeSpec := routev1.RouteSpec{
		To: routev1.RouteTargetReference{
			Kind: "Service",
			Name: ns.Name,
		},
		Port: &routev1.RoutePort{
			TargetPort: intstr.IntOrString{IntVal: port},
		},
		TLS: &routev1.TLSConfig{
			Termination:                   routev1.TLSTerminationReencrypt,
			InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyAllow,
		},
	}

	routeObj := &routev1.Route{
		ObjectMeta: objectMeta(ns.Name, ns.Namespace),
		Spec:       routeSpec,
	}

	return routeObj
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
	}
}
