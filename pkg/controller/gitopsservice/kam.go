package gitopsservice

import (
	"context"
	"fmt"
	"os"

	console "github.com/openshift/api/console/v1"
	routev1 "github.com/openshift/api/route/v1"

	pipelinesv1alpha1 "github.com/redhat-developer/gitops-operator/pkg/apis/pipelines/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const cliName = "kam"
const cliLongName = "GitOps Application Manager"
const cliImage = "quay.io/redhat-developer/kam:v0.0.19"
const cliImageEnvName = "KAM_IMAGE"
const kubeAppLabelName = "app.kubernetes.io/name"

func newDeploymentForCLI() *appsv1.Deployment {
	image := os.Getenv(cliImageEnvName)
	if image == "" {
		image = cliImage
	}
	podSpec := corev1.PodSpec{
		Containers: []corev1.Container{
			{
				Name:  cliName,
				Image: image,
				Ports: []corev1.ContainerPort{
					{
						Name:          "http",
						Protocol:      corev1.ProtocolTCP,
						ContainerPort: port, // should come from flag
					},
				},
			},
		},
	}

	template := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				kubeAppLabelName: cliName,
			},
		},
		Spec: podSpec,
	}

	var replicas int32 = 1
	deploymentSpec := appsv1.DeploymentSpec{
		Replicas: &replicas,
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				kubeAppLabelName: cliName,
			},
		},
		Template: template,
	}

	deploymentObj := &appsv1.Deployment{
		ObjectMeta: objectMeta(cliName, serviceNamespace),
		Spec:       deploymentSpec,
	}

	return deploymentObj
}

func newServiceForCLI() *corev1.Service {

	spec := corev1.ServiceSpec{
		Ports: []corev1.ServicePort{
			{
				Name:       "tcp-8080",
				Port:       port,
				Protocol:   corev1.ProtocolTCP,
				TargetPort: intstr.FromInt(int(port)),
			},
			{
				Name:       "tcp-8443",
				Port:       portTLS,
				Protocol:   corev1.ProtocolTCP,
				TargetPort: intstr.FromInt(int(portTLS)),
			},
		},
		Selector: map[string]string{
			kubeAppLabelName: cliName,
		},
	}
	svc := &corev1.Service{
		ObjectMeta: objectMeta(cliName, serviceNamespace),
		Spec:       spec,
	}
	return svc
}

func newRouteForCLI() *routev1.Route {
	routeSpec := routev1.RouteSpec{
		To: routev1.RouteTargetReference{
			Kind: "Service",
			Name: cliName,
		},
		Port: &routev1.RoutePort{
			TargetPort: intstr.IntOrString{IntVal: portTLS},
		},
		TLS: &routev1.TLSConfig{
			Termination:                   routev1.TLSTerminationPassthrough,
			InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyNone,
		},
	}

	return &routev1.Route{
		ObjectMeta: objectMeta(cliName, serviceNamespace),
		Spec:       routeSpec,
	}
}

func newConsoleCLIDownload(consoleLinkName, href, text string) *console.ConsoleCLIDownload {
	return &console.ConsoleCLIDownload{
		ObjectMeta: metav1.ObjectMeta{
			Name: consoleLinkName,
		},
		Spec: console.ConsoleCLIDownloadSpec{
			Links: []console.Link{
				{
					Text: text,
					Href: href,
				},
			},
			Description: text,
			DisplayName: text,
		},
	}
}

func (r *ReconcileGitopsService) reconcileCLIServer(cr *pipelinesv1alpha1.GitopsService, request reconcile.Request) (reconcile.Result, error) {

	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)

	deploymentObj := newDeploymentForCLI()

	if err := controllerutil.SetControllerReference(cr, deploymentObj, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	// Check if this Deployment already exists
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: deploymentObj.Name, Namespace: deploymentObj.Namespace}, &appsv1.Deployment{})
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new Deployment", "Namespace", deploymentObj.Namespace, "Name", deploymentObj.Name)
		err = r.client.Create(context.TODO(), deploymentObj)
		if err != nil {
			return reconcile.Result{}, err
		}
	}
	serviceRef := newServiceForCLI()
	if err := controllerutil.SetControllerReference(cr, serviceRef, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	// Check if this Service already exists
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: serviceRef.Name, Namespace: serviceRef.Namespace}, &corev1.Service{})
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new Service", "Namespace", deploymentObj.Namespace, "Name", deploymentObj.Name)
		err = r.client.Create(context.TODO(), serviceRef)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	routeRef := newRouteForCLI()
	if err := controllerutil.SetControllerReference(cr, routeRef, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	err = r.client.Get(context.TODO(), types.NamespacedName{Name: routeRef.Name, Namespace: routeRef.Namespace}, &routev1.Route{})
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new Route", "Namespace", routeRef.Namespace, "Name", routeRef.Name)
		err = r.client.Create(context.TODO(), routeRef)
		if err != nil {
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}

	err = r.client.Get(context.TODO(), types.NamespacedName{Name: routeRef.Name, Namespace: routeRef.Namespace}, routeRef)
	kamDownloadURLgo := fmt.Sprintf("https://%s/kam/", routeRef.Spec.Host)

	consoleCLIDownload := newConsoleCLIDownload(cliName, kamDownloadURLgo, cliLongName)
	if err := controllerutil.SetControllerReference(cr, consoleCLIDownload, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	err = r.client.Get(context.TODO(), types.NamespacedName{Name: consoleCLIDownload.Name}, &console.ConsoleCLIDownload{})
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("Creating a new ConsoleDownload", "ConsoleDownload.Name", consoleCLIDownload.Name)
			return reconcile.Result{}, r.client.Create(context.TODO(), consoleCLIDownload)
		}
		reqLogger.Error(err, "Failed to create ConsoleDownload", "ConsoleDownload.Name", consoleCLIDownload.Name)
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}
