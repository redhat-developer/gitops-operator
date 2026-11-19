package operator

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/redhat-developer/gitops-operator/internal/ai/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestAgenticCleanupChecksEveryResourceOwner(t *testing.T) {
	resources := []client.Object{
		NewService(testServiceNamespace, true),
		NewServiceAccount(testServiceNamespace),
		NewRole("lightspeed"),
		NewRoleBinding("lightspeed", testServiceNamespace),
		NewEnrollmentRole(testServiceNamespace),
		NewEnrollmentRoleBinding(testServiceNamespace),
		NewClusterRole(),
		NewClusterRoleBinding(testServiceNamespace),
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: ServiceName + "-tls", Namespace: testServiceNamespace}},
	}
	for _, withDeployment := range []bool{false, true} {
		for _, resource := range resources {
			for _, owner := range []string{"", "another-controller", utils.Manager} {
				t.Run(fmt.Sprintf("deployment=%t/%T/%s/owner=%s", withDeployment, resource, resource.GetName(), owner), func(t *testing.T) {
					cl := agenticOperatorFixture(t)
					ctx := context.Background()
					if withDeployment {
						deployment := NewDeployment(testServiceNamespace, true)
						deployment.Labels = map[string]string{utils.ManagedBy: utils.Manager}
						if err := cl.Create(ctx, deployment); err != nil {
							t.Fatal(err)
						}
					}
					obj := resource.DeepCopyObject().(client.Object)
					if owner != "" {
						obj.SetLabels(map[string]string{utils.ManagedBy: owner})
					}
					if err := cl.Create(ctx, obj); err != nil {
						t.Fatal(err)
					}
					before := obj.DeepCopyObject().(client.Object)
					for attempt := 0; attempt < 2; attempt++ {
						if err := cleanupAIController(ctx, cl, testServiceNamespace, "lightspeed"); err != nil {
							t.Fatalf("cleanup attempt %d: %v", attempt+1, err)
						}
						err := cl.Get(ctx, client.ObjectKeyFromObject(obj), obj)
						if owner == utils.Manager {
							if !apierrors.IsNotFound(err) {
								t.Fatalf("managed resource retained: %v", err)
							}
						} else {
							if err != nil {
								t.Fatalf("unowned resource was removed: %v", err)
							}
							if !reflect.DeepEqual(before, obj) {
								t.Fatal("unowned resource was changed")
							}
						}
					}
					if withDeployment {
						err := cl.Get(ctx, client.ObjectKey{Name: ServiceName, Namespace: testServiceNamespace}, &appsv1.Deployment{})
						if !apierrors.IsNotFound(err) {
							t.Fatalf("managed deployment retained: %v", err)
						}
					}
				})
			}
		}
	}
}

func TestAgenticCleanupContinuesPastUnownedDeployment(t *testing.T) {
	for _, owner := range []string{"", "another-controller"} {
		t.Run(owner, func(t *testing.T) {
			cl := agenticOperatorFixture(t)
			ctx := context.Background()
			deployment := NewDeployment(testServiceNamespace, true)
			if owner != "" {
				deployment.Labels = map[string]string{utils.ManagedBy: owner}
			}
			unowned := []client.Object{deployment, NewService(testServiceNamespace, true)}
			managed := []client.Object{
				NewServiceAccount(testServiceNamespace),
				NewRole("lightspeed"), NewRoleBinding("lightspeed", testServiceNamespace),
				NewEnrollmentRole(testServiceNamespace), NewEnrollmentRoleBinding(testServiceNamespace),
				NewClusterRole(), NewClusterRoleBinding(testServiceNamespace),
			}
			for _, obj := range managed {
				obj.SetLabels(map[string]string{utils.ManagedBy: utils.Manager})
				if err := cl.Create(ctx, obj); err != nil {
					t.Fatal(err)
				}
			}
			for _, obj := range unowned {
				if err := cl.Create(ctx, obj); err != nil {
					t.Fatal(err)
				}
			}
			for attempt := 0; attempt < 2; attempt++ {
				if err := cleanupAIController(ctx, cl, testServiceNamespace, "lightspeed"); err != nil {
					t.Fatalf("cleanup attempt %d: %v", attempt+1, err)
				}
				for _, obj := range managed {
					if err := cl.Get(ctx, client.ObjectKeyFromObject(obj), obj); !apierrors.IsNotFound(err) {
						t.Fatalf("managed %T retained: %v", obj, err)
					}
				}
				for _, before := range unowned {
					current := before.DeepCopyObject().(client.Object)
					if err := cl.Get(ctx, client.ObjectKeyFromObject(before), current); err != nil {
						t.Fatalf("unmanaged %T was removed: %v", before, err)
					}
					if !reflect.DeepEqual(before, current) {
						t.Fatalf("unmanaged %T was modified", before)
					}
				}
			}
		})
	}
}

func TestAgenticCleanupPreservesResourceWhenOwnershipLookupFails(t *testing.T) {
	cl := agenticOperatorFixture(t)
	ctx := context.Background()
	service := NewService(testServiceNamespace, true)
	service.Labels = map[string]string{utils.ManagedBy: utils.Manager}
	if err := cl.Create(ctx, service); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("temporary API failure")
	wrapped := interceptor.NewClient(cl.(client.WithWatch), interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if _, ok := obj.(*corev1.Service); ok {
				return wantErr
			}
			return c.Get(ctx, key, obj, opts...)
		},
	})
	if err := cleanupAIController(ctx, wrapped, testServiceNamespace, "lightspeed"); !errors.Is(err, wantErr) {
		t.Fatalf("got error %v, want %v", err, wantErr)
	}
	if err := cl.Get(ctx, client.ObjectKeyFromObject(service), service); err != nil {
		t.Fatalf("resource removed after ownership lookup failed: %v", err)
	}
}

func TestAgenticCleanupPreservesResourceChangedBeforeDeletion(t *testing.T) {
	cl := agenticOperatorFixture(t)
	ctx := context.Background()
	service := NewService(testServiceNamespace, true)
	service.Labels = map[string]string{utils.ManagedBy: utils.Manager}
	if err := cl.Create(ctx, service); err != nil {
		t.Fatal(err)
	}
	deleteCalled := false
	wrapped := interceptor.NewClient(cl.(client.WithWatch), interceptor.Funcs{
		Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			if _, ok := obj.(*corev1.Service); !ok {
				return c.Delete(ctx, obj, opts...)
			}
			deleteCalled = true
			current := &corev1.Service{}
			if err := c.Get(ctx, client.ObjectKeyFromObject(obj), current); err != nil {
				return err
			}
			current.Labels[utils.ManagedBy] = "another-controller"
			if err := c.Update(ctx, current); err != nil {
				return err
			}
			return c.Delete(ctx, obj, opts...)
		},
	})
	err := cleanupAIController(ctx, wrapped, testServiceNamespace, "lightspeed")
	if !deleteCalled || !apierrors.IsConflict(err) {
		t.Fatalf("concurrent change did not prevent deletion: called=%t, err=%v", deleteCalled, err)
	}
	if err := cl.Get(ctx, client.ObjectKeyFromObject(service), service); err != nil {
		t.Fatalf("changed resource was removed: %v", err)
	}
	if service.Labels[utils.ManagedBy] != "another-controller" {
		t.Fatal("new owner's label was changed")
	}
	// A later reconciliation must recognize the changed ownership and skip it.
	if err := cleanupAIController(ctx, cl, testServiceNamespace, "lightspeed"); err != nil {
		t.Fatal(err)
	}
	if err := cl.Get(ctx, client.ObjectKeyFromObject(service), service); err != nil {
		t.Fatalf("changed resource was removed on retry: %v", err)
	}
}
