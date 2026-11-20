package argocd

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	argoproj "github.com/redhat-developer/gitops-operator/argocd-operator/api/v1beta1"
	aiconfig "github.com/redhat-developer/gitops-operator/argocd-operator/pkg/ai"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestAIEnrollmentPreservesInstancesAndRevokesMatchingUID(t *testing.T) {
	r, first := aiNotificationsFixture(t, false)
	t.Setenv("ARGOCD_OPERATOR_NAMESPACE", "operator")
	ctx := context.Background()
	key := client.ObjectKey{Name: aiconfig.EnrollmentConfigMapName, Namespace: "operator"}
	second := &argoproj.ArgoCD{ObjectMeta: metav1.ObjectMeta{Name: "second", Namespace: "team-b", UID: "second-uid"}}
	if err := r.Create(ctx, aiNotificationsServiceAccount(second)); err != nil {
		t.Fatal(err)
	}
	for _, cr := range []*argoproj.ArgoCD{first, second} {
		if err := r.reconcileAIEnrollment(cr, true); err != nil {
			t.Fatal(err)
		}
	}
	registry := &corev1.ConfigMap{}
	if err := r.Get(ctx, key, registry); err != nil {
		t.Fatal(err)
	}
	if registry.Labels[aiconfig.EnrollmentManagedByLabel] != aiconfig.EnrollmentManager || len(registry.Data) != 2 {
		t.Fatalf("incorrect registry: %+v", registry)
	}
	if err := r.Get(ctx, client.ObjectKey{Name: key.Name, Namespace: first.Namespace}, &corev1.ConfigMap{}); !apierrors.IsNotFound(err) {
		t.Fatalf("registry written outside operator namespace: %v", err)
	}
	before := registry.DeepCopy()
	if err := r.reconcileAIEnrollment(first, true); err != nil {
		t.Fatal(err)
	}
	if err := r.Get(ctx, key, registry); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, registry) {
		t.Fatal("unchanged enrollment modified the registry")
	}

	successor := first.DeepCopy()
	successor.UID = "successor-uid"
	if err := r.reconcileAIEnrollment(successor, true); err != nil {
		t.Fatal(err)
	}
	if err := r.reconcileAIEnrollment(first, false); err != nil {
		t.Fatal(err)
	}
	if err := r.Get(ctx, key, registry); err != nil {
		t.Fatal(err)
	}
	var current aiconfig.InstanceEnrollment
	if err := json.Unmarshal([]byte(registry.Data[first.Namespace]), &current); err != nil {
		t.Fatal(err)
	}
	if current.UID != string(successor.UID) || len(registry.Data) != 2 {
		t.Fatal("old instance cleanup removed its successor or another instance")
	}
	if err := r.reconcileAIEnrollment(second, false); err != nil {
		t.Fatal(err)
	}
	if err := r.Get(ctx, key, registry); err != nil {
		t.Fatal(err)
	}
	if len(registry.Data) != 1 || registry.Data[first.Namespace] == "" {
		t.Fatal("revocation removed another instance")
	}
	if err := r.reconcileAIEnrollment(successor, false); err != nil {
		t.Fatal(err)
	}
	if err := r.Get(ctx, key, registry); err != nil || len(registry.Data) != 0 {
		t.Fatalf("empty registry was removed or last entry was retained: %v", err)
	}
}

func TestAIEnrollmentRejectsMissingUID(t *testing.T) {
	for _, missing := range []string{"argocd", "serviceaccount"} {
		t.Run(missing, func(t *testing.T) {
			r, cr := aiNotificationsFixture(t, false)
			if missing == "argocd" {
				cr.UID = ""
			} else {
				sa := aiNotificationsServiceAccount(cr)
				if err := r.Delete(context.Background(), sa); err != nil {
					t.Fatal(err)
				}
				sa.UID = ""
				if err := r.Create(context.Background(), sa); err != nil {
					t.Fatal(err)
				}
			}
			if err := r.reconcileAIEnrollment(cr, true); err == nil {
				t.Fatal("enrolled an identity without its UID")
			}
			key := client.ObjectKey{Name: aiconfig.EnrollmentConfigMapName, Namespace: cr.Namespace}
			if err := r.Get(context.Background(), key, &corev1.ConfigMap{}); !apierrors.IsNotFound(err) {
				t.Fatalf("registry created without a complete identity: %v", err)
			}
		})
	}
}

func TestAIEnrollmentPreservesUnownedRegistry(t *testing.T) {
	for _, owner := range []string{"", "someone-else"} {
		t.Run(owner, func(t *testing.T) {
			r, cr := aiNotificationsFixture(t, false)
			value, err := json.Marshal(aiconfig.InstanceEnrollment{Name: cr.Name, UID: string(cr.UID), ServiceAccountUID: "sa-uid"})
			if err != nil {
				t.Fatal(err)
			}
			registry := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: aiconfig.EnrollmentConfigMapName, Namespace: cr.Namespace},
				Data:       map[string]string{cr.Namespace: string(value)},
			}
			if owner != "" {
				registry.Labels = map[string]string{aiconfig.EnrollmentManagedByLabel: owner}
			}
			if err := r.Create(context.Background(), registry); err != nil {
				t.Fatal(err)
			}
			before := registry.DeepCopy()
			for _, enroll := range []bool{true, false} {
				if err := r.reconcileAIEnrollment(cr, enroll); err == nil {
					t.Fatal("accepted unowned registry")
				}
				if err := r.Get(context.Background(), client.ObjectKeyFromObject(registry), registry); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(before, registry) {
					t.Fatal("unowned registry was modified")
				}
			}
		})
	}
}

func TestAIEnrollmentRevocationDoesNotCreateRegistry(t *testing.T) {
	r, cr := aiNotificationsFixture(t, false)
	if err := r.reconcileAIEnrollment(cr, false); err != nil {
		t.Fatal(err)
	}
	key := client.ObjectKey{Name: aiconfig.EnrollmentConfigMapName, Namespace: cr.Namespace}
	if err := r.Get(context.Background(), key, &corev1.ConfigMap{}); !apierrors.IsNotFound(err) {
		t.Fatalf("revocation created registry: %v", err)
	}
}

func TestAIEnrollmentRetriesConcurrentRegistryChanges(t *testing.T) {
	for _, operation := range []string{"create", "patch"} {
		t.Run(operation, func(t *testing.T) {
			r, cr := aiNotificationsFixture(t, false)
			ctx := context.Background()
			registry := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: aiconfig.EnrollmentConfigMapName, Namespace: cr.Namespace,
					Labels: map[string]string{aiconfig.EnrollmentManagedByLabel: aiconfig.EnrollmentManager},
				},
				Data: map[string]string{"other-namespace": "other-enrollment"},
			}
			if operation == "patch" {
				if err := r.Create(ctx, registry); err != nil {
					t.Fatal(err)
				}
			}
			injected := false
			r.Client = interceptor.NewClient(r.Client.(client.WithWatch), interceptor.Funcs{
				Create: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
					if operation == "create" && !injected && obj.GetName() == aiconfig.EnrollmentConfigMapName {
						injected = true
						if err := cl.Create(ctx, registry); err != nil {
							return err
						}
						return apierrors.NewAlreadyExists(schema.GroupResource{Resource: "configmaps"}, obj.GetName())
					}
					return cl.Create(ctx, obj, opts...)
				},
				Patch: func(ctx context.Context, cl client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					if operation == "patch" && !injected && obj.GetName() == aiconfig.EnrollmentConfigMapName {
						injected = true
						registry.Data["concurrent-namespace"] = "concurrent-enrollment"
						if err := cl.Update(ctx, registry); err != nil {
							return err
						}
						return apierrors.NewConflict(schema.GroupResource{Resource: "configmaps"}, obj.GetName(), errors.New("concurrent enrollment"))
					}
					return cl.Patch(ctx, obj, patch, opts...)
				},
			})
			if err := r.reconcileAIEnrollment(cr, true); err != nil {
				t.Fatal(err)
			}
			if err := r.Get(ctx, client.ObjectKeyFromObject(registry), registry); err != nil {
				t.Fatal(err)
			}
			if !injected || registry.Data[cr.Namespace] == "" || registry.Data["other-namespace"] != "other-enrollment" {
				t.Fatalf("retry lost an enrollment: %+v", registry.Data)
			}
			if operation == "patch" && registry.Data["concurrent-namespace"] != "concurrent-enrollment" {
				t.Fatal("retry overwrote the concurrent enrollment")
			}
		})
	}
}

func TestAIEnrollmentDeletionEnqueuesManagedEnabledInstances(t *testing.T) {
	r, first := aiNotificationsFixture(t, false)
	t.Setenv("ARGOCD_OPERATOR_NAMESPACE", "operator")
	ctx := context.Background()
	first.Labels = map[string]string{"ai-scope": "managed"}
	if err := r.Update(ctx, first); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		namespace string
		enabled   bool
		selected  bool
		deleting  bool
	}{
		{namespace: "team-b", enabled: true, selected: true},
		{namespace: "disabled", selected: true},
		{namespace: "ignored", enabled: true},
		{namespace: "terminating", enabled: true, selected: true, deleting: true},
	} {
		cr := &argoproj.ArgoCD{
			ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: fixture.namespace},
			Spec:       argoproj.ArgoCDSpec{AI: &argoproj.ArgoCDAISpec{Troubleshooting: fixture.enabled}},
		}
		if fixture.selected {
			cr.Labels = map[string]string{"ai-scope": "managed"}
		}
		if fixture.deleting {
			cr.Finalizers = []string{"example.com/test-finalizer"}
		}
		if err := r.Create(ctx, cr); err != nil {
			t.Fatal(err)
		}
		if fixture.deleting {
			if err := r.Delete(ctx, cr); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, tc := range []struct {
		name      string
		object    *corev1.ConfigMap
		selector  string
		disableAI bool
		want      bool
	}{
		{
			name:   "deleted operator registry",
			object: &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: aiconfig.EnrollmentConfigMapName, Namespace: "operator"}},
			want:   true,
		},
		{
			name:   "same name in tenant namespace",
			object: &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: aiconfig.EnrollmentConfigMapName, Namespace: first.Namespace}},
		},
		{
			name:   "unrelated operator ConfigMap",
			object: &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: "operator"}},
		},
		{
			name:      "operator AI disabled",
			object:    &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: aiconfig.EnrollmentConfigMapName, Namespace: "operator"}},
			disableAI: true,
		},
		{
			name:     "invalid operator selector",
			object:   &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: aiconfig.EnrollmentConfigMapName, Namespace: "operator"}},
			selector: "invalid in (",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r.LabelSelector = "ai-scope=managed"
			if tc.selector != "" {
				r.LabelSelector = tc.selector
			}
			if tc.disableAI {
				t.Setenv(enableAIFeaturesEnv, "false")
			}
			queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
			defer queue.ShutDown()
			// Exercise the same event adapter registered by setResourceWatches.
			// Deleted ConfigMaps have no live object or enrollment data to read.
			h := handler.EnqueueRequestsFromMapFunc(r.aiNotificationDependencyRequests)
			h.Delete(ctx, event.DeleteEvent{Object: tc.object}, queue)
			want := map[types.NamespacedName]bool{}
			if tc.want {
				want[client.ObjectKeyFromObject(first)] = true
				want[types.NamespacedName{Name: "argocd", Namespace: "team-b"}] = true
			}
			if queue.Len() != len(want) {
				t.Fatalf("queued %d instances, want %d", queue.Len(), len(want))
			}
			for queue.Len() > 0 {
				request, _ := queue.Get()
				queue.Done(request)
				if !want[request.NamespacedName] {
					t.Fatalf("unexpected reconciliation: %v", request.NamespacedName)
				}
				delete(want, request.NamespacedName)
			}
			if len(want) != 0 {
				t.Fatalf("missing reconciliations: %v", want)
			}
		})
	}
}
