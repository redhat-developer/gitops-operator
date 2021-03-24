package argocd

import (
	"context"
	"net/url"
	"testing"

	"github.com/google/go-cmp/cmp"
	configv1 "github.com/openshift/api/config/v1"
	console "github.com/openshift/api/console/v1"
	routev1 "github.com/openshift/api/route/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	argocdInstanceName = "openshift-gitops"
)

var (
	argoCDRoute = &routev1.Route{
		ObjectMeta: v1.ObjectMeta{
			Name:      argocdRouteName,
			Namespace: argocdNS,
		},
		Spec: routev1.RouteSpec{
			Host: "test.com",
		},
	}

	consoleLink = &console.ConsoleLink{
		ObjectMeta: v1.ObjectMeta{
			Name:      consoleLinkName,
			Namespace: argocdNS,
		},
	}
)

func TestReconcile_create_consolelink(t *testing.T) {
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewFakeClient(argoCDRoute)

	reconcileArgoCD := newFakeReconcileArgoCD(fakeClient, s)
	want := newConsoleLink("https://test.com", "ArgoCD")

	result, err := reconcileArgoCD.Reconcile(newRequest(argocdNS, argocdInstanceName))
	assertConsoleLinkExists(t, fakeClient, reconcileResult{result, err}, want)
}

func TestReconcile_delete_consolelink(t *testing.T) {
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	fakeClient := fake.NewFakeClient(argoCDRoute, consoleLink)
	reconcileArgoCD := newFakeReconcileArgoCD(fakeClient, s)

	err := fakeClient.Delete(context.TODO(), &routev1.Route{ObjectMeta: v1.ObjectMeta{Name: argocdRouteName, Namespace: argocdNS}})
	assertNoError(t, err)

	result, err := reconcileArgoCD.Reconcile(newRequest(argocdNS, argocdRouteName))
	assertConsoleLinkDeletion(t, fakeClient, reconcileResult{result, err})
}

func TestReconcile_update_consolelink(t *testing.T) {
	s := scheme.Scheme
	addKnownTypesToScheme(s)
	fakeClient := fake.NewFakeClient(argoCDRoute, consoleLink)
	reconcileArgoCD := newFakeReconcileArgoCD(fakeClient, s)

	argoCDRoute.Spec.Host = "updated-test.com"
	err := fakeClient.Update(context.TODO(), argoCDRoute)
	assertNoError(t, err)

	_, err = reconcileArgoCD.Reconcile(newRequest(argocdNS, argocdRouteName))
	assertNoError(t, err)

	cl, err := getConsoleLink(fakeClient)
	assertNoError(t, err)
	url, err := url.Parse(cl.Spec.Href)
	assertNoError(t, err)
	if diff := cmp.Diff(argoCDRoute.Spec.Host, url.Hostname()); diff != "" {
		t.Fatalf("ConsoleLink URL mismatch: %v", diff)
	}
}

func newFakeReconcileArgoCD(client client.Client, scheme *runtime.Scheme) *ReconcileArgoCDRoute {
	return &ReconcileArgoCDRoute{
		client: client,
		scheme: scheme,
	}
}

func assertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func addKnownTypesToScheme(scheme *runtime.Scheme) {
	scheme.AddKnownTypes(routev1.GroupVersion, &routev1.Route{})
	scheme.AddKnownTypes(console.GroupVersion, &console.ConsoleLink{})
	scheme.AddKnownTypes(configv1.GroupVersion, &configv1.ClusterVersion{})
}

func newRequest(namespace, name string) reconcile.Request {
	return reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		},
	}
}

func getConsoleLink(c client.Client) (*console.ConsoleLink, error) {
	cl := &console.ConsoleLink{}
	err := c.Get(context.TODO(), types.NamespacedName{Name: consoleLinkName}, cl)
	if err != nil {
		return nil, err
	}
	return cl, nil
}

func assertConsoleLinkExists(t *testing.T, c client.Client, r reconcileResult, want *console.ConsoleLink) {
	t.Helper()
	assertNoError(t, r.err)

	if r.result.Requeue {
		t.Fatalf("Expected ConsoleLink to be deleted without requeuing")
	}

	got, err := getConsoleLink(c)
	assertNoError(t, err)
	if diff := cmp.Diff(want.Spec, got.Spec); diff != "" {
		t.Fatalf("ConsoleLink mismatch: %v", diff)
	}
}

func assertConsoleLinkDeletion(t *testing.T, c client.Client, r reconcileResult) {
	t.Helper()
	assertNoError(t, r.err)

	if r.result.Requeue {
		t.Fatalf("Expected ConsoleLink to be created without requeuing")
	}

	_, err := getConsoleLink(c)

	wantErr := `consolelinks.console.openshift.io "argocd" not found`
	if err == nil {
		t.Fatalf("was expecting an error %s, but got nil", wantErr)
	}

	if err.Error() != wantErr {
		t.Fatalf("got %s, want %s", err, wantErr)
	}
}

type reconcileResult struct {
	result reconcile.Result
	err    error
}
