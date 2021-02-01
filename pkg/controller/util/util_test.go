package util

import (
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGetClusterVersion(t *testing.T) {
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	t.Run("Valid Cluster Version", func(t *testing.T) {
		version := "4.7.1"
		fakeClient := fake.NewFakeClient(NewClusterVersion(version))
		clusterVersion, err := GetClusterVersion(fakeClient)
		assertNoError(t, err)
		if clusterVersion != version {
			t.Fatalf("got %s, want %s", clusterVersion, version)
		}
	})
	t.Run("Cluster Version not found", func(t *testing.T) {
		fakeClient := fake.NewFakeClient()
		clusterVersion, err := GetClusterVersion(fakeClient)
		assertNoError(t, err)
		if clusterVersion != "" {
			t.Fatalf("got %s, want %s", clusterVersion, "")
		}
	})
}

func addKnownTypesToScheme(scheme *runtime.Scheme) {
	scheme.AddKnownTypes(configv1.GroupVersion, &configv1.ClusterVersion{})
}

func assertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
