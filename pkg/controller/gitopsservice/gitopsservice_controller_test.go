package gitopsservice

import (
	"context"
	"os"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"
	pipelinesv1alpha1 "github.com/redhat-developer/gitops-operator/pkg/apis/pipelines/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceNamespace}, &corev1.Namespace{})
	assertNoError(t, err)

	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceName, Namespace: serviceNamespace}, &appsv1.Deployment{})
	assertNoError(t, err)

	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceName, Namespace: serviceNamespace}, &corev1.Service{})
	assertNoError(t, err)

	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceName, Namespace: serviceNamespace}, &routev1.Route{})
	assertNoError(t, err)
}

func TestReconcile_AppDeliveryNamespace(t *testing.T) {
	logf.SetLogger(logf.ZapLogger(true))
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewFakeClient(newClusterVersion("4.6.15"), newGitopsService())
	reconciler := newReconcileGitOpsService(fakeClient, s)

	_, err := reconciler.Reconcile(newRequest("test", "test"))
	assertNoError(t, err)

	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: depracatedServiceNamespace}, &corev1.Namespace{})
	assertNoError(t, err)
}

func TestReconcile_GitOpsNamespace(t *testing.T) {
	logf.SetLogger(logf.ZapLogger(true))
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewFakeClient(newClusterVersion("4.7.1"), newGitopsService())
	reconciler := newReconcileGitOpsService(fakeClient, s)

	_, err := reconciler.Reconcile(newRequest("test", "test"))
	assertNoError(t, err)

	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: serviceNamespace}, &corev1.Namespace{})
	assertNoError(t, err)
}

func TestGetClusterVersion(t *testing.T) {
	logf.SetLogger(logf.ZapLogger(true))
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	t.Run("Valid Cluster Version", func(t *testing.T) {
		version := "4.7.1"
		fakeClient := fake.NewFakeClient(newClusterVersion(version))
		clusterVersion, err := getClusterVersion(fakeClient)
		assertNoError(t, err)
		if clusterVersion != version {
			t.Fatalf("got %s, want %s", clusterVersion, version)
		}
	})
	t.Run("Cluster Version not found", func(t *testing.T) {
		fakeClient := fake.NewFakeClient()
		clusterVersion, err := getClusterVersion(fakeClient)
		assertNoError(t, err)
		if clusterVersion != "" {
			t.Fatalf("got %s, want %s", clusterVersion, "")
		}
	})
}

func newClusterVersion(version string) *configv1.ClusterVersion {
	return &configv1.ClusterVersion{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterVersionName,
		},
		Spec: configv1.ClusterVersionSpec{
			Channel: "stable",
		},
		Status: configv1.ClusterVersionStatus{
			Desired: configv1.Update{
				Version: version,
			},
		},
	}
}

func addKnownTypesToScheme(scheme *runtime.Scheme) {
	scheme.AddKnownTypes(configv1.GroupVersion, &configv1.ClusterVersion{})
	scheme.AddKnownTypes(pipelinesv1alpha1.SchemeGroupVersion, &pipelinesv1alpha1.GitopsService{})
	scheme.AddKnownTypes(routev1.GroupVersion, &routev1.Route{})
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
