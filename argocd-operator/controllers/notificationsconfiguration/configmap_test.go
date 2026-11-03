package notificationsconfiguration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-logr/logr/funcr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/redhat-developer/gitops-operator/argocd-operator/api/v1alpha1"
	"github.com/redhat-developer/gitops-operator/argocd-operator/controllers/argoutil"
)

// ---------------------------------------------------------------------------
// Test Helpers
// ---------------------------------------------------------------------------

func testScheme() *runtime.Scheme {
	s := scheme.Scheme
	_ = v1alpha1.AddToScheme(s)
	return s
}

func testReconciler(cl client.Client) *NotificationsConfigurationReconciler {
	return &NotificationsConfigurationReconciler{
		Client: cl,
		Scheme: testScheme(),
	}
}

func testClient(objs ...client.Object) client.Client {
	return fake.NewClientBuilder().
		WithScheme(testScheme()).
		WithObjects(objs...).
		Build()
}

func testClientWithInterceptors(fns interceptor.Funcs, objs ...client.Object) client.Client {
	return fake.NewClientBuilder().
		WithScheme(testScheme()).
		WithObjects(objs...).
		WithInterceptorFuncs(fns).
		Build()
}

func reconcileReq(namespace string) ctrl.Request {
	return ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: namespace,
			Name:      ArgoCDNotificationsConfigMap,
		},
	}
}

func newTestCR(name, namespace string, spec v1alpha1.NotificationsConfigurationSpec) *v1alpha1.NotificationsConfiguration {
	return &v1alpha1.NotificationsConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: spec,
	}
}

func getConfigMap(t *testing.T, cl client.Client, namespace string) *corev1.ConfigMap {
	t.Helper()
	cm := &corev1.ConfigMap{}
	err := cl.Get(context.TODO(), types.NamespacedName{
		Namespace: namespace,
		Name:      ArgoCDNotificationsConfigMap,
	}, cm)
	require.NoError(t, err)
	return cm
}

func configMapExists(cl client.Client, namespace string) bool {
	cm := &corev1.ConfigMap{}
	err := cl.Get(context.TODO(), types.NamespacedName{
		Namespace: namespace,
		Name:      ArgoCDNotificationsConfigMap,
	}, cm)
	return err == nil
}

func managedCM(namespace string, data map[string]string, ownerRefs ...metav1.OwnerReference) *corev1.ConfigMap {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:            ArgoCDNotificationsConfigMap,
			Namespace:       namespace,
			OwnerReferences: ownerRefs,
		},
		Data: data,
	}
	argoutil.AddTrackedByOperatorLabel(&cm.ObjectMeta)
	return cm
}

func boolPtr(b bool) *bool { return &b }

// legacyRef builds an owner reference from the old single-CR reconciler.
func legacyRef(name, uid string) metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion:         "argoproj.io/v1alpha1",
		Kind:               "NotificationsConfiguration",
		Name:               name,
		UID:                types.UID(uid),
		Controller:         boolPtr(true),
		BlockOwnerDeletion: boolPtr(true),
	}
}

type writeTracker struct {
	updateCalled bool
	patchCalled  bool
	deleteCalled bool
}

// noWriteInterceptors records ConfigMap update, patch, and delete calls.
func noWriteInterceptors(tracker *writeTracker) interceptor.Funcs {
	return interceptor.Funcs{
		Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			if _, ok := obj.(*corev1.ConfigMap); ok {
				tracker.updateCalled = true
			}
			return c.Update(ctx, obj, opts...)
		},
		Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			if _, ok := obj.(*corev1.ConfigMap); ok {
				tracker.patchCalled = true
			}
			return c.Patch(ctx, obj, patch, opts...)
		},
		Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			if _, ok := obj.(*corev1.ConfigMap); ok {
				tracker.deleteCalled = true
			}
			return c.Delete(ctx, obj, opts...)
		},
	}
}

// ---------------------------------------------------------------------------
// A. Event Routing
// ---------------------------------------------------------------------------

func TestEventRouting_CRsMapToConfigMapKey(t *testing.T) {
	cr := &v1alpha1.NotificationsConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "some-cr", Namespace: "test-ns"},
	}
	reqs := mapCRToConfigMapRequest(context.TODO(), cr)
	require.Len(t, reqs, 1)
	assert.Equal(t, "test-ns", reqs[0].Namespace)
	assert.Equal(t, ArgoCDNotificationsConfigMap, reqs[0].Name)
}

func TestEventRouting_DifferentCRNamesSameNamespaceMapToSameKey(t *testing.T) {
	crA := &v1alpha1.NotificationsConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "cr-a", Namespace: "ns1"},
	}
	crB := &v1alpha1.NotificationsConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "cr-b", Namespace: "ns1"},
	}
	reqsA := mapCRToConfigMapRequest(context.TODO(), crA)
	reqsB := mapCRToConfigMapRequest(context.TODO(), crB)
	require.Len(t, reqsA, 1)
	require.Len(t, reqsB, 1)
	assert.Equal(t, reqsA[0], reqsB[0], "different CR names in same namespace should map to same key")
}

func TestEventRouting_DifferentNamespacesMapToDifferentKeys(t *testing.T) {
	crNs1 := &v1alpha1.NotificationsConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "cr", Namespace: "ns1"},
	}
	crNs2 := &v1alpha1.NotificationsConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "cr", Namespace: "ns2"},
	}
	reqs1 := mapCRToConfigMapRequest(context.TODO(), crNs1)
	reqs2 := mapCRToConfigMapRequest(context.TODO(), crNs2)
	require.Len(t, reqs1, 1)
	require.Len(t, reqs2, 1)
	assert.NotEqual(t, reqs1[0].Namespace, reqs2[0].Namespace)
}

func TestEventRouting_TargetConfigMapMapsToNamespaceKey(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: ArgoCDNotificationsConfigMap, Namespace: "test-ns"},
	}
	reqs := mapConfigMapToReconcileRequest(context.TODO(), cm)
	require.Len(t, reqs, 1)
	assert.Equal(t, "test-ns", reqs[0].Namespace)
	assert.Equal(t, ArgoCDNotificationsConfigMap, reqs[0].Name)
}

func TestEventRouting_UnrelatedConfigMapIgnored(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "some-other-cm", Namespace: "test-ns"},
	}
	reqs := mapConfigMapToReconcileRequest(context.TODO(), cm)
	assert.Empty(t, reqs, "unrelated ConfigMap should not produce any reconcile requests")
}

func TestEventRouting_ClusterScopedCRIgnored(t *testing.T) {
	cr := &v1alpha1.NotificationsConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "no-ns"},
	}
	reqs := mapCRToConfigMapRequest(context.TODO(), cr)
	assert.Empty(t, reqs, "cluster-scoped object (no namespace) should be ignored")
}

// ---------------------------------------------------------------------------
// B. Aggregation through the actual Reconcile entry point
// ---------------------------------------------------------------------------

func TestReconcile_OneCR_CreatesConfigMap(t *testing.T) {
	cr := newTestCR(DefaultNotificationsConfigurationName, "default",
		v1alpha1.NotificationsConfigurationSpec{
			Templates: map[string]string{"template.app-created": "email:\n  subject: Created"},
			Triggers:  map[string]string{"trigger.on-created": "- when: \"true\""},
		})

	cl := testClient(cr)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.NoError(t, err)

	cm := getConfigMap(t, cl, "default")
	assert.True(t, argoutil.IsTrackedByOperator(cm.Labels))
	assert.Empty(t, cm.OwnerReferences, "new ConfigMap must have no individual CR owner reference")
	assert.Equal(t, "email:\n  subject: Created", cm.Data["template.app-created"])
	assert.Equal(t, "- when: \"true\"", cm.Data["trigger.on-created"])
}

func TestReconcile_TwoCRs_ContributeTogether(t *testing.T) {
	crA := newTestCR("cr-a", "default",
		v1alpha1.NotificationsConfigurationSpec{
			Triggers: map[string]string{"trigger.from-a": "when: true"},
		})
	crB := newTestCR("cr-b", "default",
		v1alpha1.NotificationsConfigurationSpec{
			Triggers: map[string]string{"trigger.from-b": "when: false"},
		})

	cl := testClient(crA, crB)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.NoError(t, err)

	cm := getConfigMap(t, cl, "default")
	assert.Equal(t, "when: true", cm.Data["trigger.from-a"], "crA's trigger should be present")
	assert.Equal(t, "when: false", cm.Data["trigger.from-b"], "crB's trigger should also be present")
}

func TestReconcile_DuplicateKeyPrecedence(t *testing.T) {
	older := metav1.NewTime(time.Now().Add(-time.Hour))
	newer := metav1.NewTime(time.Now())

	crOlder := newTestCR("cr-older", "default",
		v1alpha1.NotificationsConfigurationSpec{
			Triggers: map[string]string{"trigger.shared": "older-value"},
		})
	crOlder.CreationTimestamp = older

	crNewer := newTestCR("cr-newer", "default",
		v1alpha1.NotificationsConfigurationSpec{
			Triggers: map[string]string{"trigger.shared": "newer-value"},
		})
	crNewer.CreationTimestamp = newer

	cl := testClient(crOlder, crNewer)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.NoError(t, err)

	cm := getConfigMap(t, cl, "default")
	assert.Equal(t, "older-value", cm.Data["trigger.shared"],
		"oldest CR should win by precedence")
}

func TestReconcile_TiedConflictErrorOmitsValues(t *testing.T) {
	timestamp := metav1.NewTime(time.Now())
	crA := newTestCR("cr-a", "default", v1alpha1.NotificationsConfigurationSpec{
		Services: map[string]string{"service.webhook": "token: selected-secret"},
	})
	crB := newTestCR("cr-b", "default", v1alpha1.NotificationsConfigurationSpec{
		Services: map[string]string{"service.webhook": "token: conflicting-secret"},
	})
	crA.CreationTimestamp = timestamp
	crB.CreationTimestamp = timestamp
	r := testReconciler(testClient(crB, crA))

	var output string
	logger := funcr.NewJSON(func(entry string) { output += entry + "\n" }, funcr.Options{})
	_, err := r.Reconcile(ctrl.LoggerInto(context.TODO(), logger), reconcileReq("default"))
	require.NoError(t, err)

	assert.Contains(t, output, "merge conflict: equal creation timestamps")
	assert.Contains(t, output, `"error":`)
	assert.Contains(t, output, `"key":"service.webhook"`)
	assert.Contains(t, output, `"existingCR":"default/cr-a"`)
	assert.Contains(t, output, `"conflictingCR":"default/cr-b"`)
	assert.NotContains(t, output, "selected-secret")
	assert.NotContains(t, output, "conflicting-secret")
	assert.NotContains(t, getConfigMap(t, r.Client, "default").Data, "service.webhook")
}

func TestReconcile_TimestampTieRetainsExistingKeyUntilResolved(t *testing.T) {
	for _, existing := range []string{"absent", "matching", "unmatched"} {
		for _, resolution := range []string{"edit", "delete"} {
			t.Run(fmt.Sprintf("existing=%s/%s", existing, resolution), func(t *testing.T) {
				timestamp := metav1.NewTime(time.Now())
				a := newTestCR("a", "default", v1alpha1.NotificationsConfigurationSpec{
					Triggers: map[string]string{"trigger.shared": "a", "trigger.valid": "new"},
					Context:  map[string]string{"region": "east", "environment": "new"},
				})
				b := newTestCR("b", "default", v1alpha1.NotificationsConfigurationSpec{
					Triggers: map[string]string{"trigger.shared": "b"},
					Context:  map[string]string{"region": "west"},
				})
				a.CreationTimestamp, b.CreationTimestamp = timestamp, timestamp
				objects := []client.Object{a, b}
				if existing != "absent" {
					value, context := "stored", "region: stored\n"
					if existing == "matching" {
						value, context = "b", "region: west\n"
					}
					objects = append(objects, managedCM("default", map[string]string{
						"trigger.shared": value, "context": context, "stale": "remove",
					}))
				}
				cl := testClient(objects...)
				r := testReconciler(cl)
				for i := 0; i < 2; i++ {
					_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
					require.NoError(t, err)
					cm := getConfigMap(t, cl, "default")
					if existing == "matching" {
						assert.Equal(t, "b", cm.Data["trigger.shared"])
						assert.Equal(t, "west", decodeContext(t, cm.Data)["region"])
					} else {
						assert.NotContains(t, cm.Data, "trigger.shared")
						assert.NotContains(t, decodeContext(t, cm.Data), "region")
					}
					assert.Equal(t, "new", cm.Data["trigger.valid"])
					assert.Equal(t, "new", decodeContext(t, cm.Data)["environment"])
					assert.NotContains(t, cm.Data, "stale")
				}
				if resolution == "delete" {
					require.NoError(t, cl.Delete(context.TODO(), b))
				} else {
					current := &v1alpha1.NotificationsConfiguration{}
					require.NoError(t, cl.Get(context.TODO(), client.ObjectKeyFromObject(b), current))
					current.Spec = a.Spec
					require.NoError(t, cl.Update(context.TODO(), current))
				}
				_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
				require.NoError(t, err)
				cm := getConfigMap(t, cl, "default")
				assert.Equal(t, "a", cm.Data["trigger.shared"])
				assert.Equal(t, "east", decodeContext(t, cm.Data)["region"])
			})
		}
	}
}

func TestReconcile_UnreadableContextDoesNotBlockOtherKeys(t *testing.T) {
	timestamp := metav1.NewTime(time.Now())
	a := newTestCR("a", "default", v1alpha1.NotificationsConfigurationSpec{
		Context:  map[string]string{"region": "east", "environment": "new"},
		Triggers: map[string]string{"trigger.valid": "new-trigger"},
	})
	b := newTestCR("b", "default", v1alpha1.NotificationsConfigurationSpec{
		Context: map[string]string{"region": "west"},
	})
	a.CreationTimestamp, b.CreationTimestamp = timestamp, timestamp
	raw := "region: old\nunrelated: [stored-secret]\n"
	cl := testClient(a, b, managedCM("default", map[string]string{
		"context": raw, "trigger.valid": "old-trigger", "stale": "remove",
	}))
	r := testReconciler(cl)
	var output string
	logger := funcr.NewJSON(func(entry string) { output += entry + "\n" }, funcr.Options{})
	ctx := ctrl.LoggerInto(context.TODO(), logger)

	_, err := r.Reconcile(ctx, reconcileReq("default"))
	require.NoError(t, err)
	cm := getConfigMap(t, cl, "default")
	assert.Equal(t, map[string]string{"environment": "new"}, decodeContext(t, cm.Data))
	assert.Equal(t, "new-trigger", cm.Data["trigger.valid"])
	assert.NotContains(t, cm.Data, "stale")
	assert.Contains(t, output, "existing context is not a single YAML string mapping")
	assert.Contains(t, output, `"configMap":"default/argocd-notifications-cm"`)
	assert.NotContains(t, output, "stored-secret")

	version := cm.ResourceVersion
	_, err = r.Reconcile(ctx, reconcileReq("default"))
	require.NoError(t, err)
	assert.Equal(t, version, getConfigMap(t, cl, "default").ResourceVersion)

	// A readable stored value is retained only when a tied CR defines it.
	cm.Data["context"] = "region: west\n"
	require.NoError(t, cl.Update(context.TODO(), cm))
	output = ""
	_, err = r.Reconcile(ctx, reconcileReq("default"))
	require.NoError(t, err)
	cm = getConfigMap(t, cl, "default")
	assert.Equal(t, map[string]string{"region": "west", "environment": "new"}, decodeContext(t, cm.Data))
	assert.NotContains(t, output, "existing context is not a single YAML string mapping")

	// Resolving the CR conflict also replaces unreadable stored context normally.
	cm.Data["context"] = raw
	require.NoError(t, cl.Update(context.TODO(), cm))
	current := &v1alpha1.NotificationsConfiguration{}
	require.NoError(t, cl.Get(context.TODO(), client.ObjectKeyFromObject(b), current))
	current.Spec.Context = a.Spec.Context
	require.NoError(t, cl.Update(context.TODO(), current))
	_, err = r.Reconcile(ctx, reconcileReq("default"))
	require.NoError(t, err)
	assert.Equal(t, a.Spec.Context, decodeContext(t, getConfigMap(t, cl, "default").Data))
}

func TestReconcile_UpdateKeyChangesConfigMap(t *testing.T) {
	cr := newTestCR(DefaultNotificationsConfigurationName, "default",
		v1alpha1.NotificationsConfigurationSpec{
			Triggers: map[string]string{"trigger.x": "original"},
		})

	cl := testClient(cr)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.NoError(t, err)
	cm := getConfigMap(t, cl, "default")
	assert.Equal(t, "original", cm.Data["trigger.x"])

	cr.Spec.Triggers["trigger.x"] = "updated"
	cr.Spec.Triggers["trigger.new"] = "new-value"
	require.NoError(t, cl.Update(context.TODO(), cr))

	_, err = r.Reconcile(context.TODO(), reconcileReq("default"))
	require.NoError(t, err)

	cm = getConfigMap(t, cl, "default")
	assert.Equal(t, "updated", cm.Data["trigger.x"])
	assert.Equal(t, "new-value", cm.Data["trigger.new"])
}

func TestReconcile_RemoveKeyChangesConfigMap(t *testing.T) {
	cr := newTestCR(DefaultNotificationsConfigurationName, "default",
		v1alpha1.NotificationsConfigurationSpec{
			Triggers: map[string]string{
				"trigger.alpha": "when: true",
				"trigger.beta":  "when: false",
			},
		})

	cl := testClient(cr)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.NoError(t, err)
	cm := getConfigMap(t, cl, "default")
	assert.Equal(t, "when: true", cm.Data["trigger.alpha"])
	assert.Equal(t, "when: false", cm.Data["trigger.beta"])

	cr.Spec.Triggers = map[string]string{"trigger.beta": "when: false"}
	require.NoError(t, cl.Update(context.TODO(), cr))

	_, err = r.Reconcile(context.TODO(), reconcileReq("default"))
	require.NoError(t, err)

	cm = getConfigMap(t, cl, "default")
	_, hasAlpha := cm.Data["trigger.alpha"]
	assert.False(t, hasAlpha, "removed key should be absent after reconcile")
	assert.Equal(t, "when: false", cm.Data["trigger.beta"])
}

func TestReconcile_DeleteExistingCRRevealsConflicting(t *testing.T) {
	older := metav1.NewTime(time.Now().Add(-time.Hour))
	newer := metav1.NewTime(time.Now())

	crExisting := newTestCR("cr-older", "default",
		v1alpha1.NotificationsConfigurationSpec{
			Triggers: map[string]string{"trigger.shared": "existing-value"},
		})
	crExisting.CreationTimestamp = older

	crConflicting := newTestCR("cr-newer", "default",
		v1alpha1.NotificationsConfigurationSpec{
			Triggers: map[string]string{"trigger.shared": "conflicting-value"},
		})
	crConflicting.CreationTimestamp = newer

	cl := testClient(crExisting, crConflicting)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.NoError(t, err)
	cm := getConfigMap(t, cl, "default")
	assert.Equal(t, "existing-value", cm.Data["trigger.shared"])

	require.NoError(t, cl.Delete(context.TODO(), crExisting))

	_, err = r.Reconcile(context.TODO(), reconcileReq("default"))
	require.NoError(t, err)

	cm = getConfigMap(t, cl, "default")
	assert.Equal(t, "conflicting-value", cm.Data["trigger.shared"],
		"after deleting the existing CR, the conflicting CR's value should be revealed")
}

func TestReconcile_DeletingCRsDoNotContribute(t *testing.T) {
	now := metav1.NewTime(time.Now())
	crDeleting := newTestCR("deleting", "default",
		v1alpha1.NotificationsConfigurationSpec{
			Triggers: map[string]string{"trigger.x": "should-not-appear"},
		})
	crDeleting.DeletionTimestamp = &now
	crDeleting.Finalizers = []string{"fake-finalizer"}

	crActive := newTestCR("active", "default",
		v1alpha1.NotificationsConfigurationSpec{
			Triggers: map[string]string{"trigger.y": "should-appear"},
		})

	cl := testClient(crDeleting, crActive)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.NoError(t, err)

	cm := getConfigMap(t, cl, "default")
	_, hasX := cm.Data["trigger.x"]
	assert.False(t, hasX, "deleting CR's keys must not appear")
	assert.Equal(t, "should-appear", cm.Data["trigger.y"])
}

func TestReconcile_EmptySpecCRDistinguishedFromAbsent(t *testing.T) {
	cr := newTestCR("empty-spec", "default", v1alpha1.NotificationsConfigurationSpec{})

	cl := testClient(cr)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.NoError(t, err)

	assert.True(t, configMapExists(cl, "default"),
		"empty-spec CR should still cause ConfigMap to exist")
}

func TestReconcile_EmptySpecWithNilConfigMapData_NoWrite(t *testing.T) {
	cr := newTestCR("empty-spec", "default", v1alpha1.NotificationsConfigurationSpec{})
	tracker := &writeTracker{}
	cl := testClientWithInterceptors(noWriteInterceptors(tracker), cr, managedCM("default", nil))
	r := testReconciler(cl)

	for i := 0; i < 2; i++ {
		_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
		require.NoError(t, err)
	}

	assert.False(t, tracker.updateCalled, "nil and empty data should be treated as equal")
	assert.False(t, tracker.patchCalled)
	assert.False(t, tracker.deleteCalled)
}

func TestReconcile_NamespacesIsolated(t *testing.T) {
	crNs1 := newTestCR("cr-ns1", "ns1",
		v1alpha1.NotificationsConfigurationSpec{
			Triggers: map[string]string{"trigger.a": "ns1-value"},
		})
	crNs2 := newTestCR("cr-ns2", "ns2",
		v1alpha1.NotificationsConfigurationSpec{
			Triggers: map[string]string{"trigger.b": "ns2-value"},
		})

	cl := testClient(crNs1, crNs2)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("ns1"))
	require.NoError(t, err)
	_, err = r.Reconcile(context.TODO(), reconcileReq("ns2"))
	require.NoError(t, err)

	cm1 := getConfigMap(t, cl, "ns1")
	cm2 := getConfigMap(t, cl, "ns2")

	assert.Equal(t, "ns1-value", cm1.Data["trigger.a"])
	_, hasB := cm1.Data["trigger.b"]
	assert.False(t, hasB, "ns2's trigger should not leak into ns1")

	assert.Equal(t, "ns2-value", cm2.Data["trigger.b"])
	_, hasA := cm2.Data["trigger.a"]
	assert.False(t, hasA, "ns1's trigger should not leak into ns2")
}

func TestReconcile_AllSpecFieldsContribute(t *testing.T) {
	cr := newTestCR(DefaultNotificationsConfigurationName, "default",
		v1alpha1.NotificationsConfigurationSpec{
			Triggers:      map[string]string{"trigger.t1": "when: true"},
			Templates:     map[string]string{"template.t1": "email: subject: test"},
			Services:      map[string]string{"service.slack": "token: $slack-token"},
			Subscriptions: map[string]string{"subscriptions": "- recipients: [slack:general]"},
			Context:       map[string]string{"env": "prod"},
		})

	cl := testClient(cr)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.NoError(t, err)

	cm := getConfigMap(t, cl, "default")
	assert.Equal(t, "when: true", cm.Data["trigger.t1"])
	assert.Equal(t, "email: subject: test", cm.Data["template.t1"])
	assert.Equal(t, "token: $slack-token", cm.Data["service.slack"])
	assert.Contains(t, cm.Data["subscriptions"], "slack:general")
	assert.Contains(t, cm.Data["context"], "env:")
}

// ---------------------------------------------------------------------------
// C. ConfigMap Lifecycle
// ---------------------------------------------------------------------------

func TestReconcile_DeleteOneCR_PreservesOtherCRs(t *testing.T) {
	crA := newTestCR("cr-a", "default",
		v1alpha1.NotificationsConfigurationSpec{
			Triggers: map[string]string{"trigger.from-a": "val-a"},
		})
	crB := newTestCR("cr-b", "default",
		v1alpha1.NotificationsConfigurationSpec{
			Triggers: map[string]string{"trigger.from-b": "val-b"},
		})

	cl := testClient(crA, crB)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.NoError(t, err)
	cm := getConfigMap(t, cl, "default")
	assert.Equal(t, "val-a", cm.Data["trigger.from-a"])
	assert.Equal(t, "val-b", cm.Data["trigger.from-b"])

	require.NoError(t, cl.Delete(context.TODO(), crA))

	_, err = r.Reconcile(context.TODO(), reconcileReq("default"))
	require.NoError(t, err)

	cm = getConfigMap(t, cl, "default")
	_, hasA := cm.Data["trigger.from-a"]
	assert.False(t, hasA, "deleted CR's key should be absent")
	assert.Equal(t, "val-b", cm.Data["trigger.from-b"], "remaining CR's key should be preserved")
}

func TestReconcile_DeleteLastCR_DeletesConfigMap(t *testing.T) {
	cr := newTestCR("only-cr", "default",
		v1alpha1.NotificationsConfigurationSpec{
			Triggers: map[string]string{"trigger.x": "val"},
		})

	cl := testClient(cr)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.NoError(t, err)
	assert.True(t, configMapExists(cl, "default"))

	require.NoError(t, cl.Delete(context.TODO(), cr))

	_, err = r.Reconcile(context.TODO(), reconcileReq("default"))
	require.NoError(t, err)
	assert.False(t, configMapExists(cl, "default"), "ConfigMap should be deleted when last CR is removed")
}

func TestReconcile_ManualDataEdits_Corrected(t *testing.T) {
	cr := newTestCR(DefaultNotificationsConfigurationName, "default",
		v1alpha1.NotificationsConfigurationSpec{
			Triggers: map[string]string{"trigger.x": "correct"},
		})

	cl := testClient(cr)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.NoError(t, err)

	cm := getConfigMap(t, cl, "default")
	cm.Data["trigger.x"] = "tampered"
	cm.Data["extra"] = "injected"
	require.NoError(t, cl.Update(context.TODO(), cm))

	_, err = r.Reconcile(context.TODO(), reconcileReq("default"))
	require.NoError(t, err)

	cm = getConfigMap(t, cl, "default")
	assert.Equal(t, "correct", cm.Data["trigger.x"], "tampered value should be corrected")
	_, hasExtra := cm.Data["extra"]
	assert.False(t, hasExtra, "injected key should be removed")
}

func TestReconcile_DeletedConfigMap_Recreated(t *testing.T) {
	cr := newTestCR(DefaultNotificationsConfigurationName, "default",
		v1alpha1.NotificationsConfigurationSpec{
			Triggers: map[string]string{"trigger.x": "val"},
		})

	cl := testClient(cr)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.NoError(t, err)

	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name: ArgoCDNotificationsConfigMap, Namespace: "default",
	}}
	require.NoError(t, cl.Delete(context.TODO(), cm))
	assert.False(t, configMapExists(cl, "default"))

	_, err = r.Reconcile(context.TODO(), reconcileReq("default"))
	require.NoError(t, err)

	cm = getConfigMap(t, cl, "default")
	assert.Equal(t, "val", cm.Data["trigger.x"])
	assert.True(t, argoutil.IsTrackedByOperator(cm.Labels))
}

func TestReconcile_UnchangedReconcile_NoWrite(t *testing.T) {
	cr := newTestCR(DefaultNotificationsConfigurationName, "default",
		v1alpha1.NotificationsConfigurationSpec{
			Triggers: map[string]string{"trigger.x": "when: true"},
		})

	var updateCount int
	cl := testClientWithInterceptors(
		interceptor.Funcs{
			Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				updateCount++
				return c.Update(ctx, obj, opts...)
			},
		},
		cr,
	)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.NoError(t, err)
	updateCount = 0

	_, err = r.Reconcile(context.TODO(), reconcileReq("default"))
	require.NoError(t, err)
	assert.Equal(t, 0, updateCount, "second reconcile with unchanged data must not call Update")
}

func TestReconcile_UnrelatedMetadata_Survives(t *testing.T) {
	cr := newTestCR(DefaultNotificationsConfigurationName, "default",
		v1alpha1.NotificationsConfigurationSpec{
			Triggers: map[string]string{"trigger.x": "val"},
		})

	cl := testClient(cr)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.NoError(t, err)

	cm := getConfigMap(t, cl, "default")
	cm.Labels["custom-label"] = "custom-value"
	cm.Annotations = map[string]string{"custom-annotation": "ann-value"}
	require.NoError(t, cl.Update(context.TODO(), cm))

	cr.Spec.Triggers["trigger.x"] = "updated"
	require.NoError(t, cl.Update(context.TODO(), cr))

	_, err = r.Reconcile(context.TODO(), reconcileReq("default"))
	require.NoError(t, err)

	cm = getConfigMap(t, cl, "default")
	assert.Equal(t, "updated", cm.Data["trigger.x"])
	assert.Equal(t, "custom-value", cm.Labels["custom-label"], "custom label should survive update")
	assert.Equal(t, "ann-value", cm.Annotations["custom-annotation"], "custom annotation should survive update")
}

func TestReconcile_UnmanagedConfigMap_NotTakenOver(t *testing.T) {
	cr := newTestCR(DefaultNotificationsConfigurationName, "default",
		v1alpha1.NotificationsConfigurationSpec{
			Triggers: map[string]string{"trigger.x": "operator-value"},
		})

	unmanagedCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ArgoCDNotificationsConfigMap,
			Namespace: "default",
		},
		Data: map[string]string{"custom": "user-data"},
	}

	cl := testClient(cr, unmanagedCM)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not managed by this operator")

	cm := getConfigMap(t, cl, "default")
	assert.Equal(t, "user-data", cm.Data["custom"])
	_, hasTrigger := cm.Data["trigger.x"]
	assert.False(t, hasTrigger, "operator data should not have been written to unmanaged ConfigMap")
}

func TestReconcile_UnmanagedConfigMap_NotDeletedOnNoCRs(t *testing.T) {
	unmanagedCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ArgoCDNotificationsConfigMap,
			Namespace: "default",
		},
		Data: map[string]string{"custom": "data"},
	}

	cl := testClient(unmanagedCM)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.NoError(t, err, "no error when no CRs exist and ConfigMap is unmanaged")

	assert.True(t, configMapExists(cl, "default"))
	cm := getConfigMap(t, cl, "default")
	assert.Equal(t, "data", cm.Data["custom"])
}

// ---------------------------------------------------------------------------
// D. Migration and Errors
// ---------------------------------------------------------------------------

func TestReconcile_LegacyOwnerReference_Removed(t *testing.T) {
	cr := newTestCR(DefaultNotificationsConfigurationName, "default",
		v1alpha1.NotificationsConfigurationSpec{
			Triggers: map[string]string{"trigger.x": "val"},
		})
	cr.UID = "cr-uid"

	legacyCM := managedCM("default",
		map[string]string{"trigger.x": "val"},
		legacyRef(DefaultNotificationsConfigurationName, "cr-uid"),
	)

	cl := testClient(cr, legacyCM)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.NoError(t, err)

	cm := getConfigMap(t, cl, "default")
	assert.Empty(t, cm.OwnerReferences, "legacy owner reference should be removed")
	assert.True(t, argoutil.IsTrackedByOperator(cm.Labels), "tracking label should be preserved")
}

func TestReconcile_MigrationEvenWhenDataMatches(t *testing.T) {
	cr := newTestCR(DefaultNotificationsConfigurationName, "default",
		v1alpha1.NotificationsConfigurationSpec{
			Triggers: map[string]string{"trigger.x": "val"},
		})

	legacyCM := managedCM("default",
		map[string]string{"trigger.x": "val"},
		legacyRef(DefaultNotificationsConfigurationName, "old-uid"),
	)

	var updateCount int
	cl := testClientWithInterceptors(
		interceptor.Funcs{
			Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				updateCount++
				return c.Update(ctx, obj, opts...)
			},
		},
		cr, legacyCM,
	)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.NoError(t, err)

	assert.Equal(t, 1, updateCount, "should update to remove legacy owner reference even when data matches")
	cm := getConfigMap(t, cl, "default")
	assert.Empty(t, cm.OwnerReferences)
}

// Non-controller owner references can still affect garbage collection.
func TestReconcile_UnsupportedNonControllerRef_BlocksUpdate(t *testing.T) {
	cr := newTestCR(DefaultNotificationsConfigurationName, "default",
		v1alpha1.NotificationsConfigurationSpec{
			Triggers: map[string]string{"trigger.x": "val"},
		})

	cmWithUnknownOwner := managedCM("default",
		map[string]string{"trigger.x": "val"},
		metav1.OwnerReference{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
			Name:       "some-deployment",
			UID:        "deploy-uid",
		},
	)

	tracker := &writeTracker{}
	cl := testClientWithInterceptors(noWriteInterceptors(tracker), cr, cmWithUnknownOwner)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported owner reference")
	assert.False(t, tracker.updateCalled, "no Update when unsupported owner ref present")
}

func TestReconcile_ListError_DoesNotCauseDeletion(t *testing.T) {
	existingCM := managedCM("default", map[string]string{"trigger.x": "important"})

	cl := testClientWithInterceptors(
		interceptor.Funcs{
			List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				if _, ok := list.(*v1alpha1.NotificationsConfigurationList); ok {
					return fmt.Errorf("simulated list error")
				}
				return c.List(ctx, list, opts...)
			},
		},
		existingCM,
	)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list")

	assert.True(t, configMapExists(cl, "default"))
}

func TestReconcile_InvalidCR_SkippedAndConfigMapUpdated(t *testing.T) {
	badCR := newTestCR("bad", "default",
		v1alpha1.NotificationsConfigurationSpec{
			Triggers: map[string]string{"context": "this-is-reserved"},
		})

	existingCM := managedCM("default", map[string]string{"trigger.x": "stale-data"})

	cl := testClient(badCR, existingCM)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.NoError(t, err, "bad CR is skipped, not a merge error")

	cm := getConfigMap(t, cl, "default")
	assert.Empty(t, cm.Data, "stale data should be cleared when the only CR is skipped")
}

func TestReconcile_InvalidKey_SkippedButOtherEntriesKept(t *testing.T) {
	badCR := newTestCR("bad-cr", "default",
		v1alpha1.NotificationsConfigurationSpec{
			Triggers: map[string]string{
				"context":           "reserved-key",
				"trigger.on-deploy": "should-be-kept",
			},
		})
	goodCR := newTestCR("good-cr", "default",
		v1alpha1.NotificationsConfigurationSpec{
			Triggers: map[string]string{"trigger.on-sync": "valid-trigger"},
		})

	existingCM := managedCM("default", map[string]string{"trigger.x": "old-data"})

	cl := testClient(badCR, goodCR, existingCM)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.NoError(t, err)

	cm := getConfigMap(t, cl, "default")
	assert.Equal(t, "valid-trigger", cm.Data["trigger.on-sync"], "good CR entries should be present")
	assert.Equal(t, "should-be-kept", cm.Data["trigger.on-deploy"], "bad CR's valid entries should still be kept")
	assert.NotContains(t, cm.Data, "context", "reserved key should not appear as a flat entry")
	assert.NotContains(t, cm.Data, "trigger.x", "stale data from old ConfigMap should be replaced")
}

func TestReconcile_WriteConflict_ReturnsForRetry(t *testing.T) {
	cr := newTestCR(DefaultNotificationsConfigurationName, "default",
		v1alpha1.NotificationsConfigurationSpec{
			Triggers: map[string]string{"trigger.x": "new-value"},
		})

	existingCM := managedCM("default", map[string]string{"trigger.x": "old-value"})

	cl := testClientWithInterceptors(
		interceptor.Funcs{
			Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				if _, ok := obj.(*corev1.ConfigMap); ok {
					return apierrors.NewConflict(
						schema.GroupResource{Resource: "configmaps"},
						ArgoCDNotificationsConfigMap,
						fmt.Errorf("the object has been modified"),
					)
				}
				return c.Update(ctx, obj, opts...)
			},
		},
		cr, existingCM,
	)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.Error(t, err, "write conflict should be returned for retry")
	assert.True(t, apierrors.IsConflict(err))
}

func TestReconcile_DeletionPreconditions(t *testing.T) {
	existingCM := managedCM("default", map[string]string{"trigger.x": "data"})
	existingCM.UID = "cm-uid-123"
	existingCM.ResourceVersion = "rv-456"

	var capturedPreconditions *metav1.Preconditions
	cl := testClientWithInterceptors(
		interceptor.Funcs{
			Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
				deleteOpts := &client.DeleteOptions{}
				for _, o := range opts {
					o.ApplyToDelete(deleteOpts)
				}
				capturedPreconditions = deleteOpts.Preconditions
				return c.Delete(ctx, obj, opts...)
			},
		},
		existingCM,
	)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.NoError(t, err)

	require.NotNil(t, capturedPreconditions, "deletion should include preconditions")
	require.NotNil(t, capturedPreconditions.UID, "preconditions should include UID")
	require.NotNil(t, capturedPreconditions.ResourceVersion, "preconditions should include ResourceVersion")
}

func TestReconcile_DeletionNotFound_Success(t *testing.T) {
	existingCM := managedCM("default", map[string]string{"trigger.x": "data"})

	cl := testClientWithInterceptors(
		interceptor.Funcs{
			Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
				return apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, ArgoCDNotificationsConfigMap)
			},
		},
		existingCM,
	)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.NoError(t, err, "NotFound during deletion should be treated as success")
}

func TestReconcile_EmptyNamespace_Rejected(t *testing.T) {
	cl := testClient()
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: ArgoCDNotificationsConfigMap},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "namespace-scoped")
}

// ---------------------------------------------------------------------------
// E. Startup Recovery
// ---------------------------------------------------------------------------

func TestReconcile_OrphanedConfigMap_Cleaned(t *testing.T) {
	orphanedCM := managedCM("default", map[string]string{"trigger.x": "stale"})

	cl := testClient(orphanedCM)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.NoError(t, err)
	assert.False(t, configMapExists(cl, "default"),
		"orphaned ConfigMap should be cleaned up when no CRs exist")
}

func TestReconcile_MissingConfigMap_Recreated(t *testing.T) {
	cr := newTestCR(DefaultNotificationsConfigurationName, "default",
		v1alpha1.NotificationsConfigurationSpec{
			Triggers: map[string]string{"trigger.x": "recovered"},
		})

	cl := testClient(cr)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.NoError(t, err)
	assert.True(t, configMapExists(cl, "default"))

	cm := getConfigMap(t, cl, "default")
	assert.Equal(t, "recovered", cm.Data["trigger.x"])
	assert.True(t, argoutil.IsTrackedByOperator(cm.Labels))
}

// ---------------------------------------------------------------------------
// F. migrateLegacyOwnerReferences unit tests
// ---------------------------------------------------------------------------

func TestMigrateLegacyOwnerReferences_NoRefs(t *testing.T) {
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "test"}}
	assert.False(t, migrateLegacyOwnerReferences(cm))
	assert.Nil(t, cm.OwnerReferences)
}

func TestMigrateLegacyOwnerReferences_OnlyLegacy(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
			OwnerReferences: []metav1.OwnerReference{
				legacyRef("cr1", "uid1"),
				legacyRef("cr2", "uid2"),
			},
		},
	}
	assert.True(t, migrateLegacyOwnerReferences(cm))
	assert.Empty(t, cm.OwnerReferences)
}

// ---------------------------------------------------------------------------
// G. Ownership Safety
//
// Only recognized legacy CR references may be removed from the shared ConfigMap.
// Other owners block updates and deletion, regardless of the Controller field.
// ---------------------------------------------------------------------------

func TestReconcile_UnknownControllerOwner_BlocksUpdate(t *testing.T) {
	cr := newTestCR(DefaultNotificationsConfigurationName, "default",
		v1alpha1.NotificationsConfigurationSpec{
			Triggers: map[string]string{"trigger.x": "new-value"},
		})

	cmWithUnknown := managedCM("default",
		map[string]string{"trigger.x": "old-value"},
		metav1.OwnerReference{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
			Name:       "some-deployment",
			UID:        "deploy-uid",
			Controller: boolPtr(true),
		},
	)

	tracker := &writeTracker{}
	cl := testClientWithInterceptors(noWriteInterceptors(tracker), cr, cmWithUnknown)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported owner reference")
	assert.False(t, tracker.updateCalled)
}

func TestReconcile_UnknownControllerFalse_BlocksUpdate(t *testing.T) {
	cr := newTestCR(DefaultNotificationsConfigurationName, "default",
		v1alpha1.NotificationsConfigurationSpec{
			Triggers: map[string]string{"trigger.x": "val"},
		})

	cmWithFalseController := managedCM("default",
		map[string]string{"trigger.x": "old"},
		metav1.OwnerReference{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
			Name:       "some-deployment",
			UID:        "deploy-uid",
			Controller: boolPtr(false),
		},
	)

	tracker := &writeTracker{}
	cl := testClientWithInterceptors(noWriteInterceptors(tracker), cr, cmWithFalseController)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported owner reference")
	assert.False(t, tracker.updateCalled)
}

func TestReconcile_UnknownControllerNil_BlocksUpdate(t *testing.T) {
	cr := newTestCR(DefaultNotificationsConfigurationName, "default",
		v1alpha1.NotificationsConfigurationSpec{
			Triggers: map[string]string{"trigger.x": "val"},
		})

	cmWithNilController := managedCM("default",
		map[string]string{"trigger.x": "old"},
		metav1.OwnerReference{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
			Name:       "some-deployment",
			UID:        "deploy-uid",
		},
	)

	tracker := &writeTracker{}
	cl := testClientWithInterceptors(noWriteInterceptors(tracker), cr, cmWithNilController)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported owner reference")
	assert.False(t, tracker.updateCalled)
}

func TestReconcile_UnknownControllerOwner_BlocksDelete(t *testing.T) {
	cmWithUnknown := managedCM("default",
		map[string]string{"trigger.x": "data"},
		metav1.OwnerReference{
			APIVersion: "somecontroller.io/v1",
			Kind:       "SomeResource",
			Name:       "some-resource",
			UID:        "some-uid",
			Controller: boolPtr(true),
		},
	)

	tracker := &writeTracker{}
	cl := testClientWithInterceptors(noWriteInterceptors(tracker), cmWithUnknown)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported owner reference")
	assert.False(t, tracker.deleteCalled, "ConfigMap Delete must not be called")
	assert.True(t, configMapExists(cl, "default"))
}

func TestReconcile_UnknownNonControllerOwner_BlocksDelete(t *testing.T) {
	cmWithUnknown := managedCM("default",
		map[string]string{"trigger.x": "data"},
		metav1.OwnerReference{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
			Name:       "some-deployment",
			UID:        "deploy-uid",
		},
	)

	tracker := &writeTracker{}
	cl := testClientWithInterceptors(noWriteInterceptors(tracker), cmWithUnknown)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported owner reference")
	assert.False(t, tracker.deleteCalled)
	assert.True(t, configMapExists(cl, "default"))
}

func TestReconcile_SameKindDifferentAPIGroup_BlocksUpdate(t *testing.T) {
	cr := newTestCR(DefaultNotificationsConfigurationName, "default",
		v1alpha1.NotificationsConfigurationSpec{
			Triggers: map[string]string{"trigger.x": "val"},
		})

	cmWithForeign := managedCM("default",
		map[string]string{"trigger.x": "val"},
		metav1.OwnerReference{
			APIVersion: "otherprovider.io/v1alpha1",
			Kind:       "NotificationsConfiguration",
			Name:       "foreign-cr",
			UID:        "foreign-uid",
			Controller: boolPtr(true),
		},
	)

	tracker := &writeTracker{}
	cl := testClientWithInterceptors(noWriteInterceptors(tracker), cr, cmWithForeign)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported owner reference")
	assert.False(t, tracker.updateCalled)
}

func TestReconcile_SameAPIVersionKind_NonController_BlocksUpdate(t *testing.T) {
	cr := newTestCR(DefaultNotificationsConfigurationName, "default",
		v1alpha1.NotificationsConfigurationSpec{
			Triggers: map[string]string{"trigger.x": "val"},
		})

	cmWithPartialMatch := managedCM("default",
		map[string]string{"trigger.x": "val"},
		metav1.OwnerReference{
			APIVersion: "argoproj.io/v1alpha1",
			Kind:       "NotificationsConfiguration",
			Name:       "some-cr",
			UID:        "some-uid",
		},
	)

	tracker := &writeTracker{}
	cl := testClientWithInterceptors(noWriteInterceptors(tracker), cr, cmWithPartialMatch)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported owner reference")
	assert.False(t, tracker.updateCalled)
}

func TestReconcile_CorrectAPIVersionKindController_EmptyName_Blocks(t *testing.T) {
	cr := newTestCR(DefaultNotificationsConfigurationName, "default",
		v1alpha1.NotificationsConfigurationSpec{
			Triggers: map[string]string{"trigger.x": "val"},
		})

	cmWithEmptyName := managedCM("default",
		map[string]string{"trigger.x": "val"},
		metav1.OwnerReference{
			APIVersion: "argoproj.io/v1alpha1",
			Kind:       "NotificationsConfiguration",
			Name:       "",
			UID:        "some-uid",
			Controller: boolPtr(true),
		},
	)

	tracker := &writeTracker{}
	cl := testClientWithInterceptors(noWriteInterceptors(tracker), cr, cmWithEmptyName)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported owner reference")
	assert.False(t, tracker.updateCalled)
}

func TestReconcile_CorrectAPIVersionKindController_EmptyUID_Blocks(t *testing.T) {
	cr := newTestCR(DefaultNotificationsConfigurationName, "default",
		v1alpha1.NotificationsConfigurationSpec{
			Triggers: map[string]string{"trigger.x": "val"},
		})

	cmWithEmptyUID := managedCM("default",
		map[string]string{"trigger.x": "val"},
		metav1.OwnerReference{
			APIVersion: "argoproj.io/v1alpha1",
			Kind:       "NotificationsConfiguration",
			Name:       "some-cr",
			UID:        "",
			Controller: boolPtr(true),
		},
	)

	tracker := &writeTracker{}
	cl := testClientWithInterceptors(noWriteInterceptors(tracker), cr, cmWithEmptyUID)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported owner reference")
	assert.False(t, tracker.updateCalled)
}

func TestReconcile_MixedRecognizedAndUnsupported_Blocks(t *testing.T) {
	cr := newTestCR(DefaultNotificationsConfigurationName, "default",
		v1alpha1.NotificationsConfigurationSpec{
			Triggers: map[string]string{"trigger.x": "val"},
		})

	cmWithMixed := managedCM("default",
		map[string]string{"trigger.x": "val"},
		legacyRef("legacy-cr", "legacy-uid"),
		metav1.OwnerReference{
			APIVersion: "apps/v1",
			Kind:       "StatefulSet",
			Name:       "unexpected-sts",
			UID:        "sts-uid",
			Controller: boolPtr(true),
		},
	)

	tracker := &writeTracker{}
	cl := testClientWithInterceptors(noWriteInterceptors(tracker), cr, cmWithMixed)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported owner reference",
		"unsupported ref must block even when a recognized legacy ref is also present")
	assert.False(t, tracker.updateCalled, "no mutation when any unsupported ref exists")
}

func TestReconcile_LegacyOwnerWhoseCRNoLongerExists(t *testing.T) {
	cr := newTestCR(DefaultNotificationsConfigurationName, "default",
		v1alpha1.NotificationsConfigurationSpec{
			Triggers: map[string]string{"trigger.x": "val"},
		})

	cmWithStaleRef := managedCM("default",
		map[string]string{"trigger.x": "old-val"},
		legacyRef("deleted-cr-that-no-longer-exists", "stale-uid"),
	)

	cl := testClient(cr, cmWithStaleRef)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.NoError(t, err, "recognized legacy ref should be stripped even when its CR no longer exists")

	cm := getConfigMap(t, cl, "default")
	assert.Empty(t, cm.OwnerReferences, "stale legacy ref should be removed")
	assert.Equal(t, "val", cm.Data["trigger.x"])
}

func TestReconcile_RecognizedLegacyControllerRef_StrippedAndAllowed(t *testing.T) {
	cr := newTestCR(DefaultNotificationsConfigurationName, "default",
		v1alpha1.NotificationsConfigurationSpec{
			Triggers: map[string]string{"trigger.x": "new-value"},
		})

	cmWithLegacy := managedCM("default",
		map[string]string{"trigger.x": "old-value"},
		legacyRef(DefaultNotificationsConfigurationName, "cr-uid"),
	)

	cl := testClient(cr, cmWithLegacy)
	r := testReconciler(cl)

	_, err := r.Reconcile(context.TODO(), reconcileReq("default"))
	require.NoError(t, err, "recognized legacy controller ref should be stripped, not blocked")

	cm := getConfigMap(t, cl, "default")
	assert.Empty(t, cm.OwnerReferences)
	assert.Equal(t, "new-value", cm.Data["trigger.x"])
}

// ---------------------------------------------------------------------------
// H. isRecognizedLegacyOwnerReference unit tests
// ---------------------------------------------------------------------------

func TestIsRecognizedLegacyOwnerReference(t *testing.T) {
	tests := []struct {
		name     string
		ref      metav1.OwnerReference
		expected bool
	}{
		{
			name:     "complete legacy shape",
			ref:      legacyRef("cr", "uid"),
			expected: true,
		},
		{
			name: "wrong API group",
			ref: metav1.OwnerReference{
				APIVersion: "otherprovider.io/v1alpha1", Kind: "NotificationsConfiguration",
				Name: "cr", UID: "uid", Controller: boolPtr(true),
			},
			expected: false,
		},
		{
			name: "wrong version",
			ref: metav1.OwnerReference{
				APIVersion: "argoproj.io/v1beta1", Kind: "NotificationsConfiguration",
				Name: "cr", UID: "uid", Controller: boolPtr(true),
			},
			expected: false,
		},
		{
			name: "wrong kind",
			ref: metav1.OwnerReference{
				APIVersion: "argoproj.io/v1alpha1", Kind: "ArgoCD",
				Name: "cr", UID: "uid", Controller: boolPtr(true),
			},
			expected: false,
		},
		{
			name: "empty API version",
			ref: metav1.OwnerReference{
				APIVersion: "", Kind: "NotificationsConfiguration",
				Name: "cr", UID: "uid", Controller: boolPtr(true),
			},
			expected: false,
		},
		{
			name: "empty name",
			ref: metav1.OwnerReference{
				APIVersion: "argoproj.io/v1alpha1", Kind: "NotificationsConfiguration",
				Name: "", UID: "uid", Controller: boolPtr(true),
			},
			expected: false,
		},
		{
			name: "empty UID",
			ref: metav1.OwnerReference{
				APIVersion: "argoproj.io/v1alpha1", Kind: "NotificationsConfiguration",
				Name: "cr", UID: "", Controller: boolPtr(true),
			},
			expected: false,
		},
		{
			name: "controller nil",
			ref: metav1.OwnerReference{
				APIVersion: "argoproj.io/v1alpha1", Kind: "NotificationsConfiguration",
				Name: "cr", UID: "uid",
			},
			expected: false,
		},
		{
			name: "controller false",
			ref: metav1.OwnerReference{
				APIVersion: "argoproj.io/v1alpha1", Kind: "NotificationsConfiguration",
				Name: "cr", UID: "uid", Controller: boolPtr(false),
			},
			expected: false,
		},
		{
			name:     "both empty",
			ref:      metav1.OwnerReference{},
			expected: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, isRecognizedLegacyOwnerReference(tc.ref))
		})
	}
}

func TestHasUnsupportedOwnerReference(t *testing.T) {
	tests := []struct {
		name     string
		refs     []metav1.OwnerReference
		expected bool
	}{
		{
			name:     "no refs",
			refs:     nil,
			expected: false,
		},
		{
			name:     "only recognized legacy",
			refs:     []metav1.OwnerReference{legacyRef("cr", "uid")},
			expected: false,
		},
		{
			name: "unknown controller true",
			refs: []metav1.OwnerReference{
				{APIVersion: "apps/v1", Kind: "Deployment", Name: "d", UID: "u", Controller: boolPtr(true)},
			},
			expected: true,
		},
		{
			name: "unknown controller false",
			refs: []metav1.OwnerReference{
				{APIVersion: "apps/v1", Kind: "Deployment", Name: "d", UID: "u", Controller: boolPtr(false)},
			},
			expected: true,
		},
		{
			name: "unknown controller nil",
			refs: []metav1.OwnerReference{
				{APIVersion: "apps/v1", Kind: "Deployment", Name: "d", UID: "u"},
			},
			expected: true,
		},
		{
			name: "mixed recognized and unknown",
			refs: []metav1.OwnerReference{
				legacyRef("cr", "uid"),
				{APIVersion: "apps/v1", Kind: "StatefulSet", Name: "s", UID: "u", Controller: boolPtr(true)},
			},
			expected: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
				Name:            "test",
				OwnerReferences: tc.refs,
			}}
			assert.Equal(t, tc.expected, hasUnsupportedOwnerReference(cm))
		})
	}
}
