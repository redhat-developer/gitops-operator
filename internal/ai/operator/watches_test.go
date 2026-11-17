package operator

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	argoapp "github.com/redhat-developer/gitops-operator/argocd-operator/api/v1beta1"
	"github.com/redhat-developer/gitops-operator/common"
	"github.com/redhat-developer/gitops-operator/internal/ai/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const watchOperatorNamespace = "operator"

func watchFixture(t *testing.T) (*GitOpsAIControllerReconciler, workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	t.Helper()
	r := &GitOpsAIControllerReconciler{
		Client: agenticOperatorFixture(t), EnableAIFeatures: true,
		OperatorNamespace: watchOperatorNamespace,
	}
	request := reconcile.Request{NamespacedName: client.ObjectKey{Name: common.ArgoCDInstanceName, Namespace: testServiceNamespace}}
	if _, err := r.Reconcile(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
	t.Cleanup(queue.ShutDown)
	return r, queue
}

func sharedResourceObjects() []client.Object {
	return []client.Object{
		NewDeployment(watchOperatorNamespace, true), NewService(watchOperatorNamespace, true),
		NewServiceAccount(watchOperatorNamespace), NewClusterRole(), NewClusterRoleBinding(watchOperatorNamespace),
		NewRole("lightspeed"), NewRoleBinding("lightspeed", watchOperatorNamespace),
		NewEnrollmentRole(watchOperatorNamespace), NewEnrollmentRoleBinding(watchOperatorNamespace),
	}
}

func takeSharedRequest(t *testing.T, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) reconcile.Request {
	t.Helper()
	if queue.Len() != 1 {
		t.Fatalf("event enqueued %d requests, want one shared reconciliation", queue.Len())
	}
	request, shutdown := queue.Get()
	if shutdown || request.Namespace != "" || request.Name != ServiceName {
		t.Fatalf("unexpected shared reconciliation request: %+v, shutdown=%v", request, shutdown)
	}
	queue.Done(request)
	return request
}

func TestManagedResourceDeleteEventRecreatesResource(t *testing.T) {
	for _, resource := range sharedResourceObjects() {
		t.Run(fmt.Sprintf("%T/%s", resource, resource.GetName()), func(t *testing.T) {
			r, queue := watchFixture(t)
			ctx := context.Background()
			key := client.ObjectKeyFromObject(resource)
			if err := r.Client.Get(ctx, key, resource); err != nil {
				t.Fatal(err)
			}
			if len(resource.GetOwnerReferences()) != 0 {
				t.Fatal("shared resources must not depend on one ArgoCD owner")
			}
			if err := r.Client.Delete(ctx, resource); err != nil {
				t.Fatal(err)
			}
			// Exercise the same event handler used by SetupWithManager. The
			// queue, rather than the test, supplies the reconciliation key.
			r.managedResourceEventHandler().Delete(ctx, event.DeleteEvent{Object: resource, DeleteStateUnknown: true}, queue)
			if _, err := r.Reconcile(ctx, takeSharedRequest(t, queue)); err != nil {
				t.Fatal(err)
			}
			if err := r.Client.Get(ctx, key, resource); err != nil {
				t.Fatalf("deleted resource was not recreated: %v", err)
			}
			if !isManagedResource(resource) {
				t.Fatal("recreated resource is missing its management identity")
			}
		})
	}
}

func TestManagedResourceEventsIgnoreUnrelatedObjects(t *testing.T) {
	r, queue := watchFixture(t)
	ctx := context.Background()
	for _, resource := range sharedResourceObjects() {
		for _, change := range []string{"unowned", "different-manager", "different-name", "different-namespace"} {
			t.Run(fmt.Sprintf("%T/%s/%s", resource, resource.GetName(), change), func(t *testing.T) {
				obj := resource.DeepCopyObject().(client.Object)
				obj.SetLabels(map[string]string{utils.ManagedBy: utils.Manager})
				switch change {
				case "unowned":
					obj.SetLabels(nil)
				case "different-manager":
					obj.SetLabels(map[string]string{utils.ManagedBy: "another-controller"})
				case "different-name":
					obj.SetName("unrelated")
				case "different-namespace":
					obj.SetNamespace("unrelated")
				}
				r.managedResourceEventHandler().Delete(ctx, event.DeleteEvent{Object: obj}, queue)
				if queue.Len() != 0 {
					t.Fatal("unrelated object scheduled AI reconciliation")
				}
			})
		}
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Name: ServiceName, Namespace: watchOperatorNamespace,
		Labels: map[string]string{utils.ManagedBy: utils.Manager},
	}}
	r.managedResourceEventHandler().Delete(ctx, event.DeleteEvent{Object: secret}, queue)
	r.managedResourceEventHandler().Delete(ctx, event.DeleteEvent{}, queue)
	if queue.Len() != 0 {
		t.Fatal("unsupported or absent object scheduled AI reconciliation")
	}
}

func TestManagedResourceUpdateEventRepairsDriftAndSettles(t *testing.T) {
	r, queue := watchFixture(t)
	ctx := context.Background()
	deployment := &appsv1.Deployment{}
	key := client.ObjectKey{Name: ServiceName, Namespace: watchOperatorNamespace}
	if err := r.Client.Get(ctx, key, deployment); err != nil {
		t.Fatal(err)
	}
	before := deployment.DeepCopy()
	*deployment.Spec.Replicas = 0
	deployment.Spec.Template.Spec.Containers[0].Image = "unexpected-image"
	if err := r.Client.Update(ctx, deployment); err != nil {
		t.Fatal(err)
	}
	r.managedResourceEventHandler().Update(ctx, event.UpdateEvent{ObjectOld: before, ObjectNew: deployment}, queue)
	if _, err := r.Reconcile(ctx, takeSharedRequest(t, queue)); err != nil {
		t.Fatal(err)
	}
	if err := r.Client.Get(ctx, key, deployment); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.Spec, deployment.Spec) {
		t.Fatal("update event did not restore the desired Deployment")
	}
	// A subsequent event for the restored resource must not produce another
	// write, otherwise the new watches could create a reconciliation loop.
	version := deployment.ResourceVersion
	var patched []string
	r.Client = interceptor.NewClient(r.Client.(client.WithWatch), interceptor.Funcs{
		Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			patched = append(patched, fmt.Sprintf("%T/%s", obj, obj.GetName()))
			return c.Patch(ctx, obj, patch, opts...)
		},
	})
	r.managedResourceEventHandler().Update(ctx, event.UpdateEvent{ObjectOld: deployment, ObjectNew: deployment}, queue)
	if _, err := r.Reconcile(ctx, takeSharedRequest(t, queue)); err != nil {
		t.Fatal(err)
	}
	if err := r.Client.Get(ctx, key, deployment); err != nil {
		t.Fatal(err)
	}
	if deployment.ResourceVersion != version {
		t.Fatal("already-reconciled Deployment was written again")
	}
	if len(patched) != 0 {
		t.Fatalf("already-reconciled resources were patched again: %v", patched)
	}
}

func TestManagedResourceUpdateUsesOldManagementIdentity(t *testing.T) {
	r, queue := watchFixture(t)
	ctx := context.Background()
	deployment := &appsv1.Deployment{}
	key := client.ObjectKey{Name: ServiceName, Namespace: watchOperatorNamespace}
	if err := r.Client.Get(ctx, key, deployment); err != nil {
		t.Fatal(err)
	}
	before := deployment.DeepCopy()
	deployment.Labels = nil
	if err := r.Client.Update(ctx, deployment); err != nil {
		t.Fatal(err)
	}
	r.managedResourceEventHandler().Update(ctx, event.UpdateEvent{ObjectOld: before, ObjectNew: deployment}, queue)
	if _, err := r.Reconcile(ctx, takeSharedRequest(t, queue)); err == nil {
		t.Fatal("reconciliation should preserve the resource after ownership was removed")
	}
	if err := r.Client.Get(ctx, key, deployment); err != nil {
		t.Fatal(err)
	}
	if isManagedResource(deployment) {
		t.Fatal("reconciliation adopted an unowned resource")
	}
}

func TestManagedResourceDeleteEventHonorsDisable(t *testing.T) {
	r, queue := watchFixture(t)
	ctx := context.Background()
	service := &corev1.Service{}
	key := client.ObjectKey{Name: ServiceName, Namespace: watchOperatorNamespace}
	if err := r.Client.Get(ctx, key, service); err != nil {
		t.Fatal(err)
	}
	if err := r.Client.Delete(ctx, service); err != nil {
		t.Fatal(err)
	}
	r.EnableAIFeatures = false
	r.managedResourceEventHandler().Delete(ctx, event.DeleteEvent{Object: service}, queue)
	if _, err := r.Reconcile(ctx, takeSharedRequest(t, queue)); err != nil {
		t.Fatal(err)
	}
	for _, obj := range sharedResourceObjects() {
		if err := r.Client.Get(ctx, client.ObjectKeyFromObject(obj), obj); !apierrors.IsNotFound(err) {
			t.Fatalf("disabled AI recreated or retained %T: %v", obj, err)
		}
	}
}

func TestManagedResourceRecoveryDoesNotDependOnOriginalInstance(t *testing.T) {
	for _, anotherInstance := range []bool{false, true} {
		t.Run(fmt.Sprintf("another-instance=%v", anotherInstance), func(t *testing.T) {
			r, queue := watchFixture(t)
			ctx := context.Background()
			if anotherInstance {
				second := &argoapp.ArgoCD{ObjectMeta: metav1.ObjectMeta{Name: "argocd", Namespace: "team-b"}}
				if err := r.Client.Create(ctx, second); err != nil {
					t.Fatal(err)
				}
			}
			original := &argoapp.ArgoCD{ObjectMeta: metav1.ObjectMeta{Name: common.ArgoCDInstanceName, Namespace: testServiceNamespace}}
			if err := r.Client.Delete(ctx, original); err != nil {
				t.Fatal(err)
			}
			service := &corev1.Service{}
			key := client.ObjectKey{Name: ServiceName, Namespace: watchOperatorNamespace}
			if err := r.Client.Get(ctx, key, service); err != nil {
				t.Fatal(err)
			}
			if err := r.Client.Delete(ctx, service); err != nil {
				t.Fatal(err)
			}
			r.managedResourceEventHandler().Delete(ctx, event.DeleteEvent{Object: service}, queue)
			if _, err := r.Reconcile(ctx, takeSharedRequest(t, queue)); err != nil {
				t.Fatal(err)
			}
			err := r.Client.Get(ctx, key, service)
			if anotherInstance && err != nil {
				t.Fatalf("remaining instance did not permit shared resource recovery: %v", err)
			}
			if !anotherInstance && !apierrors.IsNotFound(err) {
				t.Fatalf("resource provisioned without any ArgoCD: %v", err)
			}
		})
	}
}

func TestManagedResourceRecoveryReturnsListErrorsForRetry(t *testing.T) {
	r, queue := watchFixture(t)
	ctx := context.Background()
	service := &corev1.Service{}
	key := client.ObjectKey{Name: ServiceName, Namespace: watchOperatorNamespace}
	if err := r.Client.Get(ctx, key, service); err != nil {
		t.Fatal(err)
	}
	if err := r.Client.Delete(ctx, service); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("temporary list failure")
	baseClient := r.Client
	r.Client = interceptor.NewClient(baseClient.(client.WithWatch), interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(*argoapp.ArgoCDList); ok {
				return wantErr
			}
			return c.List(ctx, list, opts...)
		},
	})
	r.managedResourceEventHandler().Delete(ctx, event.DeleteEvent{Object: service}, queue)
	request := takeSharedRequest(t, queue)
	if _, err := r.Reconcile(ctx, request); !errors.Is(err, wantErr) {
		t.Fatalf("list failure must reach controller-runtime for retry: %v", err)
	}
	r.Client = baseClient
	if _, err := r.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := r.Client.Get(ctx, key, service); err != nil {
		t.Fatalf("retry did not recreate the Service: %v", err)
	}
}
