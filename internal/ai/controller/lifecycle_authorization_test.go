package controller

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	aiconfig "github.com/redhat-developer/gitops-operator/argocd-operator/pkg/ai"
	"github.com/redhat-developer/gitops-operator/internal/ai/utils"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ktesting "k8s.io/client-go/testing"
)

var lifecycleRevocations = []string{"disabled", "deleted", "terminating", "replaced and enrolled", "enrollment removed"}

// Change authorization after the webhook has already passed TokenReview and
// instance checks. Re-enrolling a replacement proves the worker checks the
// original instance UID, not just whether some enabled instance now exists.
func revokeLifecycleInstance(t *testing.T, s *TriggerService, change string) {
	t.Helper()
	ctx := context.Background()
	instances := s.dynClient.Resource(argocdGVR).Namespace("gitops")
	instance, err := instances.Get(ctx, "argocd", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	switch change {
	case "disabled":
		if err := unstructured.SetNestedField(instance.Object, false, "spec", "ai", "troubleshooting"); err != nil {
			t.Fatal(err)
		}
	case "terminating":
		now := metav1.Now()
		instance.SetDeletionTimestamp(&now)
	case "deleted", "replaced and enrolled":
		if err := instances.Delete(ctx, instance.GetName(), metav1.DeleteOptions{}); err != nil {
			t.Fatal(err)
		}
		if change == "deleted" {
			return
		}
		instance.SetUID(types.UID(string(instance.GetUID()) + "-replacement"))
		if _, err := instances.Create(ctx, instance, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		registry := testEnrollmentRegistry(instance)
		if _, err := s.kubeClient.CoreV1().ConfigMaps(testOperatorNamespace).Update(ctx, registry, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
		return
	case "enrollment removed":
		if err := s.kubeClient.CoreV1().ConfigMaps(testOperatorNamespace).Delete(ctx, aiconfig.EnrollmentConfigMapName, metav1.DeleteOptions{}); err != nil {
			t.Fatal(err)
		}
		return
	default:
		t.Fatalf("unknown revocation %q", change)
	}
	if _, err := instances.Update(ctx, instance, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestQueuedDiagnosisRevalidatesInstanceAfterGrace(t *testing.T) {
	for _, change := range lifecycleRevocations {
		t.Run(change, func(t *testing.T) {
			s, dyn, now := incidentFixture(t)
			grace := 20 * time.Millisecond
			s.config.AutoDiagnosisGracePeriod = grace
			changeApp(t, dyn, func(app *unstructured.Unstructured) {
				app.SetCreationTimestamp(metav1.NewTime(*now))
			})
			sendDiagnosisNotification(t, s)
			processQueuedApplication(t, s) // Schedule the grace deadline.
			countRuns(t, s, 0)

			revokeLifecycleInstance(t, s, change)
			*now = now.Add(grace)
			waitForQueuedApplication(t, s)
			processQueuedApplication(t, s)
			countRuns(t, s, 0)
			if s.queue.Len() != 0 || s.queue.NumRequeues("gitops/demo/argocd-gitops-argocd") != 0 {
				t.Fatal("revoked diagnosis was queued for another attempt")
			}
			if _, ok := s.graceStarted["gitops/demo/argocd-gitops-argocd"]; ok {
				t.Fatal("revoked diagnosis retained its grace clock")
			}
		})
	}
}

func TestRetryRevalidatesInstanceAfterCooldown(t *testing.T) {
	for _, change := range lifecycleRevocations {
		t.Run(change, func(t *testing.T) {
			s, dyn, now := incidentFixture(t)
			s.config.Cooldown = 20 * time.Millisecond
			sendDiagnosisNotification(t, s)
			processQueuedApplication(t, s) // Persist the authorized incident.
			processQueuedApplication(t, s) // Create attempt 1.
			app, err := s.getApplication(context.Background(), "gitops", "demo")
			if err != nil {
				t.Fatal(err)
			}
			state, err := decodeIncident(app)
			if err != nil || state == nil {
				t.Fatalf("missing initial incident: %v", err)
			}
			first := mustGetRun(t, s, state.RunName)
			finishRun(t, dyn, first, false, *now)
			s.enqueueRunOwner(context.Background(), mustGetRun(t, s, state.RunName))
			processQueuedApplication(t, s) // Schedule the failed-run cooldown.

			revokeLifecycleInstance(t, s, change)
			*now = now.Add(s.config.Cooldown)
			waitForQueuedApplication(t, s)
			processQueuedApplication(t, s)
			countRuns(t, s, 1)
			if s.queue.Len() != 0 || s.queue.NumRequeues("gitops/demo/argocd-gitops-argocd") != 0 {
				t.Fatal("revoked retry was rescheduled")
			}
			app, err = s.getApplication(context.Background(), "gitops", "demo")
			if err != nil {
				t.Fatal(err)
			}
			state, err = decodeIncident(app)
			if err != nil || state == nil || state.Attempt != 1 {
				t.Fatalf("revocation changed retry budget: state=%+v err=%v", state, err)
			}
		})
	}
}

func TestRunEventRejectsRevokedInstance(t *testing.T) {
	for _, change := range lifecycleRevocations {
		t.Run(change, func(t *testing.T) {
			s, dyn, now := incidentFixture(t)
			first := reconcileApp(t, s)
			finishRun(t, dyn, first, false, *now)
			revokeLifecycleInstance(t, s, change)
			s.enqueueRunOwner(context.Background(), mustGetRun(t, s, first.GetName()))
			if s.queue.Len() != 0 {
				t.Fatal("run event queued work for a revoked instance")
			}
			countRuns(t, s, 1)
		})
	}
}

func TestLifecycleAuthorizationAPIFailureIsRetried(t *testing.T) {
	s, dyn, _ := incidentFixture(t)
	sendDiagnosisNotification(t, s)
	fail := true
	dyn.PrependReactor("list", "argocds", func(ktesting.Action) (bool, runtime.Object, error) {
		if fail {
			fail = false
			return true, nil, apierrors.NewServiceUnavailable("temporary instance lookup failure")
		}
		return false, nil, nil
	})
	processQueuedApplication(t, s)
	countRuns(t, s, 0)
	if s.queue.NumRequeues("gitops/demo/argocd-gitops-argocd") != 1 {
		t.Fatal("temporary authorization lookup failure was not retried")
	}
	waitForQueuedApplication(t, s)
	processQueuedApplication(t, s)
	processQueuedApplication(t, s)
	countRuns(t, s, 1)
}

func TestMissingDiagnosisOwnershipDoesNotRestoreRetryBudget(t *testing.T) {
	for _, missingFrom := range []string{"incident", "run"} {
		t.Run(missingFrom, func(t *testing.T) {
			s, dyn, _ := incidentFixture(t)
			first := reconcileApp(t, s)
			changeApp(t, dyn, func(app *unstructured.Unstructured) {
				annotations := app.GetAnnotations()
				if missingFrom == "run" {
					delete(annotations, utils.IncidentAnnotation)
				} else {
					state, err := decodeIncident(app)
					if err != nil {
						t.Fatal(err)
					}
					state.InstanceUID = ""
					encoded, err := json.Marshal(state)
					if err != nil {
						t.Fatal(err)
					}
					annotations[utils.IncidentAnnotation] = string(encoded)
				}
				app.SetAnnotations(annotations)
			})
			if missingFrom == "run" {
				run := mustGetRun(t, s, first.GetName())
				annotations := run.GetAnnotations()
				delete(annotations, instanceUIDAnnotation)
				run.SetAnnotations(annotations)
				if err := dyn.Tracker().Update(agenticRunGVR, run, run.GetNamespace()); err != nil {
					t.Fatal(err)
				}
				s.enqueueRunOwner(context.Background(), run)
				if s.queue.Len() != 0 {
					t.Fatal("run without an instance identity queued work")
				}
			}
			before, err := s.getApplication(context.Background(), "gitops", "demo")
			if err != nil {
				t.Fatal(err)
			}
			_, _, _, _, err = s.reconcileDiagnosis(context.Background(), "gitops", "demo")
			var terminal *terminalDiagnosisError
			if !errors.As(err, &terminal) {
				t.Fatalf("missing instance identity did not fail closed: %v", err)
			}
			after, err := s.getApplication(context.Background(), "gitops", "demo")
			if err != nil {
				t.Fatal(err)
			}
			if before.GetAnnotations()[utils.IncidentAnnotation] != after.GetAnnotations()[utils.IncidentAnnotation] {
				t.Fatal("incident was rewritten instead of preserving its retry budget")
			}
			countRuns(t, s, 1)
		})
	}
}

func TestRunEventRetriesTemporaryAuthorizationFailure(t *testing.T) {
	s, dyn, _ := incidentFixture(t)
	first := reconcileApp(t, s)
	lookups := 0
	dyn.PrependReactor("list", "argocds", func(ktesting.Action) (bool, runtime.Object, error) {
		lookups++
		if lookups == 1 {
			return true, nil, apierrors.NewServiceUnavailable("temporary instance lookup failure")
		}
		return false, nil, nil
	})
	s.enqueueRunOwner(context.Background(), first)
	if lookups < 2 || s.queue.Len() != 1 {
		t.Fatalf("run event was lost after temporary authorization failure: lookups=%d queue=%d", lookups, s.queue.Len())
	}
}

func TestReplacementCannotInheritPreviousInstanceDiagnosis(t *testing.T) {
	for _, persisted := range []bool{true, false} {
		name := "adopting an old run"
		if persisted {
			name = "persisted incident"
		}
		t.Run(name, func(t *testing.T) {
			s, dyn, _ := incidentFixture(t)
			reconcileApp(t, s)
			if !persisted {
				changeApp(t, dyn, func(app *unstructured.Unstructured) {
					annotations := app.GetAnnotations()
					delete(annotations, utils.IncidentAnnotation)
					app.SetAnnotations(annotations)
				})
			}
			revokeLifecycleInstance(t, s, "replaced and enrolled")
			// A fresh webhook is authorized for the replacement, but it cannot
			// claim the original instance's incident or reset its retry budget.
			sendDiagnosisNotification(t, s)
			before, err := s.getApplication(context.Background(), "gitops", "demo")
			if err != nil {
				t.Fatal(err)
			}
			processQueuedApplication(t, s)
			if s.queue.Len() != 0 || s.queue.NumRequeues("gitops/demo/argocd-gitops-argocd") != 0 {
				t.Fatal("replacement inherited an old diagnosis")
			}
			after, err := s.getApplication(context.Background(), "gitops", "demo")
			if err != nil {
				t.Fatal(err)
			}
			if before.GetAnnotations()[utils.IncidentAnnotation] != after.GetAnnotations()[utils.IncidentAnnotation] {
				t.Fatal("replacement rewrote the old incident")
			}
			countRuns(t, s, 1)
		})
	}
}

type delayedDiagnosisBody struct {
	reading chan struct{}
	release chan struct{}
	body    io.Reader
}

func (b *delayedDiagnosisBody) Read(p []byte) (int, error) {
	select {
	case <-b.reading:
	default:
		close(b.reading)
	}
	<-b.release
	return b.body.Read(p)
}

func TestStaleWebhookCannotReplaceNewInstanceQueuedWork(t *testing.T) {
	for _, newRequestFirst := range []bool{true, false} {
		name := "stale request finishes first"
		if newRequestFirst {
			name = "new request finishes first"
		}
		t.Run(name, func(t *testing.T) {
			s, _, _ := incidentFixture(t)
			body := &delayedDiagnosisBody{
				reading: make(chan struct{}), release: make(chan struct{}),
				body: strings.NewReader(`{"application":"demo","applicationNamespace":"gitops"}`),
			}
			request := httptest.NewRequest(http.MethodPost, "/api/v1/diagnose", body)
			request.Header.Set("Authorization", "Bearer token")
			response := httptest.NewRecorder()
			done := make(chan struct{})
			go func() {
				defer close(done)
				s.HandleDiagnose(response, request)
			}()
			t.Cleanup(func() {
				select {
				case <-body.release:
				default:
					close(body.release)
				}
				<-done
			})
			select {
			case <-body.reading: // The old request passed authorization.
			case <-time.After(3 * time.Second):
				t.Fatal("request did not reach body decoding")
			}
			revokeLifecycleInstance(t, s, "replaced and enrolled")
			if newRequestFirst {
				sendDiagnosisNotification(t, s)
			}
			close(body.release)
			<-done
			if response.Code != http.StatusAccepted {
				t.Fatalf("old request did not finish enqueueing: status=%d", response.Code)
			}
			if !newRequestFirst {
				sendDiagnosisNotification(t, s)
			}
			if s.queue.Len() != 2 {
				t.Fatalf("different instances shared a queue item: queued=%d", s.queue.Len())
			}
			for pass := 0; pass < 4 && s.queue.Len() > 0; pass++ {
				processQueuedApplication(t, s)
			}
			countRuns(t, s, 1)
			app, err := s.getApplication(context.Background(), "gitops", "demo")
			if err != nil {
				t.Fatal(err)
			}
			state, err := decodeIncident(app)
			if err != nil || state == nil || state.InstanceUID != "argocd-gitops-argocd-replacement" {
				t.Fatalf("new request lost its instance identity: state=%+v err=%v", state, err)
			}
		})
	}
}

func TestStaleQueueItemDoesNotClearReplacementGrace(t *testing.T) {
	s, dyn, now := incidentFixture(t)
	changeApp(t, dyn, func(app *unstructured.Unstructured) {
		app.SetCreationTimestamp(metav1.NewTime(*now))
	})
	revokeLifecycleInstance(t, s, "replaced and enrolled")
	sendDiagnosisNotification(t, s)
	s.enqueueApplication("gitops", "demo", "argocd-gitops-argocd")
	processQueuedApplication(t, s) // The new instance starts its grace period.
	processQueuedApplication(t, s) // The old instance's work is rejected.
	if _, ok := s.graceStarted["gitops/demo/argocd-gitops-argocd-replacement"]; !ok {
		t.Fatal("stale work cleared the replacement instance's grace clock")
	}
	countRuns(t, s, 0)
}
