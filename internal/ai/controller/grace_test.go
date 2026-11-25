package controller

import (
	"context"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"
)

func TestRecreatedApplicationGetsFreshGrace(t *testing.T) {
	for _, elapsed := range []time.Duration{0, 2 * time.Second} {
		t.Run(elapsed.String(), func(t *testing.T) {
			s, dyn, now := incidentFixture(t)
			s.config.AutoDiagnosisGracePeriod = 90 * time.Second
			changeApp(t, dyn, func(app *unstructured.Unstructured) {
				app.Object["status"].(map[string]any)["health"] = map[string]any{
					"status": "Progressing", "lastTransitionTime": now.Add(-89 * time.Second).Format(time.RFC3339),
				}
			})
			ctx := context.Background()
			_, _, wait, _, err := s.reconcileDiagnosis(ctx, "gitops", "demo")
			if err != nil || wait != time.Second {
				t.Fatalf("initial grace: wait=%s err=%v", wait, err)
			}
			*now = now.Add(elapsed)
			if elapsed > time.Second {
				run := reconcileApp(t, s)
				if run == nil {
					t.Fatal("initial diagnosis was not created after grace expired")
				}
				finishRun(t, dyn, run, true, *now)
			}

			// Recreate without an intermediate reconcile that observes NotFound.
			// The name, revision, and managing ArgoCD instance are unchanged.
			apps := dyn.Resource(applicationGVR).Namespace("gitops")
			old, err := apps.Get(ctx, "demo", metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if err := apps.Delete(ctx, "demo", metav1.DeleteOptions{}); err != nil {
				t.Fatal(err)
			}
			replacement := old.DeepCopy()
			replacement.SetUID("replacement-application-uid")
			replacement.SetResourceVersion("")
			replacement.SetCreationTimestamp(metav1.NewTime(*now))
			replacement.SetAnnotations(nil)
			replacement.Object["status"].(map[string]any)["health"] = map[string]any{
				"status": "Progressing", "lastTransitionTime": now.Format(time.RFC3339),
			}
			if _, err := apps.Create(ctx, replacement, metav1.CreateOptions{}); err != nil {
				t.Fatal(err)
			}
			run, reason, wait, requeue, err := s.reconcileDiagnosis(ctx, "gitops", "demo")
			if err != nil || wait != 90*time.Second || requeue || run != nil {
				t.Fatalf("replacement did not receive fresh grace: reason=%q wait=%s requeue=%v run=%v err=%v", reason, wait, requeue, run, err)
			}
			// Repeated notifications for the replacement must use its stable timer.
			*now = now.Add(30 * time.Second)
			_, _, wait, _, err = s.reconcileDiagnosis(ctx, "gitops", "demo")
			if err != nil || wait != time.Minute {
				t.Fatalf("replacement grace did not advance: wait=%s err=%v", wait, err)
			}
		})
	}
}

func TestFutureHealthTimestampUsesObservedDiagnosisGrace(t *testing.T) {
	for _, health := range []string{"Progressing", "Missing"} {
		t.Run(health, func(t *testing.T) {
			s, dyn, now := incidentFixture(t)
			s.config.AutoDiagnosisGracePeriod = 10 * time.Minute
			setRecoveryTestHealth(t, dyn, health, now.Add(time.Hour))
			_, _, wait, _, err := s.reconcileDiagnosis(context.Background(), "gitops", "demo")
			if err != nil || wait != 10*time.Minute {
				t.Fatalf("future timestamp extended grace: wait=%s err=%v", wait, err)
			}
			countRuns(t, s, 0)

			// Later notifications with future timestamps retain the original deadline.
			*now = now.Add(time.Minute)
			setRecoveryTestHealth(t, dyn, health, now.Add(2*time.Hour))
			_, _, wait, _, err = s.reconcileDiagnosis(context.Background(), "gitops", "demo")
			if err != nil || wait != 9*time.Minute {
				t.Fatalf("repeated notification restarted grace: wait=%s err=%v", wait, err)
			}
			*now = now.Add(9 * time.Minute)
			if reconcileApp(t, s) == nil {
				t.Fatal("diagnosis did not start at the original deadline")
			}
			countRuns(t, s, 1)
		})
	}
}

func TestMissingApplicationClearsGrace(t *testing.T) {
	for _, missingOn := range []string{"first read", "second read"} {
		t.Run(missingOn, func(t *testing.T) {
			s, dyn, now := incidentFixture(t)
			changeApp(t, dyn, func(app *unstructured.Unstructured) {
				app.SetCreationTimestamp(metav1.NewTime(*now))
			})
			ctx := context.Background()
			if _, _, wait, _, err := s.reconcileDiagnosis(ctx, "gitops", "demo"); err != nil || wait == 0 {
				t.Fatalf("initial grace: wait=%s err=%v", wait, err)
			}
			reads := 0
			dyn.PrependReactor("get", "applications", func(action ktesting.Action) (bool, runtime.Object, error) {
				reads++
				if missingOn == "first read" || reads == 2 {
					return true, nil, apierrors.NewNotFound(applicationGVR.GroupResource(), "demo")
				}
				return false, nil, nil
			})
			if _, _, _, _, err := s.reconcileDiagnosis(ctx, "gitops", "demo"); !apierrors.IsNotFound(err) {
				t.Fatalf("expected missing Application, got %v", err)
			}
			if len(s.graceStarted) != 0 {
				t.Fatalf("missing Application retained grace timer: %v", s.graceStarted)
			}
		})
	}
}
