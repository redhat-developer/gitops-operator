package controller

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/redhat-developer/gitops-operator/internal/ai/utils"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestAdoptionRejectsDamagedRetryMetadata(t *testing.T) {
	tests := []struct {
		name   string
		remove []string
		values map[string]string
	}{
		{name: "missing incident ID", remove: []string{incidentIDAnnotation}},
		{name: "empty incident ID", values: map[string]string{incidentIDAnnotation: ""}},
		{name: "whitespace incident ID", values: map[string]string{incidentIDAnnotation: " "}},
		{name: "changed incident ID", values: map[string]string{incidentIDAnnotation: "different-incident"}},
		{name: "missing attempt", remove: []string{utils.AppAttempt}},
		{name: "nonnumeric attempt", values: map[string]string{utils.AppAttempt: "two"}},
		{name: "zero attempt", values: map[string]string{utils.AppAttempt: "0"}},
		{name: "negative attempt", values: map[string]string{utils.AppAttempt: "-2"}},
		{name: "overflowing attempt", values: map[string]string{utils.AppAttempt: "999999999999999999999999999999"}},
		{name: "noncanonical attempt", values: map[string]string{utils.AppAttempt: "02"}},
		{name: "signed attempt", values: map[string]string{utils.AppAttempt: "+2"}},
		{name: "wrong positive attempt", values: map[string]string{utils.AppAttempt: "1"}},
		{name: "missing incident ID and attempt", remove: []string{incidentIDAnnotation, utils.AppAttempt}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, dyn, now := incidentFixture(t)
			s.config.MaxAttempts = 2
			ctx := context.Background()
			first := reconcileApp(t, s)
			if first == nil {
				t.Fatal("initial diagnosis was not created")
			}
			finishRun(t, dyn, first, false, *now)
			reconcileApp(t, s) // Persist the failed attempt's retry deadline.
			*now = now.Add(s.config.Cooldown)
			second := reconcileApp(t, s)
			if second == nil {
				t.Fatal("second diagnosis was not created")
			}
			finishRun(t, dyn, second, false, *now)
			*now = now.Add(time.Hour)
			countRuns(t, s, 2)

			validRun := mustGetRun(t, s, second.GetName())
			if validRun.GetAnnotations()[utils.AppAttempt] != "2" {
				t.Fatal("fixture did not exhaust the retry budget")
			}
			damagedRun := validRun.DeepCopy()
			annotations := damagedRun.GetAnnotations()
			for _, key := range tt.remove {
				delete(annotations, key)
			}
			for key, value := range tt.values {
				annotations[key] = value
			}
			damagedRun.SetAnnotations(annotations)
			if err := dyn.Tracker().Update(agenticRunGVR, damagedRun, damagedRun.GetNamespace()); err != nil {
				t.Fatal(err)
			}
			changeApp(t, dyn, func(app *unstructured.Unstructured) {
				annotations := app.GetAnnotations()
				delete(annotations, utils.IncidentAnnotation)
				app.SetAnnotations(annotations)
			})
			before, err := s.getApplication(ctx, "gitops", "demo")
			if err != nil {
				t.Fatal(err)
			}
			dyn.ClearActions()

			// The older valid run remains in the list. Reject the newest run's
			// damaged identity instead of adopting the older attempt or guessing.
			run, _, wait, requeue, err := s.reconcileDiagnosis(ctx, "gitops", "demo")
			var terminal *terminalDiagnosisError
			if !errors.As(err, &terminal) || run != nil || wait != 0 || requeue {
				t.Fatalf("damaged metadata was not terminal: run=%v wait=%v requeue=%v err=%v", run, wait, requeue, err)
			}
			key := diagnosisQueueKey("gitops", "demo", "argocd-gitops-argocd")
			for range 3 {
				s.queue.Add(key)
				processQueuedApplication(t, s)
				if s.queue.Len() != 0 || s.queue.NumRequeues(key) != 0 {
					t.Fatal("damaged metadata was retried by the workqueue")
				}
			}
			for _, action := range dyn.Actions() {
				switch action.GetVerb() {
				case "create", "patch", "update", "delete":
					t.Fatalf("damaged metadata caused %s on %s", action.GetVerb(), action.GetResource().Resource)
				}
			}
			after, err := s.getApplication(ctx, "gitops", "demo")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before.Object, after.Object) {
				t.Fatal("Application was changed while adoption was rejected")
			}
			if !reflect.DeepEqual(damagedRun.Object, mustGetRun(t, s, damagedRun.GetName()).Object) {
				t.Fatal("run metadata was rewritten during adoption")
			}
			countRuns(t, s, 2)

			// Repairing the same run permits recovery of its actual spent budget,
			// even if the configured limit has since been lowered below attempt 2.
			if err := dyn.Tracker().Update(agenticRunGVR, validRun, validRun.GetNamespace()); err != nil {
				t.Fatal(err)
			}
			s.config.MaxAttempts = 1
			reconcileApp(t, s)
			restoredApp, err := s.getApplication(ctx, "gitops", "demo")
			if err != nil {
				t.Fatal(err)
			}
			state, err := decodeIncident(restoredApp)
			if err != nil || state == nil || state.Attempt != 2 || state.ID != validRun.GetAnnotations()[incidentIDAnnotation] {
				t.Fatalf("repaired run did not preserve its identity and budget: state=%+v err=%v", state, err)
			}
			countRuns(t, s, 2)
		})
	}
}

func TestAdoptionOfFirstAttemptPreservesOneRemainingRetry(t *testing.T) {
	s, dyn, now := incidentFixture(t)
	s.config.MaxAttempts = 2
	first := reconcileApp(t, s)
	if first == nil {
		t.Fatal("initial diagnosis was not created")
	}
	finishRun(t, dyn, first, false, *now)
	changeApp(t, dyn, func(app *unstructured.Unstructured) { app.SetAnnotations(nil) })
	reconcileApp(t, s)
	countRuns(t, s, 1)
	*now = now.Add(s.config.Cooldown)
	second := reconcileApp(t, s)
	if second == nil || second.GetAnnotations()[utils.AppAttempt] != "2" || second.GetAnnotations()[incidentIDAnnotation] != first.GetAnnotations()[incidentIDAnnotation] {
		t.Fatalf("valid adoption did not preserve the remaining retry: %v", second)
	}
	finishRun(t, dyn, second, false, *now)
	*now = now.Add(time.Hour)
	reconcileApp(t, s)
	countRuns(t, s, 2)
}
