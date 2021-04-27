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
	"os"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	argoapp "github.com/argoproj-labs/argocd-operator/pkg/apis/argoproj/v1alpha1"
	routev1 "github.com/openshift/api/route/v1"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pipelinesv1alpha1 "github.com/redhat-developer/gitops-operator/api/v1alpha1"
	argocd "github.com/redhat-developer/gitops-operator/controllers/argocd"

	"github.com/redhat-developer/gitops-operator/controllers/util"
)

var (

	// backend service deployment variables
	port                       int32  = 8080
	portTLS                    int32  = 8443
	backendImage               string = "quay.io/redhat-developer/gitops-backend:v0.0.1"
	backendImageEnvName               = "BACKEND_IMAGE"
	insecureEnvVar                    = "INSECURE"
	insecureEnvVarValue               = "true"
	serviceNamespace                  = "openshift-gitops"
	depracatedServiceNamespace        = "openshift-pipelines-app-delivery"
	serviceName                       = "cluster"
)

// GitopsServiceReconciler reconciles a GitopsService object
type GitopsServiceReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=pipelines.openshift.io,resources=gitopsservices,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=pipelines.openshift.io,resources=gitopsservices/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=pipelines.openshift.io,resources=gitopsservices/finalizers,verbs=update

//+kubebuilder:rbac:groups=pipelines.openshift.io,resources=*,verbs=create;delete;get;list;patch;update;watch

//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings,verbs=*
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=*,verbs=*

//+kubebuilder:rbac:groups=,resources=configmaps;endpoints;events;persistentvolumeclaims;pods;secrets;serviceaccounts;services;services/finalizers,verbs=*
//+kubebuilder:rbac:groups=,resources=pods;pods/log,verbs=get
//+kubebuilder:rbac:groups=,resources=pods,verbs=get
//+kubebuilder:rbac:groups=,resources=pods;services;services/finalizers;endpoints;persistentvolumeclaims;events;configmaps;secrets;namespaces,verbs=create;delete;get;list;patch;update;watch

//+kubebuilder:rbac:groups=apps,resources=deployments;replicasets;statefulsets,verbs=*
//+kubebuilder:rbac:groups=apps,resources=deployments;daemonsets;replicasets;statefulsets,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups=apps,resourceNames=gitops-operator,resources=deployments/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=replicasets;deployments,verbs=get

//+kubebuilder:rbac:groups=apps.openshift.io,resources=*,verbs=*

//+kubebuilder:rbac:groups=route.openshift.io,resources=routes;routes/custom-host,verbs=*
//+kubebuilder:rbac:groups=route.openshift.io,resources=*,verbs=*

//+kubebuilder:rbac:groups=config.openshift.io,resources=clusterversions,verbs=get;list;watch

//+kubebuilder:rbac:groups=console.openshift.io,resources=consoleclidownloads,verbs=create;get;list;patch;update;watch
//+kubebuilder:rbac:groups=console.openshift.io,resources=consolelinks,verbs=create;delete;get;list;patch;update;watch

//+kubebuilder:rbac:groups=argoproj.io,resources=argocds;argocds/finalizers;argocds/status;applications;appprojects,verbs=*
//+kubebuilder:rbac:groups=argoproj.io,resources=argocds,verbs=get;list;watch;create

//+kubebuilder:rbac:groups=autoscaling,resources=horizontalpodautoscalers,verbs=*
//+kubebuilder:rbac:groups=batch,resources=cronjobs;jobs,verbs=*
//+kubebuilder:rbac:groups=extensions,resources=ingresses,verbs=*

//+kubebuilder:rbac:groups=operators.coreos.com,resources=operatorgroups;subscriptions;clusterserviceversions,verbs=create;get;list;watch

// Reconcile reads that state of the cluster for a GitopsService object and makes changes based on the state read
// and what is in the GitopsService.Spec
func (r *GitopsServiceReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	reqLogger := r.Log.WithValues("gitopsservice", req.NamespacedName)

	reqLogger.Info("Reconciling GitopsService")

	// Fetch the GitopsService instance
	instance := &pipelinesv1alpha1.GitopsService{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: serviceName}, instance)
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

	namespaceRef := newNamespace(namespace)
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: namespace}, &corev1.Namespace{})
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new Namespace", "Name", namespace)
		err = r.Client.Create(context.TODO(), namespaceRef)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	serviceNamespacedName := types.NamespacedName{
		Name:      serviceName,
		Namespace: namespace,
	}

	defaultArgoCDInstance, err := argocd.NewCR(util.ArgoCDInstanceName, serviceNamespace)

	// The operator decides the namespace based on the version of the cluster it is installed in
	// 4.6 Cluster: Backend in openshift-pipelines-app-delivery namespace and argocd in openshift-gitops namespace
	// 4.7 Cluster: Both backend and argocd instance in openshift-gitops namespace
	argocdNS := newNamespace(defaultArgoCDInstance.Namespace)
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: argocdNS.Name}, &corev1.Namespace{})
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new Namespace", "Name", argocdNS.Name)
		err = r.Client.Create(context.TODO(), argocdNS)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	// Set GitopsService instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, defaultArgoCDInstance, r.Scheme); err != nil {
		return reconcile.Result{}, err
	}

	existingArgoCD := &argoapp.ArgoCD{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: defaultArgoCDInstance.Name, Namespace: defaultArgoCDInstance.Namespace}, existingArgoCD)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new ArgoCD instance", "Namespace", defaultArgoCDInstance.Namespace, "Name", defaultArgoCDInstance.Name)
		err = r.Client.Create(context.TODO(), defaultArgoCDInstance)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	// Define a new Pod object
	deploymentObj := newBackendDeployment(serviceNamespacedName)

	// Set GitopsService instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, deploymentObj, r.Scheme); err != nil {
		return reconcile.Result{}, err
	}

	// Check if this Deployment already exists
	found := &appsv1.Deployment{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: deploymentObj.Name, Namespace: deploymentObj.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new Deployment", "Namespace", deploymentObj.Namespace, "Name", deploymentObj.Name)
		err = r.Client.Create(context.TODO(), deploymentObj)
		if err != nil {
			return reconcile.Result{}, err
		}
	}
	serviceRef := newBackendService(serviceNamespacedName)
	// Set GitopsService instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, serviceRef, r.Scheme); err != nil {
		return reconcile.Result{}, err
	}

	// Check if this Service already exists
	existingServiceRef := &corev1.Service{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: serviceRef.Name, Namespace: serviceRef.Namespace}, existingServiceRef)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new Service", "Namespace", deploymentObj.Namespace, "Name", deploymentObj.Name)
		err = r.Client.Create(context.TODO(), serviceRef)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	routeRef := newBackendRoute(serviceNamespacedName)
	// Set GitopsService instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, routeRef, r.Scheme); err != nil {
		return reconcile.Result{}, err
	}

	existingRoute := &routev1.Route{}

	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: routeRef.Name, Namespace: routeRef.Namespace}, existingRoute)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new Route", "Namespace", routeRef.Namespace, "Name", routeRef.Name)
		err = r.Client.Create(context.TODO(), routeRef)
		if err != nil {
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}

	return r.reconcileCLIServer(instance, req)

}

// SetupWithManager sets up the controller with the Manager.
func (r *GitopsServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&pipelinesv1alpha1.GitopsService{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&routev1.Route{}).
		Complete(r)
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
