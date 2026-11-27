package argocd

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	testclient "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	argoproj "github.com/redhat-developer/gitops-operator/argocd-operator/api/v1beta1"
	"github.com/redhat-developer/gitops-operator/argocd-operator/common"
)

// Withdrawing consent (deleting or retargeting a NamespaceManagement CR) must enqueue the
// instance that held it, even when the predicate skips or fails its own cleanup, so the
// reconcile can prune what that instance was granted.
func TestNamespaceManagementPredicateEnqueuesOnConsentWithdrawal(t *testing.T) {
	const (
		manager = "manager"
		target  = "target"
	)
	nm := func(managedBy string) *argoproj.NamespaceManagement {
		return &argoproj.NamespaceManagement{
			ObjectMeta: metav1.ObjectMeta{Name: "nm", Namespace: target},
			Spec:       argoproj.NamespaceManagementSpec{ManagedBy: managedBy},
		}
	}
	selfLabelled := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name:   manager,
		Labels: map[string]string{common.ArgoCDManagedByLabel: manager},
	}}

	tests := []struct {
		name string
		// old is nil for a delete event.
		old, cur    *argoproj.NamespaceManagement
		namespaces  []client.Object
		wantEnqueue bool
	}{
		{
			name:        "delete enqueues when the managing namespace carries a matching managed-by label",
			cur:         nm(manager),
			namespaces:  []client.Object{selfLabelled},
			wantEnqueue: true,
		},
		{
			name:        "delete enqueues when the managing namespace cannot be read",
			cur:         nm(manager),
			wantEnqueue: true,
		},
		{
			name:        "delete without .spec.managedBy has no instance to enqueue",
			cur:         nm(""),
			wantEnqueue: false,
		},
		{
			name:        "retarget enqueues when the managing namespace carries a matching managed-by label",
			old:         nm(manager),
			cur:         nm("other"),
			namespaces:  []client.Object{selfLabelled},
			wantEnqueue: true,
		},
		{
			name:        "retarget enqueues when the managing namespace cannot be read",
			old:         nm(manager),
			cur:         nm("other"),
			wantEnqueue: true,
		},
		{
			// Our own reconcile writes the CR status; enqueuing it would loop.
			name:        "status-only update is not enqueued",
			old:         nm(manager),
			cur:         nm(manager),
			namespaces:  []client.Object{&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: manager}}},
			wantEnqueue: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sch := makeTestReconcilerScheme(argoproj.AddToScheme)
			cl := makeTestReconcilerClient(sch, tt.namespaces, nil, []runtime.Object{})
			r := makeTestReconciler(cl, sch, testclient.NewSimpleClientset())

			pred := r.namespaceManagementFilterPredicate()
			var got bool
			if tt.old == nil {
				got = pred.Delete(event.DeleteEvent{Object: tt.cur})
			} else {
				got = pred.Update(event.UpdateEvent{ObjectOld: tt.old, ObjectNew: tt.cur})
			}
			assert.Equal(t, tt.wantEnqueue, got)
		})
	}
}

// Once the instance is enqueued after a NamespaceManagement CR is deleted, its reconcile must
// remove the Image Updater manager RBAC that the CR's consent allowed.
func TestImageUpdaterRBACRemovedAfterNamespaceManagementConsentWithdrawn(t *testing.T) {
	t.Setenv(common.EnableManagedNamespace, "true")
	const target = "team-a"

	a := makeTestArgoCD(func(a *argoproj.ArgoCD) {
		a.Spec.ImageUpdater.Enabled = true
		a.Spec.ImageUpdater.Env = []corev1.EnvVar{{Name: "IMAGE_UPDATER_WATCH_NAMESPACES", Value: target}}
		a.Spec.NamespaceManagement = []argoproj.ManagedNamespaces{{Name: target, AllowManagedBy: true}}
	})
	nm := &argoproj.NamespaceManagement{
		ObjectMeta: metav1.ObjectMeta{Name: "nm", Namespace: target},
		Spec:       argoproj.NamespaceManagementSpec{ManagedBy: a.Namespace},
	}
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: target}}

	sch := makeTestReconcilerScheme(argoproj.AddToScheme)
	cl := makeTestReconcilerClient(sch, []client.Object{a, nm, ns}, []client.Object{a, nm}, []runtime.Object{})
	r := makeTestReconciler(cl, sch, testclient.NewSimpleClientset())
	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "sa", Namespace: a.Namespace}}

	// The consent steps of a reconcile, followed by the Image Updater RBAC.
	reconcileRBAC := func() {
		t.Helper()
		require.NoError(t, r.setManagedNamespaces(a))
		require.NoError(t, r.reconcileNamespaceManagement(a))
		ws, expanded := testResolveWatchNamespaces(t, r, a)
		_, err := r.reconcileImageUpdaterRBAC(a, sa, ws, expanded)
		require.NoError(t, err)
	}
	roleKey := types.NamespacedName{Name: getRoleNameForApplicationSourceNamespaces(target, a), Namespace: target}
	bindingKey := types.NamespacedName{Name: getRoleBindingNameForSourceNamespaces(a.Name, target), Namespace: target}

	reconcileRBAC()
	require.NoError(t, r.Get(context.TODO(), roleKey, &rbacv1.Role{}), "consent should grant the manager Role")
	require.NoError(t, r.Get(context.TODO(), bindingKey, &rbacv1.RoleBinding{}), "consent should grant the manager RoleBinding")

	require.NoError(t, r.Delete(context.TODO(), nm))
	reconcileRBAC()
	assert.True(t, errors.IsNotFound(r.Get(context.TODO(), roleKey, &rbacv1.Role{})), "manager Role must be removed once consent is withdrawn")
	assert.True(t, errors.IsNotFound(r.Get(context.TODO(), bindingKey, &rbacv1.RoleBinding{})), "manager RoleBinding must be removed once consent is withdrawn")
}
