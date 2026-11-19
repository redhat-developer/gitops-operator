package argocd

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/redhat-developer/gitops-operator/argocd-operator/api/v1alpha1"
	aiconfig "github.com/redhat-developer/gitops-operator/argocd-operator/pkg/ai"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestAINotificationsPreservesUnownedResources(t *testing.T) {
	for _, object := range []client.Object{
		&v1alpha1.NotificationsConfiguration{ObjectMeta: metav1.ObjectMeta{Name: aiNotificationsConfigurationName}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: aiTriggerTokenSecretName}, Data: map[string][]byte{"token": []byte("unrelated")}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: aiNotificationsSAName}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: aiCAConfigMapName}, Data: map[string]string{aiCABundleKey: "unrelated"}},
	} {
		for _, ownership := range []string{"unowned", "another-instance", "previous-incarnation"} {
			t.Run(object.GetName()+"/"+ownership, func(t *testing.T) {
				r, cr := aiNotificationsFixture(t, true)
				ctx := context.Background()
				obj := object.DeepCopyObject().(client.Object)
				obj.SetNamespace(cr.Namespace)
				if ownership != "unowned" {
					other := cr.DeepCopy()
					other.UID = "another-uid"
					if ownership == "another-instance" {
						other.Name = "another-instance"
					}
					meta := aiNotificationObjectMeta(other, obj.GetName())
					obj.SetOwnerReferences(meta.OwnerReferences)
					obj.SetLabels(meta.Labels)
				}
				if _, ok := obj.(*corev1.ServiceAccount); ok {
					if err := r.Delete(ctx, aiNotificationsServiceAccount(cr)); err != nil {
						t.Fatal(err)
					}
				}
				if err := r.Create(ctx, obj); err != nil {
					t.Fatal(err)
				}
				before := obj.DeepCopyObject()
				if err := r.reconcileAINotifications(cr); err == nil || !strings.Contains(err.Error(), "not owned") {
					t.Fatalf("expected an ownership conflict, got %v", err)
				}
				cr.Spec.AI.Troubleshooting = false
				if err := r.reconcileAINotifications(cr); err != nil {
					t.Fatal(err)
				}
				if err := r.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
					t.Fatalf("cleanup deleted unrelated resource: %v", err)
				}
				if !reflect.DeepEqual(before, obj) {
					t.Fatal("reconciliation modified an unrelated resource")
				}
			})
		}
	}
}

func TestAINotificationsCleanupPreservesUnownedEntries(t *testing.T) {
	for _, owner := range []string{"", "successor-uid"} {
		t.Run("owner="+owner, func(t *testing.T) {
			r, cr := aiNotificationsFixture(t, true)
			t.Setenv(enableAIFeaturesEnv, "false")
			cr.Spec.AI = nil
			ctx := context.Background()
			objects := []client.Object{
				&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: aiNotificationsSecretName, Namespace: cr.Namespace}, Data: map[string][]byte{aiTriggerTokenKey: []byte("keep")}},
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: aiTLSCertsConfigMapName, Namespace: cr.Namespace}, Data: map[string]string{"gitops-ai-controller." + cr.Namespace + ".svc": "keep"}},
			}
			for _, obj := range objects {
				if owner != "" {
					obj.SetAnnotations(map[string]string{aiNotificationsOwnerAnnotation: owner})
				}
				if err := r.Create(ctx, obj); err != nil {
					t.Fatal(err)
				}
			}
			if err := r.reconcileAINotifications(cr); err != nil {
				t.Fatal(err)
			}
			for _, obj := range objects {
				before := obj.DeepCopyObject()
				if err := r.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(before, obj) {
					t.Fatal("cleanup changed an entry it does not own")
				}
			}
		})
	}
}

func TestAINotificationsRestoresConfigurationAndPreservesDefault(t *testing.T) {
	r, cr := aiNotificationsFixture(t, false)
	ctx := context.Background()
	defaultNC := &v1alpha1.NotificationsConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "default-notifications-configuration", Namespace: cr.Namespace},
		Spec:       v1alpha1.NotificationsConfigurationSpec{Triggers: map[string]string{"trigger.user": "keep"}},
	}
	if err := r.Create(ctx, defaultNC); err != nil {
		t.Fatal(err)
	}
	defaultBefore := defaultNC.DeepCopy()
	pending := func() {
		t.Helper()
		if err := r.reconcileAINotifications(cr); !errors.Is(err, errAINotificationsPending) {
			t.Fatalf("expected token provisioning to be pending, got %v", err)
		}
	}
	pending()
	nc := &v1alpha1.NotificationsConfiguration{}
	key := client.ObjectKey{Name: aiNotificationsConfigurationName, Namespace: cr.Namespace}
	if err := r.Get(ctx, key, nc); err != nil {
		t.Fatal(err)
	}
	desired := nc.DeepCopy()
	if !aiNotificationOwnedBy(cr, nc) || nc.Labels[aiconfig.EnrollmentManagedByLabel] != aiconfig.EnrollmentManager {
		t.Fatal("AI configuration was created without management metadata")
	}
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
	defer queue.ShutDown()
	events := handler.EnqueueRequestsFromMapFunc(r.aiNotificationDependencyRequests)
	for _, action := range []string{"edit", "delete"} {
		switch action {
		case "edit":
			before := nc.DeepCopy()
			nc.Spec = v1alpha1.NotificationsConfigurationSpec{Services: map[string]string{aiNotifServiceKey: "url: https://untrusted.invalid"}}
			delete(nc.Labels, aiconfig.EnrollmentManagedByLabel)
			if err := r.Update(ctx, nc); err != nil {
				t.Fatal(err)
			}
			events.Update(ctx, event.UpdateEvent{ObjectOld: before, ObjectNew: nc}, queue)
		case "delete":
			if err := r.Delete(ctx, nc); err != nil {
				t.Fatal(err)
			}
			events.Delete(ctx, event.DeleteEvent{Object: nc}, queue)
		}
		if queue.Len() != 1 {
			t.Fatalf("%s enqueued %d repairs, want 1", action, queue.Len())
		}
		request, _ := queue.Get()
		if request.NamespacedName != client.ObjectKeyFromObject(cr) {
			t.Fatalf("%s enqueued the wrong ArgoCD: %v", action, request)
		}
		queue.Done(request)
		pending()
		if err := r.Get(ctx, key, nc); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(nc.Spec, desired.Spec) || !aiNotificationOwnedBy(cr, nc) || nc.Labels[aiconfig.EnrollmentManagedByLabel] != aiconfig.EnrollmentManager {
			t.Fatalf("%s did not restore operator-managed configuration", action)
		}
	}
	if err := r.Get(ctx, client.ObjectKeyFromObject(defaultNC), defaultNC); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(defaultBefore, defaultNC) {
		t.Fatal("AI reconciliation changed the user-managed default configuration")
	}
}

func TestAINotificationsCleanupRejectsConcurrentOwnershipChange(t *testing.T) {
	r, cr := aiNotificationsFixture(t, false)
	ctx := context.Background()
	obj := &corev1.Secret{ObjectMeta: aiNotificationObjectMeta(cr, aiTriggerTokenSecretName)}
	obj.UID = "original-secret-uid"
	if err := r.Create(ctx, obj); err != nil {
		t.Fatal(err)
	}
	originalVersion := obj.ResourceVersion
	r.Client = interceptor.NewClient(r.Client.(client.WithWatch), interceptor.Funcs{
		Delete: func(ctx context.Context, cl client.WithWatch, obj client.Object, options ...client.DeleteOption) error {
			opts := (&client.DeleteOptions{}).ApplyOptions(options)
			if opts.Preconditions == nil || opts.Preconditions.UID == nil || *opts.Preconditions.UID != "original-secret-uid" || opts.Preconditions.ResourceVersion == nil || *opts.Preconditions.ResourceVersion != originalVersion {
				t.Fatal("cleanup omitted resource identity or version preconditions")
			}
			current := &corev1.Secret{}
			if err := cl.Get(ctx, client.ObjectKeyFromObject(obj), current); err != nil {
				return err
			}
			current.OwnerReferences[0].UID = "successor-uid"
			if err := cl.Update(ctx, current); err != nil {
				return err
			}
			return cl.Delete(ctx, obj, options...)
		},
	})
	if err := r.deleteAINotificationResource(cr, obj.Name, &corev1.Secret{}); !apierrors.IsConflict(err) {
		t.Fatalf("expected concurrent ownership change to reject deletion, got %v", err)
	}
	if err := r.deleteAINotificationResource(cr, obj.Name, &corev1.Secret{}); err != nil {
		t.Fatalf("cleanup retry should preserve the new owner's resource: %v", err)
	}
	if err := r.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
		t.Fatalf("new owner's resource was deleted: %v", err)
	}
}
