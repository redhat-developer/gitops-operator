package gitopsservice

import (
	"context"

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
const cliImage = "quay.io/shbose/kam-service:v0.1"

func newDeploymentForCLI(cr *pipelinesv1alpha1.GitopsService) *appsv1.Deployment {
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
						SecretName: cliName,
					},
				},
			},
		},
	}

	template := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"app": cliName,
			},
		},
		Spec: podSpec,
	}

	var replicas int32 = 1
	deploymentSpec := appsv1.DeploymentSpec{
		Replicas: &replicas,
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"app": cliName,
			},
		},
		Template: template,
	}

	deploymentObj := &appsv1.Deployment{
		ObjectMeta: objectMeta(cliName, cr.Namespace),
		Spec:       deploymentSpec,
	}

	return deploymentObj
}

func newServiceForCLI(cr *pipelinesv1alpha1.GitopsService) *corev1.Service {

	spec := corev1.ServiceSpec{
		Ports: []corev1.ServicePort{
			{
				Port:       port,
				Protocol:   corev1.ProtocolTCP,
				TargetPort: intstr.FromInt(int(port)),
			},
		},
		Selector: map[string]string{
			"app": cliName,
		},
	}
	svc := &corev1.Service{
		ObjectMeta: objectMeta(cliName, cr.Namespace, func(o *metav1.ObjectMeta) {
			o.Annotations = map[string]string{
				"service.beta.openshift.io/serving-cert-secret-name": cliName,
			}
		}),
		Spec: spec,
	}
	return svc
}

func newRouteForCLI(cr *pipelinesv1alpha1.GitopsService) *routev1.Route {
	routeSpec := routev1.RouteSpec{
		To: routev1.RouteTargetReference{
			Kind: "Service",
			Name: cliName,
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
		ObjectMeta: objectMeta(cliName, cr.Namespace),
		Spec:       routeSpec,
	}

	return routeObj
}

func (r *ReconcileGitopsService) reconcileCLI(cr *pipelinesv1alpha1.GitopsService) (reconcile.Result, error) {

	deploymentObj := newDeploymentForCLI(cr)

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
	serviceRef := newServiceForCLI(cr)
	if err := controllerutil.SetControllerReference(instance, serviceRef, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	// Check if this Service already exists
	existingServiceRef := &corev1.Service{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: deploymentObj.Name, Namespace: deploymentObj.Namespace}, existingServiceRef)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new Service", "Namespace", deploymentObj.Namespace, "Name", deploymentObj.Name)
		err = r.client.Create(context.TODO(), serviceRef)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	routeRef := newRouteForCLI(cr)
	if err := controllerutil.SetControllerReference(instance, routeRef, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	existingRoute := &routev1.Route{}

	err = r.client.Get(context.TODO(), types.NamespacedName{Name: deploymentObj.Name, Namespace: deploymentObj.Namespace}, existingRoute)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new Route", "Namespace", routeRef.Namespace, "Name", routeRef.Name)
		err = r.client.Create(context.TODO(), routeRef)
		if err != nil {
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}
	return reconcile.Result{}, nil
}
