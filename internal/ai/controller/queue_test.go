package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/redhat-developer/gitops-operator/internal/ai/utils"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"
	"k8s.io/client-go/util/workqueue"
)

func TestDiagnosisQueueCoalescesAndRequeuesDirtyKey(t *testing.T) {
	q := newDiagnosisQueue()
	t.Cleanup(q.ShutDown)

	for range 4 {
		q.Add("gitops/demo")
	}
	if got := q.Len(); got != 1 {
		t.Fatalf("queued duplicates = %d, want one", got)
	}

	key, shutdown := q.Get()
	if shutdown || key != "gitops/demo" {
		t.Fatalf("first key = %q, shutdown = %v", key, shutdown)
	}
	for range 3 {
		q.Add(key)
	}
	if got := q.Len(); got != 0 {
		t.Fatalf("key was available concurrently while processing: %d", got)
	}
	q.Done(key)
	if got := q.Len(); got != 1 {
		t.Fatalf("dirty key was queued %d times, want one", got)
	}
	key, shutdown = q.Get()
	if shutdown || key != "gitops/demo" {
		t.Fatalf("second key = %q, shutdown = %v", key, shutdown)
	}
	q.Done(key)
	if got := q.Len(); got != 0 {
		t.Fatalf("queue still contains %d items", got)
	}
}

func TestDiagnosisQueueHandlesApplicationsIndependently(t *testing.T) {
	s, dyn, _ := incidentFixture(t)
	app, err := s.getApplication(context.Background(), "gitops", "demo")
	if err != nil {
		t.Fatal(err)
	}
	other := app.DeepCopy()
	other.SetName("other")
	other.SetUID("other-uid")
	other.SetResourceVersion("1")
	other.SetAnnotations(nil)
	if err := dyn.Tracker().Create(applicationGVR, other, "gitops"); err != nil {
		t.Fatal(err)
	}

	s.enqueueApplication("gitops", "demo")
	s.enqueueApplication("gitops", "other")
	if got := s.queue.Len(); got != 2 {
		t.Fatalf("queued Applications = %d, want two", got)
	}
	processQueuedApplication(t, s)
	processQueuedApplication(t, s)
	processQueuedApplication(t, s)
	processQueuedApplication(t, s)
	countRuns(t, s, 2)
}

func TestDiagnosisQueueGraceExpiryCreatesRunWithoutAnotherEvent(t *testing.T) {
	s, _, now := incidentFixture(t)
	s.config.AutoDiagnosisGracePeriod = time.Hour + 80*time.Millisecond
	s.enqueueApplication("gitops", "demo")
	processQueuedApplication(t, s)
	countRuns(t, s, 0)
	if got := s.queue.NumRequeues("gitops/demo"); got != 0 {
		t.Fatalf("grace consumed %d error retries", got)
	}

	*now = now.Add(80 * time.Millisecond)
	waitForQueuedApplication(t, s) // AddAfter, with no second webhook.
	processQueuedApplication(t, s)
	processQueuedApplication(t, s)
	countRuns(t, s, 1)
}

func sendDiagnosisNotification(t *testing.T, s *TriggerService) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/diagnose", strings.NewReader(`{"application":"demo","applicationNamespace":"gitops"}`))
	request.Header.Set("Authorization", "Bearer token")
	response := httptest.NewRecorder()
	s.HandleDiagnose(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("notification status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestEarlyHealthNotificationSchedulesGraceWithoutSecondWebhook(t *testing.T) {
	for _, health := range []string{"Degraded", "Unknown", "Missing", "Progressing"} {
		t.Run(health, func(t *testing.T) {
			s, dyn, now := incidentFixture(t)
			grace := 80 * time.Millisecond
			s.config.AutoDiagnosisGracePeriod = grace
			changeApp(t, dyn, func(app *unstructured.Unstructured) {
				_ = unstructured.SetNestedField(app.Object, health, "status", "health", "status")
				_ = unstructured.SetNestedField(app.Object, now.Format(time.RFC3339), "status", "health", "lastTransitionTime")
				if health == "Degraded" || health == "Unknown" {
					app.SetCreationTimestamp(metav1.NewTime(*now))
				}
			})
			app, err := s.getApplication(context.Background(), "gitops", "demo")
			if err != nil {
				t.Fatal(err)
			}
			needed, wait := utils.Eligibility(app, *now, grace)
			if !needed || wait != grace {
				t.Fatalf("early %s eligibility: needed=%v wait=%s", health, needed, wait)
			}
			sendDiagnosisNotification(t, s)
			processQueuedApplication(t, s)
			countRuns(t, s, 0)
			*now = now.Add(grace)
			waitForQueuedApplication(t, s) // Delayed queue item; no second webhook.
			processQueuedApplication(t, s) // Record incident.
			processQueuedApplication(t, s) // Create run.
			countRuns(t, s, 1)
		})
	}
}

func TestGraceRecheckUsesLatestApplicationState(t *testing.T) {
	for _, change := range []string{"Healthy", "Suspended", "new revision", "recreated"} {
		t.Run(change, func(t *testing.T) {
			s, dyn, now := incidentFixture(t)
			grace := 80 * time.Millisecond
			s.config.AutoDiagnosisGracePeriod = grace
			changeApp(t, dyn, func(app *unstructured.Unstructured) {
				app.SetCreationTimestamp(metav1.NewTime(*now))
			})
			sendDiagnosisNotification(t, s)
			processQueuedApplication(t, s)
			countRuns(t, s, 0)
			if change == "recreated" {
				old, err := s.getApplication(context.Background(), "gitops", "demo")
				if err != nil {
					t.Fatal(err)
				}
				replacement := old.DeepCopy()
				replacement.SetUID("replacement-uid")
				replacement.SetResourceVersion("1")
				replacement.SetAnnotations(nil)
				if err := dyn.Tracker().Delete(applicationGVR, "gitops", "demo"); err != nil {
					t.Fatal(err)
				}
				if err := dyn.Tracker().Create(applicationGVR, replacement, "gitops"); err != nil {
					t.Fatal(err)
				}
			} else {
				changeApp(t, dyn, func(app *unstructured.Unstructured) {
					switch change {
					case "Healthy", "Suspended":
						_ = unstructured.SetNestedField(app.Object, change, "status", "health", "status")
					case "new revision":
						_ = unstructured.SetNestedField(app.Object, "rev2", "status", "sync", "revision")
					}
				})
			}
			*now = now.Add(grace)
			waitForQueuedApplication(t, s)
			processQueuedApplication(t, s)
			if change == "Healthy" || change == "Suspended" {
				countRuns(t, s, 0)
				return
			}
			processQueuedApplication(t, s)
			countRuns(t, s, 1)
			app, err := s.getApplication(context.Background(), "gitops", "demo")
			if err != nil {
				t.Fatal(err)
			}
			state, err := decodeIncident(app)
			if err != nil {
				t.Fatal(err)
			}
			run, err := s.getAgenticRun(context.Background(), state.RunNamespace, state.RunName)
			if err != nil {
				t.Fatal(err)
			}
			if change == "new revision" && run.GetAnnotations()[utils.AppRevision] != "rev2" {
				t.Fatal("delayed check diagnosed the old revision")
			}
			if change == "recreated" && run.GetLabels()[utils.AppUID] != "replacement-uid" {
				t.Fatal("delayed check used the previous Application UID")
			}
		})
	}
}

func TestRecoveryNotificationClosesExistingIncident(t *testing.T) {
	for _, health := range []string{"Healthy", "Suspended"} {
		t.Run(health, func(t *testing.T) {
			s, dyn, _ := incidentFixture(t)
			reconcileApp(t, s)
			changeApp(t, dyn, func(app *unstructured.Unstructured) {
				_ = unstructured.SetNestedField(app.Object, health, "status", "health", "status")
			})
			sendDiagnosisNotification(t, s)
			processQueuedApplication(t, s)
			countRuns(t, s, 1)
			app, err := s.getApplication(context.Background(), "gitops", "demo")
			if err != nil {
				t.Fatal(err)
			}
			state, err := decodeIncident(app)
			if err != nil || !state.Closed {
				t.Fatalf("recovery was not recorded: state=%+v err=%v", state, err)
			}
		})
	}
}

func TestDiagnosisQueueConflictsUseSeparateBackoffAttempts(t *testing.T) {
	s, dyn, _ := incidentFixture(t)
	patches := 0
	dyn.PrependReactor("patch", "applications", func(_ ktesting.Action) (bool, runtime.Object, error) {
		patches++
		if patches <= 3 {
			return true, nil, apierrors.NewConflict(applicationGVR.GroupResource(), "demo", fmt.Errorf("concurrent Application update"))
		}
		return false, nil, nil
	})
	s.enqueueApplication("gitops", "demo")
	started := time.Now()
	for attempt := 1; attempt <= 3; attempt++ {
		if attempt > 1 {
			waitForQueuedApplication(t, s)
		}
		processQueuedApplication(t, s)
		if patches != attempt || s.queue.NumRequeues("gitops/demo") != attempt {
			t.Fatalf("attempt %d made %d patches and %d rate-limited retries", attempt, patches, s.queue.NumRequeues("gitops/demo"))
		}
		countRuns(t, s, 0)
	}
	waitForQueuedApplication(t, s)
	if elapsed := time.Since(started); elapsed < 20*time.Millisecond {
		t.Fatalf("three conflicts retried without queue backoff: %s", elapsed)
	}
	processQueuedApplication(t, s) // Persist incident after the API stops conflicting.
	if got := s.queue.NumRequeues("gitops/demo"); got != 0 {
		t.Fatalf("successful persistence retained %d infrastructure retries", got)
	}
	processQueuedApplication(t, s) // Create the recorded attempt.
	countRuns(t, s, 1)
	app, err := s.getApplication(context.Background(), "gitops", "demo")
	if err != nil {
		t.Fatal(err)
	}
	state, err := decodeIncident(app)
	if err != nil || state == nil || state.Attempt != 1 || !state.RunCreated {
		t.Fatalf("conflicts changed diagnosis attempt: state=%+v err=%v", state, err)
	}
}

func TestDiagnosisQueueTerminalIncidentErrorsAreForgotten(t *testing.T) {
	for name, value := range map[string]string{
		"invalid JSON":     `{broken`,
		"invalid identity": `{"id":"","revision":"rev1","attempt":1,"runCreated":false,"runName":"run","runNamespace":"lightspeed"}`,
	} {
		t.Run(name, func(t *testing.T) {
			s, dyn, _ := incidentFixture(t)
			changeApp(t, dyn, func(app *unstructured.Unstructured) {
				app.SetAnnotations(map[string]string{utils.IncidentAnnotation: value})
			})
			s.enqueueApplication("gitops", "demo")
			processQueuedApplication(t, s)
			if s.queue.NumRequeues("gitops/demo") != 0 || s.queue.Len() != 0 {
				t.Fatal("invalid incident was queued for an endless retry")
			}
			countRuns(t, s, 0)
		})
	}
}

func TestDiagnosisQueueConflictingRunIsTerminal(t *testing.T) {
	s, dyn, _ := incidentFixture(t)
	app, err := s.getApplication(context.Background(), "gitops", "demo")
	if err != nil {
		t.Fatal(err)
	}
	state := &incidentState{ID: "incident", Revision: "rev1", Attempt: 1}
	s.prepareRunAttempt(app, state)
	if err := s.saveIncident(context.Background(), app, state); err != nil {
		t.Fatal(err)
	}
	conflict := s.buildIncidentRun(app, state)
	annotations := conflict.GetAnnotations()
	annotations[incidentIDAnnotation] = "different-incident"
	conflict.SetAnnotations(annotations)
	if err := dyn.Tracker().Create(agenticRunGVR, conflict, "lightspeed"); err != nil {
		t.Fatal(err)
	}
	s.enqueueApplication("gitops", "demo")
	processQueuedApplication(t, s)
	if s.queue.NumRequeues("gitops/demo") != 0 || s.queue.Len() != 0 {
		t.Fatal("conflicting deterministic run was queued for another retry")
	}
	app, err = s.getApplication(context.Background(), "gitops", "demo")
	if err != nil {
		t.Fatal(err)
	}
	state, err = decodeIncident(app)
	if err != nil || state == nil || state.RunCreated {
		t.Fatalf("conflicting run was confirmed: state=%+v err=%v", state, err)
	}
}

func TestDiagnosisQueueRetriesTemporaryCreateFailure(t *testing.T) {
	s, dyn, _ := incidentFixture(t)
	creates := 0
	dyn.PrependReactor("create", "agenticruns", func(_ ktesting.Action) (bool, runtime.Object, error) {
		creates++
		if creates == 1 {
			return true, nil, apierrors.NewTimeoutError("Create response lost before persistence", 1)
		}
		return false, nil, nil
	})
	s.enqueueApplication("gitops", "demo")
	processQueuedApplication(t, s) // Record incident identity.
	processQueuedApplication(t, s) // Create times out; worker rate-limits.
	if creates != 1 || s.queue.NumRequeues("gitops/demo") != 1 {
		t.Fatalf("Create timeout did not stop and rate-limit: creates=%d retries=%d", creates, s.queue.NumRequeues("gitops/demo"))
	}
	countRuns(t, s, 0)
	waitForQueuedApplication(t, s)
	processQueuedApplication(t, s)
	countRuns(t, s, 1)
	if creates != 2 || s.queue.NumRequeues("gitops/demo") != 0 {
		t.Fatalf("Create retry did not settle: creates=%d retries=%d", creates, s.queue.NumRequeues("gitops/demo"))
	}
}

func TestDiagnosisQueueDropsAfterMaximumInfrastructureRetries(t *testing.T) {
	s, dyn, _ := incidentFixture(t)
	s.queue.ShutDown()
	s.queue = workqueue.NewTypedRateLimitingQueue(
		workqueue.NewTypedItemExponentialFailureRateLimiter[string](time.Nanosecond, time.Nanosecond),
	)
	t.Cleanup(s.queue.ShutDown)
	patches := 0
	dyn.PrependReactor("patch", "applications", func(_ ktesting.Action) (bool, runtime.Object, error) {
		patches++
		return true, nil, apierrors.NewConflict(applicationGVR.GroupResource(), "demo", fmt.Errorf("persistent conflict"))
	})
	s.enqueueApplication("gitops", "demo")
	for attempt := 0; attempt <= maxDiagnosisQueueRetries; attempt++ {
		if attempt > 0 {
			waitForQueuedApplication(t, s)
		}
		processQueuedApplication(t, s)
	}
	if patches != maxDiagnosisQueueRetries+1 || s.queue.NumRequeues("gitops/demo") != 0 || s.queue.Len() != 0 {
		t.Fatalf("retry cap was not enforced: patches=%d retries=%d queued=%d", patches, s.queue.NumRequeues("gitops/demo"), s.queue.Len())
	}
	countRuns(t, s, 0)
}

func TestDiagnosisQueueShutdownDiscardsPendingRetry(t *testing.T) {
	s, dyn, _ := incidentFixture(t)
	patches := 0
	dyn.PrependReactor("patch", "applications", func(_ ktesting.Action) (bool, runtime.Object, error) {
		patches++
		return true, nil, apierrors.NewConflict(applicationGVR.GroupResource(), "demo", fmt.Errorf("temporary conflict"))
	})
	s.enqueueApplication("gitops", "demo")
	processQueuedApplication(t, s)
	s.queue.ShutDown()
	if s.processNextWorkItem(context.Background()) || patches != 1 {
		t.Fatalf("shutdown processed a pending retry: patches=%d", patches)
	}
}

func TestDiagnosisQueueStaleGraceWakeupReadsRecoveredApplication(t *testing.T) {
	s, dyn, now := incidentFixture(t)
	s.config.AutoDiagnosisGracePeriod = time.Hour + 80*time.Millisecond
	s.enqueueApplication("gitops", "demo")
	processQueuedApplication(t, s)
	changeApp(t, dyn, func(app *unstructured.Unstructured) {
		_ = unstructured.SetNestedField(app.Object, "Healthy", "status", "health", "status")
	})

	*now = now.Add(80 * time.Millisecond)
	waitForQueuedApplication(t, s)
	processQueuedApplication(t, s)
	countRuns(t, s, 0)
	app, err := s.getApplication(context.Background(), "gitops", "demo")
	if err != nil {
		t.Fatal(err)
	}
	if app.GetAnnotations()[utils.IncidentAnnotation] != "" {
		t.Fatal("stale grace wakeup created an incident after recovery")
	}
}

func TestDiagnosisQueueRateLimitsErrorsAndForgetsAfterSuccess(t *testing.T) {
	s, dyn, _ := incidentFixture(t)
	fail := true
	lookups := 0
	dyn.PrependReactor("get", "applications", func(_ ktesting.Action) (bool, runtime.Object, error) {
		lookups++
		if fail {
			return true, nil, fmt.Errorf("temporary Application API failure")
		}
		return false, nil, nil
	})

	s.enqueueApplication("gitops", "demo")
	processQueuedApplication(t, s)
	if lookups != 1 || s.queue.NumRequeues("gitops/demo") != 1 {
		t.Fatalf("error was not rate limited: lookups=%d requeues=%d", lookups, s.queue.NumRequeues("gitops/demo"))
	}
	fail = false
	waitForQueuedApplication(t, s)
	processQueuedApplication(t, s)
	if got := s.queue.NumRequeues("gitops/demo"); got != 0 {
		t.Fatalf("successful reconciliation retained %d retries", got)
	}
	processQueuedApplication(t, s)
	countRuns(t, s, 1)
}

func TestAgenticRunDeleteEventEnqueuesApplication(t *testing.T) {
	s, _, _ := incidentFixture(t)
	run := reconcileApp(t, s)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); s.runAgenticRunEvents(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("AgenticRun informer did not stop")
		}
	})

	waitForQueuedApplication(t, s) // Startup Add.
	processQueuedApplication(t, s)
	if err := s.dynClient.Resource(agenticRunGVR).Namespace("lightspeed").Delete(context.Background(), run.GetName(), metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	waitForQueuedApplication(t, s) // Watch Delete.
	processQueuedApplication(t, s)
	countRuns(t, s, 0)

	app, err := s.getApplication(context.Background(), "gitops", "demo")
	if err != nil {
		t.Fatal(err)
	}
	state, err := decodeIncident(app)
	if err != nil || state == nil || state.RunName != run.GetName() || !state.RunCreated {
		t.Fatalf("deleted run changed durable incident: state=%+v err=%v", state, err)
	}
}
