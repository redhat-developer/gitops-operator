package dependency

import (
	"context"
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"

	v1 "github.com/operator-framework/api/pkg/operators/v1"
	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestInstall(t *testing.T) {
	s := scheme.Scheme
	addDependencyTypesToScheme(s)
	fc := fake.NewFakeClient()
	dependency := fakeDependencyClient(fc, "test")

	err := dependency.Install()
	assertNoError(t, err)

	// Check if namepace, operatorGroup and subscription is created for argocd operator
	argocdOperator := newArgoCDOperator("test")
	assertOperatorCreation(t, fc, argocdOperator)

	// Check if namepace, operatorGroup and subscription is created for sealed-secrets operator
	sealedSecretsOperator := newSealedSecretsOperator("test")
	assertOperatorCreation(t, fc, sealedSecretsOperator)
}

func TestCreateResourceIfAbsent(t *testing.T) {
	s := scheme.Scheme
	addDependencyTypesToScheme(s)
	resource := newOperatorGroup("test", "test-group")
	fc := fake.NewFakeClient(resource)
	dc := fakeDependencyClient(fc, "")
	ctx := context.Background()

	t.Run("Resource don't exist", func(t *testing.T) {
		sub := newSubscription("test", "test-subscription")
		err := dc.createResourceIfAbsent(ctx, sub, types.NamespacedName{Name: sub.Name, Namespace: sub.Namespace})
		assertNoError(t, err)
		assertResourceExists(t, fc, types.NamespacedName{Name: sub.Name, Namespace: sub.Namespace}, sub)
	})

	t.Run("Resource already exist", func(t *testing.T) {
		err := dc.createResourceIfAbsent(ctx, resource, types.NamespacedName{Name: resource.Name, Namespace: resource.Namespace})
		assertNoError(t, err)
	})
}

func TestIsOperatorReady_PhaseFailed(t *testing.T) {
	s := scheme.Scheme
	addDependencyTypesToScheme(s)
	fakeClient := fake.NewFakeClient(&v1alpha1.ClusterServiceVersion{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test-ns",
		},
		Status: v1alpha1.ClusterServiceVersionStatus{
			Phase:  v1alpha1.CSVPhaseFailed,
			Reason: "Install plan failed",
		},
	})
	wf := isOperatorReady(context.TODO(), fakeClient, types.NamespacedName{Name: "test", Namespace: "test-ns"})
	ready, err := wf()
	if ready {
		t.Fatalf("Expected the CSV to be in failed phase: got %v, want %v", ready, false)
	}
	wantErr := fmt.Errorf("Operator installation failed: Install plan failed")
	if err.Error() != wantErr.Error() {
		t.Fatalf("got %s, want %s", err, wantErr)
	}
}

func TestIsOperatorReady_PhaseSucceeded(t *testing.T) {
	s := scheme.Scheme
	addDependencyTypesToScheme(s)
	fakeClient := fake.NewFakeClient(&v1alpha1.ClusterServiceVersion{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test-ns",
		},
		Status: v1alpha1.ClusterServiceVersionStatus{
			Phase:  v1alpha1.CSVPhaseSucceeded,
			Reason: "Operator installed successfully",
		},
	})
	wf := isOperatorReady(context.TODO(), fakeClient, types.NamespacedName{Name: "test", Namespace: "test-ns"})
	ready, err := wf()
	if !ready {
		t.Fatalf("Expected the CSV to be in succeeded phase: got %v, want %v", ready, true)
	}
	if err != nil {
		t.Fatalf("got %v, want %v", err, nil)
	}
}

func TestIsOperatorReady_PhaseUnkown(t *testing.T) {
	s := scheme.Scheme
	addDependencyTypesToScheme(s)
	fakeClient := fake.NewFakeClient(&v1alpha1.ClusterServiceVersion{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test-ns",
		},
	})
	wf := isOperatorReady(context.TODO(), fakeClient, types.NamespacedName{Name: "test", Namespace: "test-ns"})
	ready, err := wf()
	if ready {
		t.Fatalf("Expected the CSV to be in unknown phase: got %v, want %v", ready, false)
	}
	if err != nil {
		t.Fatalf("got %v, want %v", err, nil)
	}
}

func assertResourceExists(t *testing.T, client client.Client, ns types.NamespacedName, resource runtime.Object) {
	t.Helper()
	err := client.Get(context.TODO(), ns, resource)
	if err != nil {
		if errors.IsNotFound(err) {
			t.Fatalf("Expected the resource to exist: %s", ns.Name)
		}
		t.Fatalf("Failed to fetch resource: %v", err)
	}
}

func addDependencyTypesToScheme(scheme *runtime.Scheme) {
	scheme.AddKnownTypes(v1.GroupVersion, &v1.OperatorGroup{})
	scheme.AddKnownTypes(v1alpha1.SchemeGroupVersion, &v1alpha1.Subscription{})
	scheme.AddKnownTypes(v1alpha1.SchemeGroupVersion, &v1alpha1.ClusterServiceVersion{})
}

func assertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func assertOperatorCreation(t *testing.T, client client.Client, operator operatorResource) {
	t.Helper()
	ns := operator.GetNamespace()
	assertResourceExists(t, client, types.NamespacedName{Name: ns.Name, Namespace: ns.Namespace}, ns)

	og := operator.GetOperatorGroup()
	assertResourceExists(t, client, types.NamespacedName{Name: og.Name, Namespace: og.Namespace}, og)

	sub := operator.GetSubscription()
	assertResourceExists(t, client, types.NamespacedName{Name: sub.Name, Namespace: sub.Namespace}, sub)
}

func fakeDependencyClient(client client.Client, prefix string) *Dependency {
	return &Dependency{
		client: client,
		isReady: func() (bool, error) {
			return true, nil
		},
		log:    log.WithName("GitOps Dependencies"),
		prefix: prefix,
	}
}
