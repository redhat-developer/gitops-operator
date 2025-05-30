package utils

import (
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	argocdv1alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"

	osappsv1 "github.com/openshift/api/apps/v1"
	olmv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"

	rolloutmanagerv1alpha1 "github.com/argoproj-labs/argo-rollouts-manager/api/v1alpha1"
	argov1alpha1api "github.com/argoproj-labs/argocd-operator/api/v1alpha1"
	consolev1 "github.com/openshift/api/console/v1"
	routev1 "github.com/openshift/api/route/v1"
	securityv1 "github.com/openshift/api/security/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	gitopsoperatorv1alpha1 "github.com/redhat-developer/gitops-operator/api/v1alpha1"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	apps "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	crdv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	. "github.com/onsi/gomega"
)

func GetE2ETestKubeClient() (client.Client, *runtime.Scheme) {
	config, err := getSystemKubeConfig()
	Expect(err).ToNot(HaveOccurred())

	k8sClient, scheme, err := getKubeClient(config)
	Expect(err).ToNot(HaveOccurred())

	return k8sClient, scheme
}

func GetE2ETestKubeClientWithError() (client.Client, *runtime.Scheme, error) {
	config, err := getSystemKubeConfig()
	if err != nil {
		return nil, nil, err
	}

	k8sClient, scheme, err := getKubeClient(config)
	if err != nil {
		return nil, nil, err
	}

	return k8sClient, scheme, nil
}

// getKubeClient returns a controller-runtime Client for accessing K8s API resources used by the controller.
func getKubeClient(config *rest.Config) (client.Client, *runtime.Scheme, error) {

	scheme := runtime.NewScheme()

	if err := corev1.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}

	if err := apps.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}
	if err := rbacv1.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}

	if err := admissionv1.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}

	if err := monitoringv1.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}

	if err := crdv1.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}

	if err := argov1beta1api.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}

	if err := argocdv1alpha1.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}

	if err := gitopsoperatorv1alpha1.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}

	if err := olmv1alpha1.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}

	if err := routev1.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}

	if err := osappsv1.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}

	if err := consolev1.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}
	if err := rolloutmanagerv1alpha1.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}

	if err := argov1alpha1api.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}

	if err := securityv1.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}

	if err := networkingv1.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}

	if err := autoscalingv2.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}

	k8sClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return nil, nil, err
	}

	return k8sClient, scheme, nil

}

// Retrieve the system-level Kubernetes config (e.g. ~/.kube/config or service account config from volume)
func getSystemKubeConfig() (*rest.Config, error) {

	overrides := clientcmd.ConfigOverrides{}

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	clientConfig := clientcmd.NewInteractiveDeferredLoadingClientConfig(loadingRules, &overrides, os.Stdin)

	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, err
	}
	return restConfig, nil
}
