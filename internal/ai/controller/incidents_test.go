package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/redhat-developer/gitops-operator/internal/ai/utils"
	authv1 "k8s.io/api/authentication/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func incidentFixture(t *testing.T) (*TriggerService, *dynamicfake.FakeDynamicClient, *time.Time) {
	t.Helper()
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	app := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "argoproj.io/v1alpha1", "kind": "Application",
		"metadata": map[string]any{"name": "demo", "namespace": "gitops", "uid": "app-uid", "resourceVersion": "1", "creationTimestamp": now.Add(-time.Hour).Format(time.RFC3339)},
		"spec":     map[string]any{"destination": map[string]any{"namespace": "workload"}},
		"status":   map[string]any{"sync": map[string]any{"status": "Synced", "revision": "rev1"}, "health": map[string]any{"status": "Degraded", "lastTransitionTime": now.Add(-time.Hour).Format(time.RFC3339)}},
	}}
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{applicationGVR: "ApplicationList", agenticRunGVR: "AgenticRunList"}, app)
	// The dynamic fake does not implement resourceVersion preconditions. Model
	// apiserver compare-and-swap so concurrent tests actually exercise conflicts.
	var mu sync.Mutex
	dyn.PrependReactor("patch", "applications", func(action ktesting.Action) (bool, runtime.Object, error) {
		mu.Lock()
		defer mu.Unlock()
		patch := action.(ktesting.PatchAction)
		obj, err := dyn.Tracker().Get(applicationGVR, patch.GetNamespace(), patch.GetName())
		if err != nil {
			return true, nil, err
		}
		current := obj.(*unstructured.Unstructured).DeepCopy()
		var body struct {
			Metadata struct {
				ResourceVersion string             `json:"resourceVersion"`
				UID             string             `json:"uid"`
				Annotations     map[string]*string `json:"annotations"`
			} `json:"metadata"`
		}
		if err := json.Unmarshal(patch.GetPatch(), &body); err != nil {
			return true, nil, err
		}
		if body.Metadata.ResourceVersion != current.GetResourceVersion() || body.Metadata.UID != string(current.GetUID()) {
			return true, nil, apierrors.NewConflict(applicationGVR.GroupResource(), current.GetName(), fmt.Errorf("changed"))
		}
		annotations := current.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}
		for k, v := range body.Metadata.Annotations {
			if v == nil {
				delete(annotations, k)
			} else {
				annotations[k] = *v
			}
		}
		current.SetAnnotations(annotations)
		n, _ := strconv.Atoi(current.GetResourceVersion())
		current.SetResourceVersion(strconv.Itoa(n + 1))
		err = dyn.Tracker().Update(applicationGVR, current, current.GetNamespace())
		return true, current, err
	})
	kube := kubefake.NewClientset()
	kube.PrependReactor("create", "tokenreviews", func(action ktesting.Action) (bool, runtime.Object, error) {
		return true, &authv1.TokenReview{Status: authv1.TokenReviewStatus{Authenticated: true, User: authv1.UserInfo{Username: "system:serviceaccount:gitops:" + notificationsSAName}}}, nil
	})
	s := &TriggerService{dynClient: dyn, kubeClient: kube, logger: logr.Discard(), config: ServiceConfig{RunNamespace: "lightspeed", ArgocdNamespace: "gitops", Cooldown: 10 * time.Minute, AutoDiagnosisGracePeriod: 10 * time.Minute}, now: func() time.Time { return now }, queue: newDiagnosisQueue()}
	t.Cleanup(s.queue.ShutDown)
	return s, dyn, &now
}

func changeApp(t *testing.T, dyn *dynamicfake.FakeDynamicClient, mutate func(*unstructured.Unstructured)) {
	t.Helper()
	obj, err := dyn.Tracker().Get(applicationGVR, "gitops", "demo")
	if err != nil {
		t.Fatal(err)
	}
	app := obj.(*unstructured.Unstructured).DeepCopy()
	mutate(app)
	n, _ := strconv.Atoi(app.GetResourceVersion())
	app.SetResourceVersion(strconv.Itoa(n + 1))
	if err := dyn.Tracker().Update(applicationGVR, app, "gitops"); err != nil {
		t.Fatal(err)
	}
}

func reconcileApp(t *testing.T, s *TriggerService) *unstructured.Unstructured {
	t.Helper()
	run, _, _, err := reconcileUntilSettled(s)
	if err != nil {
		t.Fatal(err)
	}
	return run
}

// Drive expected persisted-state transitions in tests. Production performs
// each step through a separate queue item instead of looping in reconciliation.
func reconcileUntilSettled(s *TriggerService) (*unstructured.Unstructured, string, time.Duration, error) {
	for range 12 {
		run, reason, wait, requeue, err := s.reconcileDiagnosis(context.Background(), "gitops", "demo")
		if err != nil || !requeue {
			return run, reason, wait, err
		}
	}
	return nil, "", 0, fmt.Errorf("diagnosis did not settle in test")
}

func countRuns(t *testing.T, s *TriggerService, expected int) {
	t.Helper()
	list, err := s.dynClient.Resource(agenticRunGVR).Namespace("lightspeed").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != expected {
		t.Fatalf("runs=%d, want %d", len(list.Items), expected)
	}
}

func finishRun(t *testing.T, dyn *dynamicfake.FakeDynamicClient, run *unstructured.Unstructured, success bool, now time.Time) {
	t.Helper()
	obj, err := dyn.Tracker().Get(agenticRunGVR, "lightspeed", run.GetName())
	if err != nil {
		t.Fatal(err)
	}
	run = obj.(*unstructured.Unstructured).DeepCopy()
	status := "False"
	if success {
		status = "True"
	}
	run.Object["status"] = map[string]any{"conditions": []any{map[string]any{"type": "Analyzed", "status": status, "reason": "AnalysisOutcome", "lastTransitionTime": now.Format(time.RFC3339)}}}
	if err := dyn.Tracker().Update(agenticRunGVR, run, "lightspeed"); err != nil {
		t.Fatal(err)
	}
}

func TestIncidentBoundedRetrySurvivesRestart(t *testing.T) {
	s, dyn, now := incidentFixture(t)
	first := reconcileApp(t, s)
	if first == nil {
		t.Fatal("no initial run")
	}
	finishRun(t, dyn, first, false, *now)
	s.enqueueRunOwner(context.Background(), mustGetRun(t, s, first.GetName()))
	processQueuedApplication(t, s)
	countRuns(t, s, 1)
	*now = now.Add(10*time.Minute - time.Second)
	reconcileApp(t, s)
	countRuns(t, s, 1)
	*now = now.Add(time.Second)
	restarted := &TriggerService{dynClient: dyn, config: s.config, logger: logr.Discard(), now: s.now, queue: newDiagnosisQueue()}
	t.Cleanup(restarted.queue.ShutDown)
	restarted.enqueueRunOwner(context.Background(), mustGetRun(t, restarted, first.GetName()))
	processQueuedApplication(t, restarted)
	processQueuedApplication(t, restarted) // Persisted attempt 2 is created next.
	countRuns(t, s, 2)
	app, _ := s.getApplication(context.Background(), "gitops", "demo")
	state, _ := decodeIncident(app)
	second, _ := s.getAgenticRun(context.Background(), "lightspeed", state.RunName)
	finishRun(t, dyn, second, false, *now)
	*now = now.Add(time.Hour)
	reconcileApp(t, restarted)
	countRuns(t, s, 2)
	state, _ = decodeIncident(app)
	if state.Attempt != 2 {
		t.Fatalf("attempt=%d", state.Attempt)
	}
}

func TestIncidentSuccessFlapsRecoveryAndNewRevision(t *testing.T) {
	s, dyn, now := incidentFixture(t)
	first := reconcileApp(t, s)
	finishRun(t, dyn, first, true, *now)
	*now = now.Add(time.Hour)
	reconcileApp(t, s)
	countRuns(t, s, 1)
	changeApp(t, dyn, func(app *unstructured.Unstructured) {
		_ = unstructured.SetNestedField(app.Object, "Unknown", "status", "health", "status")
	})
	reconcileApp(t, s)
	countRuns(t, s, 1)
	changeApp(t, dyn, func(app *unstructured.Unstructured) {
		_ = unstructured.SetNestedField(app.Object, "Healthy", "status", "health", "status")
	})
	reconcileApp(t, s)
	appAfterRecovery, err := s.getApplication(context.Background(), "gitops", "demo")
	if err != nil {
		t.Fatal(err)
	}
	closed, err := decodeIncident(appAfterRecovery)
	if err != nil || !closed.Closed || !closed.RunCreated {
		t.Fatalf("confirmed incident was not closed after recovery: state=%+v err=%v", closed, err)
	}
	annots := appAfterRecovery.GetAnnotations()
	if annots[utils.DiagnosisState] != "" || annots[utils.AgenticRunName] != "" || annots[utils.AgenticRunNamespace] != "" {
		t.Fatalf("visual UI annotations not cleared after recovery: %v", annots)
	}
	changeApp(t, dyn, func(app *unstructured.Unstructured) {
		_ = unstructured.SetNestedField(app.Object, "Degraded", "status", "health", "status")
	})
	second := reconcileApp(t, s)
	countRuns(t, s, 2)
	changeApp(t, dyn, func(app *unstructured.Unstructured) {
		_ = unstructured.SetNestedField(app.Object, "rev2", "status", "sync", "revision")
	})
	reconcileApp(t, s)
	countRuns(t, s, 2)
	app, _ := s.getApplication(context.Background(), "gitops", "demo")
	state, _ := decodeIncident(app)
	if state.Revision != "rev1" {
		t.Fatal("active run's incident was replaced before it finished")
	}
	if strings.Contains(app.GetAnnotations()[utils.IncidentAnnotation], "\"pending\"") {
		t.Fatal("waiting revision persisted an unnecessary Pending flag")
	}
	finishRun(t, dyn, second, true, *now)
	s.enqueueRunOwner(context.Background(), mustGetRun(t, s, second.GetName()))
	processQueuedApplication(t, s)
	processQueuedApplication(t, s) // Create the newly recorded revision.
	countRuns(t, s, 3)
}

func TestConcurrentIncidentCreation(t *testing.T) {
	s, _, _ := incidentFixture(t)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { _, _, _, _, _ = s.reconcileDiagnosis(context.Background(), "gitops", "demo") })
	}
	wg.Wait()
	reconcileApp(t, s)
	countRuns(t, s, 1)
}

func TestAmbiguousCreateResumesSameIncident(t *testing.T) {
	s, dyn, _ := incidentFixture(t)
	calls := 0
	dyn.PrependReactor("create", "agenticruns", func(action ktesting.Action) (bool, runtime.Object, error) {
		calls++
		if calls > 1 {
			return false, nil, nil
		}
		obj := action.(ktesting.CreateAction).GetObject()
		if err := dyn.Tracker().Create(agenticRunGVR, obj, "lightspeed"); err != nil {
			t.Fatal(err)
		}
		return true, nil, apierrors.NewTimeoutError("response lost", 1)
	})
	_, _, _, err := reconcileUntilSettled(s)
	if err == nil {
		t.Fatal("expected timeout")
	}
	// The run already exists. Changes to the Application or service configuration
	// must not invalidate its durable incident identity.
	changeApp(t, dyn, func(app *unstructured.Unstructured) {
		_ = unstructured.SetNestedField(app.Object, "OutOfSync", "status", "sync", "status")
		_ = unstructured.SetNestedField(app.Object, "different-target", "spec", "destination", "namespace")
	})
	reconcileApp(t, s)
	countRuns(t, s, 1)
	app, err := s.getApplication(context.Background(), "gitops", "demo")
	if err != nil {
		t.Fatal(err)
	}
	state, err := decodeIncident(app)
	if err != nil || !state.RunCreated {
		t.Fatalf("ambiguous creation was not confirmed: state=%+v err=%v", state, err)
	}
}

func TestIncidentRecoveryAfterAmbiguousSuccessfulCreateConfirmsRun(t *testing.T) {
	s, dyn, _ := incidentFixture(t)
	dyn.PrependReactor("create", "agenticruns", func(action ktesting.Action) (bool, runtime.Object, error) {
		obj := action.(ktesting.CreateAction).GetObject()
		if err := dyn.Tracker().Create(agenticRunGVR, obj, "lightspeed"); err != nil {
			t.Fatal(err)
		}
		return true, nil, apierrors.NewTimeoutError("response lost", 1)
	})
	if _, _, _, err := reconcileUntilSettled(s); err == nil {
		t.Fatal("expected ambiguous Create result")
	}
	changeApp(t, dyn, func(app *unstructured.Unstructured) {
		_ = unstructured.SetNestedField(app.Object, "Healthy", "status", "health", "status")
	})
	reconcileApp(t, s)
	countRuns(t, s, 1)
	app, err := s.getApplication(context.Background(), "gitops", "demo")
	if err != nil {
		t.Fatal(err)
	}
	state, err := decodeIncident(app)
	if err != nil || !state.RunCreated || !state.Closed {
		t.Fatalf("existing run was not confirmed and closed: state=%+v err=%v", state, err)
	}
}

func TestIncidentCrashBeforeCreateUsesLatestContext(t *testing.T) {
	s, dyn, _ := incidentFixture(t)
	var planned *unstructured.Unstructured
	calls := 0
	dyn.PrependReactor("create", "agenticruns", func(action ktesting.Action) (bool, runtime.Object, error) {
		calls++
		if calls == 1 {
			planned = action.(ktesting.CreateAction).GetObject().(*unstructured.Unstructured).DeepCopy()
			return true, nil, apierrors.NewTimeoutError("create was not accepted", 1)
		}
		return false, nil, nil
	})
	if _, _, _, err := reconcileUntilSettled(s); err == nil {
		t.Fatal("expected first Create to fail")
	}
	countRuns(t, s, 0)
	app, err := s.getApplication(context.Background(), "gitops", "demo")
	if err != nil {
		t.Fatal(err)
	}
	annotation := app.GetAnnotations()[utils.IncidentAnnotation]
	state, err := decodeIncident(app)
	if err != nil || state.RunCreated || strings.Contains(annotation, "\"snapshot\"") {
		t.Fatalf("invalid unconfirmed incident: state=%+v err=%v", state, err)
	}
	attemptName := state.RunName
	changeApp(t, dyn, func(app *unstructured.Unstructured) {
		_ = unstructured.SetNestedField(app.Object, "OutOfSync", "status", "sync", "status")
		_ = unstructured.SetNestedSlice(app.Object, []any{map[string]any{"type": "NewError", "message": "later condition"}}, "status", "conditions")
	})
	changedConfig := &TriggerService{dynClient: dyn, config: s.config, logger: logr.Discard(), now: s.now}
	run, _, _, _, err := changedConfig.reconcileDiagnosis(context.Background(), "gitops", "demo")
	if err != nil || run == nil {
		t.Fatalf("restart did not create intended run: run=%v err=%v", run, err)
	}
	request, _, _ := unstructured.NestedString(run.Object, "spec", "request")
	previousRequest, _, _ := unstructured.NestedString(planned.Object, "spec", "request")
	if run.GetName() != attemptName || request == previousRequest ||
		!strings.Contains(request, "later condition") ||
		run.GetAnnotations()[utils.AppSyncStatus] != "OutOfSync" {
		t.Fatal("restart did not use the same name with latest Application/configuration")
	}
	app, err = s.getApplication(context.Background(), "gitops", "demo")
	if err != nil {
		t.Fatal(err)
	}
	state, err = decodeIncident(app)
	if err != nil || !state.RunCreated || strings.Contains(app.GetAnnotations()[utils.IncidentAnnotation], "\"snapshot\"") {
		t.Fatalf("created run persisted an invalid incident: state=%+v err=%v", state, err)
	}
	reconcileApp(t, s)
	countRuns(t, s, 1)
}

func TestIncidentRecoveryBeforeCreateSkipsDiagnosis(t *testing.T) {
	s, dyn, _ := incidentFixture(t)
	calls := 0
	dyn.PrependReactor("create", "agenticruns", func(_ ktesting.Action) (bool, runtime.Object, error) {
		calls++
		if calls == 1 {
			return true, nil, apierrors.NewTimeoutError("create was not accepted", 1)
		}
		return false, nil, nil
	})
	if _, _, _, err := reconcileUntilSettled(s); err == nil {
		t.Fatal("expected first Create to fail")
	}
	app, err := s.getApplication(context.Background(), "gitops", "demo")
	if err != nil {
		t.Fatal(err)
	}
	unfinished, err := decodeIncident(app)
	if err != nil || unfinished.RunCreated {
		t.Fatalf("no unfinished incident: state=%+v err=%v", unfinished, err)
	}
	changeApp(t, dyn, func(app *unstructured.Unstructured) {
		_ = unstructured.SetNestedField(app.Object, "Healthy", "status", "health", "status")
	})
	if run := reconcileApp(t, s); run != nil {
		t.Fatal("created a run after Application recovery")
	}
	countRuns(t, s, 0)
	app, err = s.getApplication(context.Background(), "gitops", "demo")
	if err != nil {
		t.Fatal(err)
	}
	closed, err := decodeIncident(app)
	if err != nil || !closed.Closed || closed.RunCreated {
		t.Fatalf("unfinished incident was not closed: state=%+v err=%v", closed, err)
	}
	changeApp(t, dyn, func(app *unstructured.Unstructured) {
		_ = unstructured.SetNestedField(app.Object, "Degraded", "status", "health", "status")
	})
	run := reconcileApp(t, s)
	if run == nil || run.GetName() == unfinished.RunName {
		t.Fatal("same-revision degradation reused the abandoned incident")
	}
	countRuns(t, s, 1)
}

func TestIncidentRevisionChangeBeforeCreateStartsNewIncident(t *testing.T) {
	s, dyn, _ := incidentFixture(t)
	calls := 0
	dyn.PrependReactor("create", "agenticruns", func(_ ktesting.Action) (bool, runtime.Object, error) {
		calls++
		if calls == 1 {
			return true, nil, apierrors.NewTimeoutError("create was not accepted", 1)
		}
		return false, nil, nil
	})
	if _, _, _, err := reconcileUntilSettled(s); err == nil {
		t.Fatal("expected first Create to fail")
	}
	app, err := s.getApplication(context.Background(), "gitops", "demo")
	if err != nil {
		t.Fatal(err)
	}
	unfinished, err := decodeIncident(app)
	if err != nil {
		t.Fatal(err)
	}
	changeApp(t, dyn, func(app *unstructured.Unstructured) {
		_ = unstructured.SetNestedField(app.Object, "rev2", "status", "sync", "revision")
	})
	run := reconcileApp(t, s)
	if run == nil || run.GetName() == unfinished.RunName || run.GetAnnotations()[utils.AppRevision] != "rev2" {
		t.Fatal("new revision used the old unfinished incident")
	}
	app, err = s.getApplication(context.Background(), "gitops", "demo")
	if err != nil {
		t.Fatal(err)
	}
	current, err := decodeIncident(app)
	if err != nil || !current.RunCreated || current.Revision != "rev2" || current.ID == unfinished.ID {
		t.Fatalf("new revision was not persisted as a new incident: state=%+v err=%v", current, err)
	}
	countRuns(t, s, 1)
}

func TestIncidentRejectsConflictingDeterministicRun(t *testing.T) {
	s, dyn, _ := incidentFixture(t)
	dyn.PrependReactor("create", "agenticruns", func(action ktesting.Action) (bool, runtime.Object, error) {
		conflict := action.(ktesting.CreateAction).GetObject().(*unstructured.Unstructured).DeepCopy()
		annotations := conflict.GetAnnotations()
		annotations[incidentIDAnnotation] = "another-incident"
		conflict.SetAnnotations(annotations)
		if err := dyn.Tracker().Create(agenticRunGVR, conflict, "lightspeed"); err != nil {
			t.Fatal(err)
		}
		return true, nil, apierrors.NewAlreadyExists(agenticRunGVR.GroupResource(), conflict.GetName())
	})
	if _, _, _, err := reconcileUntilSettled(s); err == nil || !strings.Contains(err.Error(), "conflicts with incident identity") {
		t.Fatalf("conflicting run was accepted: %v", err)
	}
	app, err := s.getApplication(context.Background(), "gitops", "demo")
	if err != nil {
		t.Fatal(err)
	}
	state, err := decodeIncident(app)
	if err != nil || state.RunCreated {
		t.Fatalf("conflicting run was marked created: state=%+v err=%v", state, err)
	}
}

func TestRunBelongsToIncidentChecksCorrelation(t *testing.T) {
	s, _, _ := incidentFixture(t)
	run := reconcileApp(t, s)
	app, err := s.getApplication(context.Background(), "gitops", "demo")
	if err != nil {
		t.Fatal(err)
	}
	state, err := decodeIncident(app)
	if err != nil || !runBelongsToIncident(run, app, state) {
		t.Fatalf("created run did not match its incident: %v", err)
	}
	setLabel := func(run *unstructured.Unstructured, key, value string) {
		labels := run.GetLabels()
		labels[key] = value
		run.SetLabels(labels)
	}
	setAnnotation := func(run *unstructured.Unstructured, key, value string) {
		annotations := run.GetAnnotations()
		annotations[key] = value
		run.SetAnnotations(annotations)
	}
	checks := map[string]func(*unstructured.Unstructured){
		"name":                  func(run *unstructured.Unstructured) { run.SetName("other") },
		"namespace":             func(run *unstructured.Unstructured) { run.SetNamespace("other") },
		"managed-by":            func(run *unstructured.Unstructured) { setLabel(run, utils.ManagedBy, "other") },
		"application name":      func(run *unstructured.Unstructured) { setAnnotation(run, utils.AppName, "other") },
		"application namespace": func(run *unstructured.Unstructured) { setLabel(run, utils.AppNamespace, "other") },
		"application UID":       func(run *unstructured.Unstructured) { setLabel(run, utils.AppUID, "other") },
		"incident ID":           func(run *unstructured.Unstructured) { setAnnotation(run, incidentIDAnnotation, "other") },
		"attempt": func(run *unstructured.Unstructured) {
			setAnnotation(run, utils.AppAttempt, "2")
		},
		"revision": func(run *unstructured.Unstructured) {
			setAnnotation(run, utils.AppRevision, "other")
		},
	}
	for name, mutate := range checks {
		t.Run(name, func(t *testing.T) {
			changed := run.DeepCopy()
			mutate(changed)
			if runBelongsToIncident(changed, app, state) {
				t.Fatal("accepted conflicting run")
			}
		})
	}
}

func TestConfirmExistingRun(t *testing.T) {
	setLabel := func(run *unstructured.Unstructured, key, value string) {
		labels := run.GetLabels()
		labels[key] = value
		run.SetLabels(labels)
	}
	setAnnotation := func(run *unstructured.Unstructured, key, value string) {
		annotations := run.GetAnnotations()
		annotations[key] = value
		run.SetAnnotations(annotations)
	}
	tests := []struct {
		name           string
		mutateState    func(*incidentState)
		mutateRun      func(*unstructured.Unstructured)
		storeRun       bool
		previous       bool
		getError       bool
		patchConflict  bool
		wantConfirmed  bool
		wantTerminal   bool
		wantRetryError bool
	}{
		{name: "run absent"},
		{name: "matching run found by exact GET", storeRun: true, wantConfirmed: true},
		{name: "matching run already listed", previous: true, wantConfirmed: true},
		{name: "invalid deterministic name", mutateState: func(state *incidentState) { state.RunName = "wrong" }, wantTerminal: true},
		{name: "wrong run namespace", mutateState: func(state *incidentState) { state.RunNamespace = "other" }, wantTerminal: true},
		{name: "wrong Application UID", previous: true, mutateRun: func(run *unstructured.Unstructured) { setLabel(run, utils.AppUID, "other") }, wantTerminal: true},
		{name: "wrong incident ID", previous: true, mutateRun: func(run *unstructured.Unstructured) { setAnnotation(run, incidentIDAnnotation, "other") }, wantTerminal: true},
		{name: "wrong attempt", previous: true, mutateRun: func(run *unstructured.Unstructured) { setAnnotation(run, utils.AppAttempt, "2") }, wantTerminal: true},
		{name: "wrong revision", previous: true, mutateRun: func(run *unstructured.Unstructured) {
			setAnnotation(run, utils.AppRevision, "other")
		}, wantTerminal: true},
		{name: "wrong managed-by", previous: true, mutateRun: func(run *unstructured.Unstructured) { setLabel(run, utils.ManagedBy, "other") }, wantTerminal: true},
		{name: "temporary GET error", getError: true, wantRetryError: true},
		{name: "save conflict", previous: true, patchConflict: true, wantRetryError: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, dyn, _ := incidentFixture(t)
			ctx := context.Background()
			app, err := s.getApplication(ctx, "gitops", "demo")
			if err != nil {
				t.Fatal(err)
			}
			state := &incidentState{ID: "incident", Revision: "rev1", Attempt: 1}
			s.prepareRunAttempt(app, state)
			if err := s.saveIncident(ctx, app, state); err != nil {
				t.Fatal(err)
			}
			app, err = s.getApplication(ctx, "gitops", "demo")
			if err != nil {
				t.Fatal(err)
			}
			run := s.buildIncidentRun(app, state)
			if tc.mutateState != nil {
				tc.mutateState(state)
			}
			if tc.mutateRun != nil {
				tc.mutateRun(run)
			}
			if tc.storeRun {
				if err := dyn.Tracker().Create(agenticRunGVR, run, run.GetNamespace()); err != nil {
					t.Fatal(err)
				}
			}
			if tc.getError {
				dyn.PrependReactor("get", "agenticruns", func(_ ktesting.Action) (bool, runtime.Object, error) {
					return true, nil, fmt.Errorf("temporary GET failure")
				})
			}
			if tc.patchConflict {
				dyn.PrependReactor("patch", "applications", func(_ ktesting.Action) (bool, runtime.Object, error) {
					return true, nil, apierrors.NewConflict(applicationGVR.GroupResource(), "demo", fmt.Errorf("changed"))
				})
			}
			var previous *unstructured.Unstructured
			if tc.previous {
				previous = run
			}
			found, confirmed, err := s.confirmExistingRun(ctx, app, state, previous)
			var terminal *terminalDiagnosisError
			if confirmed != tc.wantConfirmed || errors.As(err, &terminal) != tc.wantTerminal ||
				(err != nil && !tc.wantTerminal && !tc.wantRetryError) ||
				(err == nil && (tc.wantTerminal || tc.wantRetryError)) {
				t.Fatalf("found=%v confirmed=%v err=%v", found, confirmed, err)
			}
			if tc.wantConfirmed && (found == nil || found.GetName() != state.RunName || !state.RunCreated) {
				t.Fatalf("matching run not confirmed: found=%v state=%+v", found, state)
			}
			persistedApp, err := s.getApplication(ctx, "gitops", "demo")
			if err != nil {
				t.Fatal(err)
			}
			persisted, err := decodeIncident(persistedApp)
			if err != nil || persisted.RunCreated != tc.wantConfirmed {
				t.Fatalf("persisted state=%+v err=%v", persisted, err)
			}
		})
	}
}

func TestCreateSaveConflictRecoversRunFromInitialList(t *testing.T) {
	s, dyn, _ := incidentFixture(t)
	ctx := context.Background()
	if _, reason, _, requeue, err := s.reconcileDiagnosis(ctx, "gitops", "demo"); err != nil || reason != "incident recorded" || !requeue {
		t.Fatalf("incident was not recorded: reason=%q requeue=%v err=%v", reason, requeue, err)
	}
	creates := 0
	dyn.PrependReactor("create", "agenticruns", func(_ ktesting.Action) (bool, runtime.Object, error) {
		creates++
		return false, nil, nil
	})
	failSave := true
	dyn.PrependReactor("patch", "applications", func(_ ktesting.Action) (bool, runtime.Object, error) {
		if failSave {
			failSave = false
			return true, nil, apierrors.NewConflict(applicationGVR.GroupResource(), "demo", fmt.Errorf("changed after Create"))
		}
		return false, nil, nil
	})
	if _, _, _, _, err := s.reconcileDiagnosis(ctx, "gitops", "demo"); !apierrors.IsConflict(err) {
		t.Fatalf("Create save conflict was not retryable: %v", err)
	}
	countRuns(t, s, 1)
	app, err := s.getApplication(ctx, "gitops", "demo")
	if err != nil {
		t.Fatal(err)
	}
	state, err := decodeIncident(app)
	if err != nil || state.RunCreated {
		t.Fatalf("failed save committed the incident: state=%+v err=%v", state, err)
	}
	// The next reconciliation sees the run in its initial list and must not
	// issue an extra exact GET or a second Create.
	gets := 0
	dyn.PrependReactor("get", "agenticruns", func(_ ktesting.Action) (bool, runtime.Object, error) {
		gets++
		return true, nil, fmt.Errorf("unexpected exact run GET")
	})
	run, reason, _, requeue, err := s.reconcileDiagnosis(ctx, "gitops", "demo")
	if err != nil || run == nil || reason != "creation confirmed" || !requeue || creates != 1 || gets != 0 {
		t.Fatalf("listed run not confirmed: run=%v reason=%q requeue=%v creates=%d gets=%d err=%v", run, reason, requeue, creates, gets, err)
	}
	app, err = s.getApplication(ctx, "gitops", "demo")
	if err != nil {
		t.Fatal(err)
	}
	state, err = decodeIncident(app)
	if err != nil || !state.RunCreated {
		t.Fatalf("confirmation was not persisted: state=%+v err=%v", state, err)
	}
}

func TestReconcileDiagnosisAdoptsNewestMatchingRun(t *testing.T) {
	s, dyn, now := incidentFixture(t)
	ctx := context.Background()
	app, err := s.getApplication(ctx, "gitops", "demo")
	if err != nil {
		t.Fatal(err)
	}
	var newest string
	for i, triggeredAt := range []time.Time{now.Add(-time.Hour), *now} {
		state := &incidentState{ID: fmt.Sprintf("existing-%d", i), Revision: "rev1", Attempt: 1}
		s.prepareRunAttempt(app, state)
		run := s.buildIncidentRun(app, state)
		annotations := run.GetAnnotations()
		annotations[utils.AppTriggeredAt] = triggeredAt.Format(time.RFC3339Nano)
		run.SetAnnotations(annotations)
		if err := dyn.Tracker().Create(agenticRunGVR, run, run.GetNamespace()); err != nil {
			t.Fatal(err)
		}
		newest = run.GetName()
	}
	if _, reason, _, requeue, err := s.reconcileDiagnosis(ctx, "gitops", "demo"); err != nil || reason != "existing run adopted" || !requeue {
		t.Fatalf("matching run not adopted: reason=%q requeue=%v err=%v", reason, requeue, err)
	}
	app, err = s.getApplication(ctx, "gitops", "demo")
	if err != nil {
		t.Fatal(err)
	}
	state, err := decodeIncident(app)
	if err != nil || state.RunName != newest || !state.RunCreated {
		t.Fatalf("wrong run adopted: state=%+v newest=%q err=%v", state, newest, err)
	}
}

func TestUnconfirmedIncidentContainsOnlyIdentity(t *testing.T) {
	s, _, _ := incidentFixture(t)
	app, err := s.getApplication(context.Background(), "gitops", "demo")
	if err != nil {
		t.Fatal(err)
	}
	state := &incidentState{ID: "size-example", Revision: "rev1", Attempt: 1}
	s.prepareRunAttempt(app, state)
	compact, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if len(compact) >= 256 || strings.Contains(string(compact), "\"snapshot\"") {
		t.Fatalf("unconfirmed incident contains extra state: %s", compact)
	}
}

func TestIncidentAdoptionAndApplicationRecreation(t *testing.T) {
	s, dyn, now := incidentFixture(t)
	run := reconcileApp(t, s)
	finishRun(t, dyn, run, true, *now)
	changeApp(t, dyn, func(app *unstructured.Unstructured) { app.SetAnnotations(nil) })
	reconcileApp(t, s)
	countRuns(t, s, 1)
	changeApp(t, dyn, func(app *unstructured.Unstructured) { app.SetUID("new-uid"); app.SetAnnotations(nil) })
	reconcileApp(t, s)
	countRuns(t, s, 2)
}

func TestIncidentAdoptionRequiresApplicationOwnership(t *testing.T) {
	s, dyn, _ := incidentFixture(t)
	app, err := s.getApplication(context.Background(), "gitops", "demo")
	if err != nil {
		t.Fatal(err)
	}
	checks := map[string]func(*unstructured.Unstructured){
		"manager": func(run *unstructured.Unstructured) {
			labels := run.GetLabels()
			labels[utils.ManagedBy] = "other-controller"
			run.SetLabels(labels)
		},
		"application name": func(run *unstructured.Unstructured) {
			annotations := run.GetAnnotations()
			annotations[utils.AppName] = "other"
			run.SetAnnotations(annotations)
		},
		"missing application name annotation": func(run *unstructured.Unstructured) {
			annotations := run.GetAnnotations()
			delete(annotations, utils.AppName)
			run.SetAnnotations(annotations)
			labels := run.GetLabels()
			labels[utils.AppName] = "demo"
			run.SetLabels(labels)
		},
		"application namespace": func(run *unstructured.Unstructured) {
			labels := run.GetLabels()
			labels[utils.AppNamespace] = "other"
			run.SetLabels(labels)
		},
		"application UID": func(run *unstructured.Unstructured) {
			labels := run.GetLabels()
			labels[utils.AppUID] = "other-uid"
			run.SetLabels(labels)
		},
		"revision": func(run *unstructured.Unstructured) {
			annotations := run.GetAnnotations()
			annotations[utils.AppRevision] = "old-revision"
			run.SetAnnotations(annotations)
		},
	}
	for name, mutate := range checks {
		run := s.buildIncidentRun(app, &incidentState{ID: name, Revision: "rev1", Attempt: 1, RunName: "foreign-" + strings.ReplaceAll(name, " ", "-"), RunNamespace: "lightspeed"})
		mutate(run)
		if err := unstructured.SetNestedSlice(run.Object, []any{map[string]any{"type": "Analyzed", "status": "True"}}, "status", "conditions"); err != nil {
			t.Fatal(err)
		}
		if err := dyn.Tracker().Create(agenticRunGVR, run, "lightspeed"); err != nil {
			t.Fatal(err)
		}
	}
	run := reconcileApp(t, s)
	if run == nil || strings.HasPrefix(run.GetName(), "foreign-") {
		t.Fatalf("unrelated run was adopted: run=%v", run)
	}
	countRuns(t, s, len(checks)+1)
	app, err = s.getApplication(context.Background(), "gitops", "demo")
	if err != nil {
		t.Fatal(err)
	}
	state, err := decodeIncident(app)
	if err != nil || state == nil || strings.HasPrefix(state.ID, "adopted-") {
		t.Fatalf("unrelated run was adopted: state=%+v err=%v", state, err)
	}
}

func TestDecodeIncidentRejectsUnknownFields(t *testing.T) {
	s, _, _ := incidentFixture(t)
	app, err := s.getApplication(context.Background(), "gitops", "demo")
	if err != nil {
		t.Fatal(err)
	}
	app.SetAnnotations(map[string]string{utils.IncidentAnnotation: `{"id":"incident","revision":"rev1","attempt":1,"runCreated":false,"runName":"run","runNamespace":"lightspeed","unsupported":true}`})
	if state, err := decodeIncident(app); err == nil || state != nil {
		t.Fatalf("accepted unknown field: state=%+v err=%v", state, err)
	}
}

func TestQueuedWebhookAndLiveHealthGrace(t *testing.T) {
	s, dyn, now := incidentFixture(t)
	s.config.AutoDiagnosisGracePeriod = 90 * time.Second
	changeApp(t, dyn, func(app *unstructured.Unstructured) {
		_ = unstructured.SetNestedField(app.Object, "Progressing", "status", "health", "status")
		_ = unstructured.SetNestedField(app.Object, now.Add(-89*time.Second).Format(time.RFC3339), "status", "health", "lastTransitionTime")
	})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/diagnose", strings.NewReader(`{"application":"demo","applicationNamespace":"gitops"}`))
	request.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	s.HandleDiagnose(w, request)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if got := s.queue.Len(); got != 1 {
		t.Fatalf("webhook queued %d Applications, want one", got)
	}
	countRuns(t, s, 0)
	processQueuedApplication(t, s)
	countRuns(t, s, 0)
	*now = now.Add(time.Second)
	s.enqueueApplication("gitops", "demo")
	processQueuedApplication(t, s)
	processQueuedApplication(t, s)
	countRuns(t, s, 1)
}

func TestUnconfirmedIncidentWaitsForRemainingGrace(t *testing.T) {
	s, dyn, now := incidentFixture(t)
	s.config.AutoDiagnosisGracePeriod = 2 * time.Minute
	changeApp(t, dyn, func(app *unstructured.Unstructured) {
		_ = unstructured.SetNestedField(app.Object, "Progressing", "status", "health", "status")
		_ = unstructured.SetNestedField(app.Object, now.Add(-90*time.Second).Format(time.RFC3339), "status", "health", "lastTransitionTime")
	})
	app, err := s.getApplication(context.Background(), "gitops", "demo")
	if err != nil {
		t.Fatal(err)
	}
	state := &incidentState{ID: "pending-grace", Revision: "rev1", Attempt: 1}
	s.prepareRunAttempt(app, state)
	if err := s.saveIncident(context.Background(), app, state); err != nil {
		t.Fatal(err)
	}
	run, reason, wait, _, err := s.reconcileDiagnosis(context.Background(), "gitops", "demo")
	if err != nil || run != nil || reason != "waiting for diagnosis grace period" || wait != 30*time.Second {
		t.Fatalf("run=%v reason=%q wait=%s err=%v", run, reason, wait, err)
	}
	countRuns(t, s, 0)
	app, err = s.getApplication(context.Background(), "gitops", "demo")
	if err != nil {
		t.Fatal(err)
	}
	state, err = decodeIncident(app)
	if err != nil || state.RunCreated || state.Closed {
		t.Fatalf("grace changed unconfirmed incident: state=%+v err=%v", state, err)
	}
	*now = now.Add(wait)
	if run := reconcileApp(t, s); run == nil || run.GetName() != state.RunName {
		t.Fatal("grace expiry did not create the pending deterministic run")
	}
	countRuns(t, s, 1)
}

// TestGraceClockStableAcrossArgoCDRetries verifies that ArgoCD sync retries
// resetting lastTransitionTime do not reset the grace period indefinitely.
func TestGraceClockStableAcrossArgoCDRetries(t *testing.T) {
	s, dyn, now := incidentFixture(t)
	grace := 90 * time.Second
	s.config.AutoDiagnosisGracePeriod = grace

	// App created 1h ago; lastTransitionTime initially set to now-89s.
	changeApp(t, dyn, func(app *unstructured.Unstructured) {
		_ = unstructured.SetNestedField(app.Object, "Progressing", "status", "health", "status")
		_ = unstructured.SetNestedField(app.Object, now.Add(-89*time.Second).Format(time.RFC3339), "status", "health", "lastTransitionTime")
	})

	// First reconcile: 1s remaining in grace period.
	_, reason, wait, _, err := s.reconcileDiagnosis(context.Background(), "gitops", "demo")
	if err != nil || reason != "waiting for diagnosis grace period" || wait != time.Second {
		t.Fatalf("first reconcile: reason=%q wait=%s err=%v", reason, wait, err)
	}

	// Simulate ArgoCD resetting lastTransitionTime (self-heal retry).
	changeApp(t, dyn, func(app *unstructured.Unstructured) {
		_ = unstructured.SetNestedField(app.Object, now.Format(time.RFC3339), "status", "health", "lastTransitionTime")
	})

	// Without the grace clock, Eligibility would now return wait=90s and the
	// grace period would reset. The grace clock must keep the original ~1s.
	_, reason, wait, _, err = s.reconcileDiagnosis(context.Background(), "gitops", "demo")
	if err != nil || reason != "waiting for diagnosis grace period" || wait > 2*time.Second {
		t.Fatalf("after lastTransitionTime reset: reason=%q wait=%s err=%v (expected ≤2s)", reason, wait, err)
	}

	// Advance past grace; next reconcile must create the incident and run.
	*now = now.Add(2 * time.Second)
	if run := reconcileApp(t, s); run == nil {
		t.Fatal("grace expiry did not create the run")
	}
	countRuns(t, s, 1)
}

func TestApplicationUIDChangeBetweenReadsRetries(t *testing.T) {
	s, dyn, _ := incidentFixture(t)
	reads := 0
	dyn.PrependReactor("get", "applications", func(action ktesting.Action) (bool, runtime.Object, error) {
		reads++
		if reads != 2 {
			return false, nil, nil
		}
		obj, err := dyn.Tracker().Get(applicationGVR, action.GetNamespace(), "demo")
		if err != nil {
			return true, nil, err
		}
		replacement := obj.(*unstructured.Unstructured).DeepCopy()
		replacement.SetUID("replacement-uid")
		replacement.SetResourceVersion("2")
		if err := dyn.Tracker().Update(applicationGVR, replacement, action.GetNamespace()); err != nil {
			return true, nil, err
		}
		return true, replacement, nil
	})
	s.enqueueApplication("gitops", "demo")
	processQueuedApplication(t, s)
	if got := s.queue.NumRequeues("gitops/demo"); got != 1 || reads != 2 {
		t.Fatalf("UID change did not stop and rate-limit this attempt: retries=%d reads=%d", got, reads)
	}
	countRuns(t, s, 0)
	waitForQueuedApplication(t, s)
	processQueuedApplication(t, s) // Record incident for replacement UID.
	processQueuedApplication(t, s) // Create its AgenticRun.
	runs, err := s.listAgenticRuns(context.Background(), "demo", "gitops", "replacement-uid")
	if err != nil || len(runs) != 1 || reads < 6 {
		t.Fatalf("did not retry for replacement Application: runs=%d reads=%d err=%v", len(runs), reads, err)
	}
	countRuns(t, s, 1)
}

func TestWebhookOnlyEnqueuesApplication(t *testing.T) {
	s, _, _ := incidentFixture(t)
	for range 3 {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/diagnose", strings.NewReader(`{"application":"demo","applicationNamespace":"gitops"}`))
		request.Header.Set("Authorization", "Bearer token")
		response := httptest.NewRecorder()
		s.HandleDiagnose(response, request)
		if response.Code != http.StatusAccepted {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	}
	if got := s.queue.Len(); got != 1 {
		t.Fatalf("duplicate webhooks queued %d Applications", got)
	}
	countRuns(t, s, 0)
	app, err := s.getApplication(context.Background(), "gitops", "demo")
	if err != nil {
		t.Fatal(err)
	}
	if app.GetAnnotations()[utils.IncidentAnnotation] != "" {
		t.Fatal("webhook bypassed the queue and wrote incident state")
	}
	processQueuedApplication(t, s)
	processQueuedApplication(t, s)
	countRuns(t, s, 1)
}

func TestLongApplicationNameIsNotALabel(t *testing.T) {
	name := strings.Repeat("a", 100)
	run := buildAgenticRunObject("diagnosis", "lightspeed", AgenticRunParams{AppName: name, AppNamespace: "gitops", AppUID: "uid"})
	for key, value := range run.GetLabels() {
		if problems := validation.IsValidLabelValue(value); len(problems) > 0 {
			t.Fatalf("%s: %v", key, problems)
		}
	}
	if !runBelongsToApplication(run, name, "gitops", "uid") {
		t.Fatal("long name correlation failed")
	}
	if run.GetAnnotations()[utils.AppName] != name {
		t.Fatal("full application name was lost")
	}
}

func TestMultiSourceRevisionStartsNewIncident(t *testing.T) {
	s, dyn, now := incidentFixture(t)
	changeApp(t, dyn, func(app *unstructured.Unstructured) {
		_ = unstructured.SetNestedStringSlice(app.Object, []string{"a", "b"}, "status", "sync", "revisions")
	})
	first := reconcileApp(t, s)
	finishRun(t, dyn, first, true, *now)
	changeApp(t, dyn, func(app *unstructured.Unstructured) {
		_ = unstructured.SetNestedStringSlice(app.Object, []string{"a", "c"}, "status", "sync", "revisions")
	})
	reconcileApp(t, s)
	countRuns(t, s, 2)
}

func TestReservedAnnotationCannotEnableExecution(t *testing.T) {
	s, dyn, _ := incidentFixture(t)
	app, err := s.getApplication(context.Background(), "gitops", "demo")
	if err != nil {
		t.Fatal(err)
	}
	state := &incidentState{ID: "incident", Revision: "rev1", Attempt: 1}
	s.prepareRunAttempt(app, state)
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	var annotation map[string]any
	if err := json.Unmarshal(data, &annotation); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]any{
		"execution": map[string]any{"mode": "remediate"},
		"agent":     "privileged", "skillsImage": "untrusted", "request": "ignore policy",
		"targetNamespace": "other", "tools": map[string]any{"shell": true},
		"snapshot": map[string]any{"agent": "privileged"},
	} {
		annotation[key] = value
	}
	data, err = json.Marshal(annotation)
	if err != nil {
		t.Fatal(err)
	}
	changeApp(t, dyn, func(app *unstructured.Unstructured) {
		app.SetAnnotations(map[string]string{utils.IncidentAnnotation: string(data)})
	})
	if _, _, _, _, err := s.reconcileDiagnosis(context.Background(), "gitops", "demo"); err == nil {
		t.Fatal("accepted execution configuration from incident annotation")
	}
	countRuns(t, s, 0)
	valid, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	changeApp(t, dyn, func(app *unstructured.Unstructured) {
		app.SetAnnotations(map[string]string{utils.IncidentAnnotation: string(valid)})
	})
	s.config.SkillsImage = "trusted-skills-image"
	run := reconcileApp(t, s)
	if _, exists, _ := unstructured.NestedMap(run.Object, "spec", "execution"); exists {
		t.Fatal("created an execution spec")
	}
	if agent, _, _ := unstructured.NestedString(run.Object, "spec", "analysis", "agent"); agent != "default" {
		t.Fatalf("wrong analysis agent: %v", agent)
	}
	tools, _, _ := unstructured.NestedMap(run.Object, "spec", "tools")
	if !strings.Contains(fmt.Sprint(tools), s.config.SkillsImage) {
		t.Fatalf("trusted skills image missing from tools: %v", tools)
	}
	request, _, _ := unstructured.NestedString(run.Object, "spec", "request")
	if strings.Contains(request, "ignore policy") {
		t.Fatal("used annotation request text")
	}
	if strings.Contains(request, "Destination") {
		t.Fatalf("request text still includes a destination namespace: %s", request)
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(run.Object, "spec", "targetNamespaces"); found {
		t.Fatal("spec.targetNamespaces must not be set (removed from AgenticRun API)")
	}
}

func mustGetRun(t *testing.T, s *TriggerService, name string) *unstructured.Unstructured {
	t.Helper()
	run, err := s.getAgenticRun(context.Background(), s.config.RunNamespace, name)
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func processQueuedApplication(t *testing.T, s *TriggerService) {
	t.Helper()
	if s.queue.Len() == 0 {
		t.Fatal("expected a queued Application")
	}
	if !s.processNextWorkItem(context.Background()) {
		t.Fatal("queue shut down")
	}
}

func waitForQueuedApplication(t *testing.T, s *TriggerService) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for s.queue.Len() == 0 {
		select {
		case <-deadline:
			t.Fatal("AgenticRun event did not enqueue its Application")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestAgenticRunEventsReplayRetryAfterRestart(t *testing.T) {
	s, dyn, now := incidentFixture(t)
	first := reconcileApp(t, s)
	finishRun(t, dyn, first, false, *now)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); s.runAgenticRunEvents(ctx) }()
	waitForQueuedApplication(t, s) // Initial informer list replays the failed run.
	processQueuedApplication(t, s)
	app, err := s.getApplication(context.Background(), "gitops", "demo")
	if err != nil {
		t.Fatal(err)
	}
	state, err := decodeIncident(app)
	if err != nil || state.RetryAt == nil {
		t.Fatalf("retry deadline not persisted: %v", err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("AgenticRun informer did not stop")
	}
	s.queue.ShutDown()

	*now = now.Add(10 * time.Minute)
	restarted := &TriggerService{dynClient: dyn, config: s.config, logger: logr.Discard(), now: s.now, queue: newDiagnosisQueue()}
	t.Cleanup(restarted.queue.ShutDown)
	restartCtx, restartCancel := context.WithCancel(context.Background())
	restartDone := make(chan struct{})
	go func() { defer close(restartDone); restarted.runAgenticRunEvents(restartCtx) }()
	waitForQueuedApplication(t, restarted)
	processQueuedApplication(t, restarted)
	processQueuedApplication(t, restarted)
	countRuns(t, restarted, 2)
	restartCancel()
	select {
	case <-restartDone:
	case <-time.After(3 * time.Second):
		t.Fatal("restarted informer did not stop")
	}
}

func TestAgenticRunEventRejectsRecreatedApplicationAndCoalesces(t *testing.T) {
	s, dyn, _ := incidentFixture(t)
	run := reconcileApp(t, s)
	withoutName := run.DeepCopy()
	annotations := withoutName.GetAnnotations()
	delete(annotations, utils.AppName)
	withoutName.SetAnnotations(annotations)
	labels := withoutName.GetLabels()
	labels[utils.AppName] = "demo"
	withoutName.SetLabels(labels)
	s.enqueueRunOwner(context.Background(), withoutName)
	if s.queue.Len() != 0 {
		t.Fatal("run without Application name annotation was queued")
	}
	changeApp(t, dyn, func(app *unstructured.Unstructured) { app.SetUID("replacement-uid") })
	for range 3 {
		s.enqueueRunOwner(context.Background(), run)
	}
	if got := s.queue.Len(); got != 0 {
		t.Fatalf("old Application UID queued replacement: %d items", got)
	}
	changeApp(t, dyn, func(app *unstructured.Unstructured) { app.SetUID("app-uid") })
	for range 3 {
		s.enqueueRunOwner(context.Background(), run)
	}
	if got := s.queue.Len(); got != 1 {
		t.Fatalf("duplicate events queued %d items, want one", got)
	}
	processQueuedApplication(t, s)
	countRuns(t, s, 1)
}

func TestAgenticRunStatusUpdateEnqueuesApplication(t *testing.T) {
	s, dyn, now := incidentFixture(t)
	first := reconcileApp(t, s)
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
	waitForQueuedApplication(t, s)
	processQueuedApplication(t, s) // Startup Add for a running run.
	finishRun(t, dyn, first, false, *now)
	waitForQueuedApplication(t, s) // Watch Update, not a scanner tick.
	processQueuedApplication(t, s)
	app, err := s.getApplication(context.Background(), "gitops", "demo")
	if err != nil {
		t.Fatal(err)
	}
	state, err := decodeIncident(app)
	if err != nil || state.RetryAt == nil {
		t.Fatalf("status event did not schedule retry: %v", err)
	}
}

func TestNewRevisionIsRecheckedAfterActiveRunFinishes(t *testing.T) {
	s, dyn, now := incidentFixture(t)
	first := reconcileApp(t, s)
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
	waitForQueuedApplication(t, s)
	processQueuedApplication(t, s) // Initial informer Add for revision A.

	changeApp(t, dyn, func(app *unstructured.Unstructured) {
		_ = unstructured.SetNestedField(app.Object, "rev2", "status", "sync", "revision")
	})
	s.enqueueApplication("gitops", "demo") // Notification for revision B.
	processQueuedApplication(t, s)
	countRuns(t, s, 1)
	app, err := s.getApplication(context.Background(), "gitops", "demo")
	if err != nil {
		t.Fatal(err)
	}
	state, err := decodeIncident(app)
	if err != nil || state == nil || state.Revision != "rev1" {
		t.Fatalf("active run's incident was replaced: state=%+v err=%v", state, err)
	}

	finishRun(t, dyn, first, true, *now)
	waitForQueuedApplication(t, s) // Informer Update for terminal revision A.
	for range 3 {
		s.enqueueRunOwner(ctx, mustGetRun(t, s, first.GetName()))
	}
	if got := s.queue.Len(); got != 1 {
		t.Fatalf("terminal run events queued %d Applications, want one", got)
	}
	processQueuedApplication(t, s)
	processQueuedApplication(t, s)
	countRuns(t, s, 2)
	app, err = s.getApplication(context.Background(), "gitops", "demo")
	if err != nil {
		t.Fatal(err)
	}
	state, err = decodeIncident(app)
	if err != nil || state == nil || state.Revision != "rev2" || !state.RunCreated || state.RunName == first.GetName() {
		t.Fatalf("revision B was not diagnosed after run A finished: state=%+v err=%v", state, err)
	}
	s.enqueueApplication("gitops", "demo")
	s.enqueueApplication("gitops", "demo")
	processQueuedApplication(t, s)
	countRuns(t, s, 2)
}

func TestAgenticRunEventRetriesTemporaryOwnerLookupFailure(t *testing.T) {
	s, dyn, _ := incidentFixture(t)
	run := reconcileApp(t, s)
	lookups := 0
	dyn.PrependReactor("get", "applications", func(action ktesting.Action) (bool, runtime.Object, error) {
		lookups++
		if lookups == 1 {
			return true, nil, fmt.Errorf("temporary Application API failure")
		}
		return false, nil, nil
	})
	s.enqueueRunOwner(context.Background(), run)
	if lookups < 2 || s.queue.Len() != 1 {
		t.Fatalf("owner event was lost after temporary error: lookups=%d queued=%d", lookups, s.queue.Len())
	}
}

func TestFailedRunEventSchedulesAttemptTwoWithoutAnotherNotification(t *testing.T) {
	s, dyn, now := incidentFixture(t)
	s.config.Cooldown = 80 * time.Millisecond
	first := reconcileApp(t, s)
	finishRun(t, dyn, first, false, *now)
	s.enqueueRunOwner(context.Background(), mustGetRun(t, s, first.GetName()))
	processQueuedApplication(t, s) // Persists RetryAt and schedules AddAfter.
	countRuns(t, s, 1)
	if got := s.queue.NumRequeues("gitops/demo"); got != 0 {
		t.Fatalf("cooldown consumed %d infrastructure retries", got)
	}
	*now = now.Add(s.config.Cooldown)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); s.runWorker(ctx) }()
	deadline := time.After(3 * time.Second)
	for {
		runs, err := s.listAgenticRuns(context.Background(), "demo", "gitops", "app-uid")
		if err != nil {
			t.Fatal(err)
		}
		if len(runs) == 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("delayed work did not create attempt 2")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Application worker did not stop")
	}
}
