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
	"os"
	"reflect"

	resourcev1 "k8s.io/apimachinery/pkg/api/resource"

	argocommon "github.com/argoproj-labs/argocd-operator/common"
	argocdutil "github.com/argoproj-labs/argocd-operator/controllers/argoutil"
	console "github.com/openshift/api/console/v1"
	routev1 "github.com/openshift/api/route/v1"
	pipelinesv1alpha1 "github.com/redhat-developer/gitops-operator/api/v1alpha1"
	"github.com/redhat-developer/gitops-operator/common"
	"github.com/redhat-developer/gitops-operator/controllers/util"
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
			Links: []console.CLIDownloadLink{
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

	reqLogger := logs.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)

	if !util.IsConsoleAPIFound() {
		reqLogger.Info("Skip cli server reconcile: OpenShift Console API not found")
		return reconcile.Result{}, nil
	}

	deploymentObj := newDeploymentForCLI()

	// Add SeccompProfile based on cluster version
	util.AddSeccompProfileForOpenShift(r.Client, &deploymentObj.Spec.Template.Spec)

	deploymentObj.Spec.Template.Spec.NodeSelector = argocommon.DefaultNodeSelector()

	if err := controllerutil.SetControllerReference(cr, deploymentObj, r.Scheme); err != nil {
		return reconcile.Result{}, err
	}
	if cr.Spec.RunOnInfra {
		deploymentObj.Spec.Template.Spec.NodeSelector[common.InfraNodeLabelSelector] = ""
	}
	if len(cr.Spec.NodeSelector) > 0 {
		deploymentObj.Spec.Template.Spec.NodeSelector = argocdutil.AppendStringMap(deploymentObj.Spec.Template.Spec.NodeSelector, cr.Spec.NodeSelector)
	}

	if len(cr.Spec.Tolerations) > 0 {
		deploymentObj.Spec.Template.Spec.Tolerations = cr.Spec.Tolerations
	}
	// Check if this Deployment already exists
	existingDeployment := &appsv1.Deployment{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: deploymentObj.Name, Namespace: deploymentObj.Namespace}, existingDeployment)
	if err != nil {
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
		if existingDeployment.Spec.Template.Spec.Containers[0].Resources.Requests == nil {
			existingDeployment.Spec.Template.Spec.Containers[0].Resources = deploymentObj.Spec.Template.Spec.Containers[0].Resources
			changed = true
		}
		if !reflect.DeepEqual(existingDeployment.Spec.Template.Spec.Containers[0].Image, deploymentObj.Spec.Template.Spec.Containers[0].Image) {
			existingDeployment.Spec.Template.Spec.Containers[0].Image = deploymentObj.Spec.Template.Spec.Containers[0].Image
			changed = true
		}
		if !reflect.DeepEqual(existingDeployment.Spec.Template.Spec.NodeSelector, deploymentObj.Spec.Template.Spec.NodeSelector) {
			existingDeployment.Spec.Template.Spec.NodeSelector = deploymentObj.Spec.Template.Spec.NodeSelector
			changed = true
		}
		if !reflect.DeepEqual(existingDeployment.Spec.Template.Spec.Tolerations, deploymentObj.Spec.Template.Spec.Tolerations) {
			existingDeployment.Spec.Template.Spec.Tolerations = deploymentObj.Spec.Template.Spec.Tolerations
			changed = true
		}
		if !reflect.DeepEqual(existingDeployment.Spec.Template.Spec.SecurityContext, deploymentObj.Spec.Template.Spec.SecurityContext) {
			existingDeployment.Spec.Template.Spec.SecurityContext = deploymentObj.Spec.Template.Spec.SecurityContext
			changed = true
		}
		if !reflect.DeepEqual(existingDeployment.Spec.Template.Spec.Containers[0].SecurityContext, deploymentObj.Spec.Template.Spec.Containers[0].SecurityContext) {
			existingDeployment.Spec.Template.Spec.Containers[0].SecurityContext = deploymentObj.Spec.Template.Spec.Containers[0].SecurityContext
			changed = true
		}

		if changed {
			err = r.Client.Update(context.TODO(), existingDeployment)
			if err != nil {
				return reconcile.Result{}, err
			}
		}
	}
	serviceRef := newServiceForCLI()
	if err := controllerutil.SetControllerReference(cr, serviceRef, r.Scheme); err != nil {
		return reconcile.Result{}, err
	}

	// Check if this Service already exists
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: serviceRef.Name, Namespace: serviceRef.Namespace}, &corev1.Service{})
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("Creating a new Service", "Namespace", deploymentObj.Namespace, "Name", deploymentObj.Name)
			err = r.Client.Create(context.TODO(), serviceRef)
			if err != nil {
				return reconcile.Result{}, err
			}
		} else {
			return reconcile.Result{}, err
		}
	}

	routeRef := newRouteForCLI()
	if err := controllerutil.SetControllerReference(cr, routeRef, r.Scheme); err != nil {
		return reconcile.Result{}, err
	}

	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: routeRef.Name, Namespace: routeRef.Namespace}, &routev1.Route{})
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("Creating a new Route", "Namespace", routeRef.Namespace, "Name", routeRef.Name)
			err = r.Client.Create(context.TODO(), routeRef)
			if err != nil {
				return reconcile.Result{}, err
			}
		} else {
			return reconcile.Result{}, err
		}
	}

	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: routeRef.Name, Namespace: routeRef.Namespace}, routeRef)
	kamDownloadURLgo := fmt.Sprintf("https://%s/kam/", routeRef.Spec.Host)

	consoleCLIDownload := newConsoleCLIDownload(cliName, kamDownloadURLgo, cliLongName)
	if err := controllerutil.SetControllerReference(cr, consoleCLIDownload, r.Scheme); err != nil {
		return reconcile.Result{}, err
	}

	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: consoleCLIDownload.Name}, &console.ConsoleCLIDownload{})
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("Creating a new ConsoleDownload", "ConsoleDownload.Name", consoleCLIDownload.Name)
			return reconcile.Result{}, r.Client.Create(context.TODO(), consoleCLIDownload)
		}
		reqLogger.Error(err, "Failed to create ConsoleDownload", "ConsoleDownload.Name", consoleCLIDownload.Name)
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}
