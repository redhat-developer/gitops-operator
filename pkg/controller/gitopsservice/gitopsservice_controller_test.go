package gitopsservice

import (
	"context"
	"os"
	"testing"

	"gotest.tools/assert"

	argoapp "github.com/argoproj-labs/argocd-operator/pkg/apis/argoproj/v1alpha1"
	configv1 "github.com/openshift/api/config/v1"
	consolev1 "github.com/openshift/api/console/v1"
	routev1 "github.com/openshift/api/route/v1"
	pipelinesv1alpha1 "github.com/redhat-developer/gitops-operator/pkg/apis/pipelines/v1alpha1"
	"github.com/redhat-developer/gitops-operator/pkg/controller/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	resourcev1 "k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

func TestImageFromEnvVariable(t *testing.T) {
	ns := types.NamespacedName{Name: "test", Namespace: "test"}
	t.Run("Image present as env variable", func(t *testing.T) {
		image := "quay.io/org/test"
		os.Setenv(backendImageEnvName, image)
		defer os.Unsetenv(backendImageEnvName)

		deployment := newBackendDeployment(ns)

		got := deployment.Spec.Template.Spec.Containers[0].Image
		if got != image {
			t.Errorf("Image mismatch: got %s, want %s", got, image)
		}
	})
	t.Run("env variable for image not found", func(t *testing.T) {
		deployment := newBackendDeployment(ns)

		got := deployment.Spec.Template.Spec.Containers[0].Image
		if got != backendImage {
			t.Errorf("Image mismatch: got %s, want %s", got, backendImage)
		}
	})

	t.Run("Kam Image present as env variable", func(t *testing.T) {
		image := "quay.io/org/test"
		os.Setenv(cliImageEnvName, image)
		defer os.Unsetenv(cliImageEnvName)

		deployment := newDeploymentForCLI()

		got := deployment.Spec.Template.Spec.Containers[0].Image
		if got != image {
			t.Errorf("Image mismatch: got %s, want %s", got, image)
		}
	})
	t.Run("env variable for Kam image not found", func(t *testing.T) {
		deployment := newDeploymentForCLI()

		got := deployment.Spec.Template.Spec.Containers[0].Image
		if got != cliImage {
			t.Errorf("Image mismatch: got %s, want %s", got, cliImage)
		}
	})

}

func TestReconcile(t *testing.T) {
	logf.SetLogger(logf.ZapLogger(true))
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewFakeClient(newGitopsService())
	reconciler := newReconcileGitOpsService(fakeClient, s)

	_, err := reconciler.Reconcile(newRequest("test", "test"))
	assertNoError(t, err)

	// Check if backend resources are created in openshift-gitops namespace
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceNamespace}, &corev1.Namespace{})
	assertNoError(t, err)

	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: gitopsServicePrefix + serviceName, Namespace: serviceNamespace}, &corev1.ServiceAccount{})
	assertNoError(t, err)

	role := &rbacv1.ClusterRole{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: gitopsServicePrefix + serviceName}, role)
	assertNoError(t, err)
	assert.DeepEqual(t, role.Rules, policyRuleForBackendServiceClusterRole())

	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: gitopsServicePrefix + serviceName}, &rbacv1.ClusterRoleBinding{})
	assertNoError(t, err)

	deploy := &appsv1.Deployment{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceName, Namespace: serviceNamespace}, deploy)
	assertNoError(t, err)

	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceName, Namespace: serviceNamespace}, &corev1.Service{})
	assertNoError(t, err)

	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceName, Namespace: serviceNamespace}, &routev1.Route{})
	assertNoError(t, err)

	// Check if argocd instance is created in openshift-gitops namespace
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: "openshift-gitops", Namespace: serviceNamespace}, &argoapp.ArgoCD{})
	assertNoError(t, err)

	// update Cluster Role and Backend Deployment
	updatedPolicyRules := policyRuleForBackendServiceClusterRole()
	updatedPolicyRules[0].Resources = append(updatedPolicyRules[0].Resources, "testResource")
	role.Rules = updatedPolicyRules

	err = fakeClient.Update(context.TODO(), role)
	assertNoError(t, err)

	deploy.Spec.Template.Spec.Containers[0].Image = "newTestImage:test"
	err = fakeClient.Update(context.TODO(), deploy)
	assertNoError(t, err)

	// reconcile
	_, err = reconciler.Reconcile(newRequest("test", "test"))
	assertNoError(t, err)

	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: gitopsServicePrefix + serviceName}, role)
	assertNoError(t, err)
	assert.DeepEqual(t, role.Rules, policyRuleForBackendServiceClusterRole())

	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceName, Namespace: serviceNamespace}, deploy)
	assertNoError(t, err)
	assert.DeepEqual(t, deploy.Spec.Template.Spec.Containers[0].Image, backendImage)
}

func TestReconcile_AppDeliveryNamespace(t *testing.T) {
	logf.SetLogger(logf.ZapLogger(true))
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewFakeClient(util.NewClusterVersion("4.6.15"), newGitopsService())
	reconciler := newReconcileGitOpsService(fakeClient, s)

	_, err := reconciler.Reconcile(newRequest("test", "test"))
	assertNoError(t, err)

	// Check if both openshift-gitops and openshift-pipelines-app-delivey namespace is created
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: depracatedServiceNamespace}, &corev1.Namespace{})
	assertNoError(t, err)
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceNamespace}, &corev1.Namespace{})
	assertNoError(t, err)

	// Check if backend resources are created in openshift-pipelines-app-delivery namespace
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: depracatedServiceNamespace}, &corev1.Namespace{})
	assertNoError(t, err)

	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceName, Namespace: depracatedServiceNamespace}, &appsv1.Deployment{})
	assertNoError(t, err)

	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceName, Namespace: depracatedServiceNamespace}, &corev1.Service{})
	assertNoError(t, err)

	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceName, Namespace: depracatedServiceNamespace}, &routev1.Route{})
	assertNoError(t, err)

	// Check if argocd instance is created in openshift-gitops namespace
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: "openshift-gitops", Namespace: serviceNamespace}, &argoapp.ArgoCD{})
	assertNoError(t, err)
}

func TestReconcile_GitOpsNamespace(t *testing.T) {
	logf.SetLogger(logf.ZapLogger(true))
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewFakeClient(util.NewClusterVersion("4.7.1"), newGitopsService())
	reconciler := newReconcileGitOpsService(fakeClient, s)

	_, err := reconciler.Reconcile(newRequest("test", "test"))
	assertNoError(t, err)

	// Check if only openshift-gitops namespace is created
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceNamespace}, &corev1.Namespace{})
	assertNoError(t, err)

	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: depracatedServiceNamespace}, &corev1.Namespace{})
	wantErr := `namespaces "openshift-pipelines-app-delivery" not found`
	if err == nil {
		t.Fatalf("was expecting an error %s, but got nil", wantErr)
	}
}

func TestReconcile_GitOpsNamespaceResourceQuotas(t *testing.T) {
	logf.SetLogger(logf.ZapLogger(true))
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewFakeClientWithScheme(s, util.NewClusterVersion("4.7.1"), newGitopsService())
	reconciler := newReconcileGitOpsService(fakeClient, s)

	_, err := reconciler.Reconcile(newRequest("test", "test"))
	assertNoError(t, err)

	resourceQuota := corev1.ResourceQuota{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceNamespace + "-compute-resources", Namespace: serviceNamespace}, &resourceQuota)
	assertNoError(t, err)

	assert.Equal(t, resourceQuota.Spec.Hard[corev1.ResourceCPU], resourcev1.MustParse("6688m"))
	assert.Equal(t, resourceQuota.Spec.Hard[corev1.ResourceMemory], resourcev1.MustParse("4544Mi"))
	assert.Equal(t, resourceQuota.Spec.Hard[corev1.ResourceLimitsCPU], resourcev1.MustParse("13750m"))
	assert.Equal(t, resourceQuota.Spec.Hard[corev1.ResourceLimitsMemory], resourcev1.MustParse("9070Mi"))
	assert.DeepEqual(t, resourceQuota.Spec.Scopes, []corev1.ResourceQuotaScope{"NotTerminating"})
}

func TestReconcile_BackendResourceLimits(t *testing.T) {
	logf.SetLogger(logf.ZapLogger(true))
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewFakeClientWithScheme(s, util.NewClusterVersion("4.7.1"), newGitopsService())
	reconciler := newReconcileGitOpsService(fakeClient, s)

	_, err := reconciler.Reconcile(newRequest("test", "test"))
	assertNoError(t, err)

	deployment := appsv1.Deployment{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceName, Namespace: serviceNamespace}, &deployment)
	assertNoError(t, err)

	resources := deployment.Spec.Template.Spec.Containers[0].Resources
	assert.Equal(t, resources.Requests[corev1.ResourceCPU], resourcev1.MustParse("250m"))
	assert.Equal(t, resources.Requests[corev1.ResourceMemory], resourcev1.MustParse("128Mi"))
	assert.Equal(t, resources.Limits[corev1.ResourceCPU], resourcev1.MustParse("500m"))
	assert.Equal(t, resources.Limits[corev1.ResourceMemory], resourcev1.MustParse("256Mi"))
}

func TestGetBackendNamespace(t *testing.T) {
	logf.SetLogger(logf.ZapLogger(true))
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	assertNamespace := func(t *testing.T, err error, got, want string) {
		t.Helper()
		assertNoError(t, err)
		if got != want {
			t.Fatalf("namespace mismatch: got %s, want %s", got, want)
		}
	}

	t.Run("Using a 4.7 Cluster", func(t *testing.T) {
		fakeClient := fake.NewFakeClient(util.NewClusterVersion("4.7.1"), newGitopsService())
		namespace, err := GetBackendNamespace(fakeClient)
		assertNamespace(t, err, namespace, serviceNamespace)
	})

	t.Run("Using a 4.6 Cluster", func(t *testing.T) {
		fakeClient := fake.NewFakeClient(util.NewClusterVersion("4.6.1"), newGitopsService())
		namespace, err := GetBackendNamespace(fakeClient)
		assertNamespace(t, err, namespace, depracatedServiceNamespace)
	})

	t.Run("Using a 4.X Cluster", func(t *testing.T) {
		fakeClient := fake.NewFakeClient(util.NewClusterVersion("4.X.1"), newGitopsService())
		namespace, err := GetBackendNamespace(fakeClient)
		assertNamespace(t, err, namespace, serviceNamespace)
	})
}

func addKnownTypesToScheme(scheme *runtime.Scheme) {
	scheme.AddKnownTypes(configv1.GroupVersion, &configv1.ClusterVersion{})
	scheme.AddKnownTypes(pipelinesv1alpha1.SchemeGroupVersion, &pipelinesv1alpha1.GitopsService{})
	scheme.AddKnownTypes(routev1.GroupVersion, &routev1.Route{})
	scheme.AddKnownTypes(argoapp.SchemeGroupVersion, &argoapp.ArgoCD{})
	scheme.AddKnownTypes(consolev1.GroupVersion, &consolev1.ConsoleCLIDownload{})
}

func newReconcileGitOpsService(client client.Client, scheme *runtime.Scheme) *ReconcileGitopsService {
	return &ReconcileGitopsService{
		client: client,
		scheme: scheme,
	}
}

func newRequest(namespace, name string) reconcile.Request {
	return reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		},
	}
}

func assertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
