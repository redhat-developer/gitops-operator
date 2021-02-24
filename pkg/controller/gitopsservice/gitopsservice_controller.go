package gitopsservice

import (
	"context"
	"fmt"
	"os"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/yaml"

	"github.com/redhat-developer/gitops-operator/pkg/controller/util"

	argoapp "github.com/argoproj-labs/argocd-operator/pkg/apis/argoproj/v1alpha1"
	keycloakv1alpha1 "github.com/keycloak/keycloak-operator/pkg/apis/keycloak/v1alpha1"
	oauthv1 "github.com/openshift/api/oauth/v1"
	routev1 "github.com/openshift/api/route/v1"
	pipelinesv1alpha1 "github.com/redhat-developer/gitops-operator/pkg/apis/pipelines/v1alpha1"
	"github.com/redhat-developer/gitops-operator/pkg/controller/argocd"
	"github.com/redhat-developer/gitops-operator/pkg/controller/rhsso"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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
	depracatedServiceNamespace        = "openshift-pipelines-app-delivery"
	clusterVersionName                = "version"
	argoClientID                      = "openshift-gitops"
	keycloakRouteName                 = "keycloak"
	keycloakSecretName                = fmt.Sprintf("keycloak-client-secret-%s", rhsso.KeycloakArgoClient)
	argoCDSecretName                  = "argocd-secret"
)

const gitopsIdentifier = "openshift-gitops"

func filterKeycloakRoute(namespace, name string) bool {
	return namespace == serviceNamespace && keycloakRouteName == name
}

func filterOIDCSecrets(namespace, name string) bool {
	return namespace == serviceNamespace && (keycloakSecretName == name || argoCDSecretName == name)
}

// Add creates a new GitopsService Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileGitopsService{client: mgr.GetClient(), scheme: mgr.GetScheme()}
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

	err = c.Watch(&source.Kind{Type: &routev1.Route{}}, &handler.EnqueueRequestForObject{}, argocd.FilterPredicate(argocd.FilterArgoCDRoute))
	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: &routev1.Route{}}, &handler.EnqueueRequestForObject{}, argocd.FilterPredicate(filterKeycloakRoute))
	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: &corev1.Secret{}}, &handler.EnqueueRequestForObject{}, argocd.FilterPredicate(filterOIDCSecrets))
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

	namespaceRef := newNamespace(namespace)
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: namespace}, &corev1.Namespace{})
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new Namespace", "Name", namespace)
		err = r.client.Create(context.TODO(), namespaceRef)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	serviceNamespacedName := types.NamespacedName{
		Name:      serviceName,
		Namespace: namespace,
	}

	defaultArgoCDInstance, err := argocd.NewCR(gitopsIdentifier, serviceNamespace)

	// The operator decides the namespace based on the version of the cluster it is installed in
	// 4.6 Cluster: Backend in openshift-pipelines-app-delivery namespace and argocd in openshift-gitops namespace
	// 4.7 Cluster: Both backend and argocd instance in openshift-gitops namespace
	argocdNS := newNamespace(defaultArgoCDInstance.Namespace)
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: argocdNS.Name}, &corev1.Namespace{})
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new Namespace", "Name", argocdNS.Name)
		err = r.client.Create(context.TODO(), argocdNS)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	// Set GitopsService instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, defaultArgoCDInstance, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	existingArgoCD := &argoapp.ArgoCD{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: defaultArgoCDInstance.Name, Namespace: defaultArgoCDInstance.Namespace}, existingArgoCD)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new ArgoCD instance", "Namespace", defaultArgoCDInstance.Namespace, "Name", defaultArgoCDInstance.Name)
		err = r.client.Create(context.TODO(), defaultArgoCDInstance)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	// Define a new Pod object
	deploymentObj := newBackendDeployment(serviceNamespacedName)

	// Set GitopsService instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, deploymentObj, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	// Check if this Deployment already exists
	found := &appsv1.Deployment{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: deploymentObj.Name, Namespace: deploymentObj.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new Deployment", "Namespace", deploymentObj.Namespace, "Name", deploymentObj.Name)
		err = r.client.Create(context.TODO(), deploymentObj)
		if err != nil {
			return reconcile.Result{}, err
		}
	}
	serviceRef := newBackendService(serviceNamespacedName)
	// Set GitopsService instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, serviceRef, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	// Check if this Service already exists
	existingServiceRef := &corev1.Service{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: serviceRef.Name, Namespace: serviceRef.Namespace}, existingServiceRef)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new Service", "Namespace", deploymentObj.Namespace, "Name", deploymentObj.Name)
		err = r.client.Create(context.TODO(), serviceRef)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	routeRef := newBackendRoute(serviceNamespacedName)
	// Set GitopsService instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, routeRef, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	existingRoute := &routev1.Route{}

	err = r.client.Get(context.TODO(), types.NamespacedName{Name: routeRef.Name, Namespace: routeRef.Namespace}, existingRoute)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new Route", "Namespace", routeRef.Namespace, "Name", routeRef.Name)
		err = r.client.Create(context.TODO(), routeRef)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	// Update ArgoCD instance for OIDC Config with Keycloakrealm URL
	// Keycloakrealm URL can be derived by adding realm name to Keycloak route URL
	argoCD := argoapp.ArgoCD{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: defaultArgoCDInstance.Name, Namespace: defaultArgoCDInstance.Namespace}, &argoCD)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("ArgoCD instance is not found or yet to be created", "Namespace", defaultArgoCDInstance.Namespace, "Name", defaultArgoCDInstance.Name)
		return reconcile.Result{}, nil
	}

	if !instance.Spec.EnableSSO {
		reqLogger.Info("SSO is set to false, removing if there is any RHSSO Configuration")
		argoCD.Spec.OIDCConfig = ""
		err = r.client.Update(context.TODO(), &argoCD)
		if err != nil {
			reqLogger.Error(err, "Error updating OIDC Config")
			return reconcile.Result{}, err
		}
	} else {
		reqLogger.Info("SSO is set to true, Setting up argocd OIDC Configuration")
		// Create a new Keycloak Instance. Ignore if there is one already.
		// Keycloak instance is created with default values out of the box. Admins or owners are allowed to modify the instance.
		defaultKeycloakInstance := rhsso.NewKeycloakCR(gitopsIdentifier)
		existingKeycloak := &keycloakv1alpha1.Keycloak{}

		// Set GitopsService instance as the owner and controller
		if err := controllerutil.SetControllerReference(instance, defaultKeycloakInstance, r.scheme); err != nil {
			return reconcile.Result{}, err
		}

		err = r.client.Get(context.TODO(), types.NamespacedName{Name: defaultKeycloakInstance.Name, Namespace: defaultKeycloakInstance.Namespace}, existingKeycloak)
		if err != nil {
			reqLogger.Info("Creating a new Keycloak instance", "Namespace", defaultKeycloakInstance.Namespace, "Name", defaultKeycloakInstance.Name)
			err = r.client.Create(context.TODO(), defaultKeycloakInstance)
			if err != nil {
				return reconcile.Result{}, err
			}
		}

		// Create a new Keycloak Realm Instance. Ignore if there is one already.
		// Keycloak Realm instance is created with default values out of the box. Admins or owners are allowed to modify the instance.
		defaultKeycloakRealmInstance := rhsso.NewKeycloakRealmCR(gitopsIdentifier)
		existingKeycloakRealm := &keycloakv1alpha1.KeycloakRealm{}

		// Set GitopsService instance as the owner and controller
		if err := controllerutil.SetControllerReference(instance, defaultKeycloakRealmInstance, r.scheme); err != nil {
			return reconcile.Result{}, err
		}

		err = r.client.Get(context.TODO(), types.NamespacedName{Name: defaultKeycloakRealmInstance.Name, Namespace: defaultKeycloakRealmInstance.Namespace}, existingKeycloakRealm)
		if err != nil && errors.IsNotFound(err) {
			reqLogger.Info("Creating a new Keycloak Realm", "Namespace", defaultKeycloakRealmInstance.Namespace, "Name", defaultKeycloakRealmInstance.Name)
			err = r.client.Create(context.TODO(), defaultKeycloakRealmInstance)
			if err != nil {
				return reconcile.Result{}, err
			}
		}

		// Get Argocd Route URL and pass it to the Keycloak Client installation.
		// Keycloak Client instance is created with default values out of the box. Admins or owners are allowed to modify the instance.
		argoRoute := &routev1.Route{}
		argoRouteRef := existingArgoRoute()
		err = r.client.Get(context.TODO(), types.NamespacedName{Name: argoRouteRef.Name, Namespace: argoRouteRef.Namespace}, argoRoute)
		if err != nil {
			if errors.IsNotFound(err) {
				reqLogger.Info("ArgoCD Route not found or yet to be created", "Namespace", argoRouteRef.Namespace, "Name", argoRouteRef.Name)
				return reconcile.Result{}, nil
			}
			reqLogger.Error(err, "Error getting ArgoCD Route")
			return reconcile.Result{}, err
		}
		argoRouteHost := argoRoute.Spec.Host

		// Keycloak client installation
		defaultKeycloakCientInstance := rhsso.NewKeycloakClientCR(gitopsIdentifier, argoRouteHost)
		existingKeycloakClient := &keycloakv1alpha1.KeycloakClient{}

		// Set GitopsService instance as the owner and controller
		if err := controllerutil.SetControllerReference(instance, defaultKeycloakCientInstance, r.scheme); err != nil {
			return reconcile.Result{}, err
		}

		err = r.client.Get(context.TODO(), types.NamespacedName{Name: defaultKeycloakCientInstance.Name, Namespace: defaultKeycloakCientInstance.Namespace}, existingKeycloakClient)
		if err != nil && errors.IsNotFound(err) {
			reqLogger.Info("Creating a new Keycloak Client", "Namespace", defaultKeycloakCientInstance.Namespace, "Name", defaultKeycloakCientInstance.Name)
			err = r.client.Create(context.TODO(), defaultKeycloakCientInstance)
			if err != nil {
				return reconcile.Result{}, err
			}
		}

		// Update ArgoCD instance for OIDC Config with Keycloakrealm URL
		// Keycloakrealm URL can be derived by adding realm name to Keycloak route URL
		// Get keycloak relam URL from Keycloak route
		keycloakRoute := &routev1.Route{}
		kRef := existingKeycloakRoute()
		err = r.client.Get(context.TODO(), types.NamespacedName{Name: kRef.Name, Namespace: kRef.Namespace}, keycloakRoute)
		if err != nil {
			if errors.IsNotFound(err) {
				reqLogger.Info("Keycloak Route not found or yet to be created", "Namespace", kRef.Namespace, "Name", kRef.Name)
				return reconcile.Result{}, nil
			}
			reqLogger.Error(err, "Error getting Keycloak Route")
			return reconcile.Result{}, err
		}
		keycloakRouteHost := keycloakRoute.Spec.Host
		keycloakRealmURL := fmt.Sprintf("https://%s/auth/realms/%s", keycloakRouteHost, gitopsIdentifier)

		// Update OIDC Config for ArgoCD instance
		o, _ := yaml.Marshal(argocd.OIDCConfig{
			Name:           "Keycloak",
			Issuer:         keycloakRealmURL,
			ClientID:       argoClientID,
			ClientSecret:   "$oidc.keycloak.clientSecret",
			RequestedScope: []string{"openid", "profile", "email", "groups"},
		})

		argoCD.Spec.OIDCConfig = string(o)
		err = r.client.Update(context.TODO(), &argoCD)
		if err != nil {
			reqLogger.Error(err, "Error updating OIDC Config")
			return reconcile.Result{}, err
		}

		// Get Keycloak ClientID and Secret from Keycloak secret
		keycloakSecretRef := existingKeycloakSecret()
		keycloakSecret := &corev1.Secret{}

		err = r.client.Get(context.TODO(), types.NamespacedName{Name: keycloakSecretRef.Name, Namespace: keycloakSecretRef.Namespace}, keycloakSecret)
		if err != nil {
			if errors.IsNotFound(err) {
				reqLogger.Info("Keycloak secret not found or yet to be created", "Namespace", keycloakSecretRef.Namespace, "Name", keycloakSecretRef.Name)
				return reconcile.Result{}, nil
			}
			reqLogger.Error(err, "Error getting keycloak secret")
			return reconcile.Result{}, err
		}
		clientSecret := keycloakSecret.Data["CLIENT_SECRET"]

		// Update argocd-secret secret with Keycloak ClientID and Secret
		// https://argoproj.github.io/argo-cd/operator-manual/user-management/keycloak/#configuring-argocd-oidc
		argoCDSecretRef := existingArgoCDSecret()
		argoCDSecret := corev1.Secret{}

		err = r.client.Get(context.TODO(), types.NamespacedName{Name: argoCDSecretRef.Name, Namespace: argoCDSecretRef.Namespace}, &argoCDSecret)
		if err != nil {
			if errors.IsNotFound(err) {
				reqLogger.Info("argocd secret not found or yet to be created", "Namespace", argoCDSecretRef.Namespace, "Name", argoCDSecretRef.Name)
				return reconcile.Result{}, nil
			}
			reqLogger.Error(err, "Error getting argocd secret")
			return reconcile.Result{}, err
		}

		argoCDSecret.Data["oidc.keycloak.clientSecret"] = clientSecret
		err = r.client.Update(context.TODO(), &argoCDSecret)
		if err != nil {
			reqLogger.Error(err, "Error updating argocd secret")
			return reconcile.Result{}, err
		}

		// Create a new oauthclient Instance. Ignore if there is one already.
		// oauthclient instance is created with default values out of the box. Admins or owners are allowed to modify the instance.
		defaultOAuthCientInstance := rhsso.NewOAuthClient(gitopsIdentifier, keycloakRouteHost)
		existingOAuthClient := &oauthv1.OAuthClient{}

		// Set GitopsService instance as the owner and controller
		if err := controllerutil.SetControllerReference(instance, defaultOAuthCientInstance, r.scheme); err != nil {
			return reconcile.Result{}, err
		}

		err = r.client.Get(context.TODO(), types.NamespacedName{Name: defaultOAuthCientInstance.Name, Namespace: defaultOAuthCientInstance.Namespace}, existingOAuthClient)
		if err != nil && errors.IsNotFound(err) {
			reqLogger.Info("Creating a new OAuth Client", "Namespace", defaultOAuthCientInstance.Namespace, "Name", defaultOAuthCientInstance.Name)
			err = r.client.Create(context.TODO(), defaultOAuthCientInstance)
			if err != nil {
				return reconcile.Result{}, err
			}
		}
	}

	return r.reconcileCLIServer(instance, request)
}

// GetBackendNamespace returns the backend service namespace based on OpenShift Cluster version
func GetBackendNamespace(client client.Client) (string, error) {
	version, err := util.GetClusterVersion(client)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(version, "4.6") {
		return depracatedServiceNamespace, nil
	}
	return serviceNamespace, nil
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
		Spec: pipelinesv1alpha1.GitopsServiceSpec{
			EnableSSO: false,
		},
	}
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

func existingArgoRoute() *routev1.Route {
	name := gitopsIdentifier + "-server"
	namespace := serviceNamespace
	return &routev1.Route{
		ObjectMeta: objectMeta(name, namespace),
	}
}

func existingKeycloakRoute() *routev1.Route {
	name := "keycloak"
	namespace := serviceNamespace
	return &routev1.Route{
		ObjectMeta: objectMeta(name, namespace),
	}
}

func existingKeycloakSecret() *corev1.Secret {
	keycloakSecretName := keycloakSecretName
	namespace := serviceNamespace
	return &corev1.Secret{
		ObjectMeta: objectMeta(keycloakSecretName, namespace),
	}
}

func existingArgoCDSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: objectMeta("argocd-secret", serviceNamespace),
	}
}
