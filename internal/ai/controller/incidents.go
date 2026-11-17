package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redhat-developer/gitops-operator/internal/ai/utils"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

const (
	incidentIDAnnotation  = utils.AppIncidentID
	instanceUIDAnnotation = "gitops.openshift.io/argocd-instance-uid"
	maxDiagnosisAttempts  = 2
)

// The incident stores only the durable identity and retry state. A run that
// does not yet exist is built from the current Application during reconciliation.
type incidentState struct {
	ID           string     `json:"id"`
	Revision     string     `json:"revision"`
	Closed       bool       `json:"closed,omitempty"`
	Attempt      int        `json:"attempt"`
	RunCreated   bool       `json:"runCreated"`
	RetryAt      *time.Time `json:"retryAt,omitempty"`
	RunName      string     `json:"runName"`
	RunNamespace string     `json:"runNamespace"`
	InstanceUID  string     `json:"instanceUID,omitempty"`
}

func (s *TriggerService) currentTime() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now().UTC()
}

// Decode the incident state from the application annotations.
func decodeIncident(app *unstructured.Unstructured) (*incidentState, error) {
	value := app.GetAnnotations()[utils.IncidentAnnotation]
	if value == "" {
		return nil, nil
	}
	state := &incidentState{}
	if !json.Valid([]byte(value)) {
		return nil, fmt.Errorf("invalid persisted incident JSON")
	}
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(state); err != nil {
		return nil, fmt.Errorf("invalid persisted incident: %w", err)
	}
	if state.ID == "" || state.Attempt < 1 || state.RunName == "" || state.RunNamespace == "" {
		return nil, fmt.Errorf("invalid persisted incident identity")
	}
	return state, nil
}

func (state *incidentState) phase() string {
	if state.Closed {
		return "closed"
	}
	if !state.RunCreated {
		return "pending"
	}
	if state.RetryAt != nil {
		return "retry-scheduled"
	}
	return "running"
}

func (s *TriggerService) saveIncident(ctx context.Context, app *unstructured.Unstructured, state *incidentState) error {
	b, err := json.Marshal(state)
	if err != nil {
		return err
	}
	annotations := map[string]any{
		utils.IncidentAnnotation:  string(b),
		utils.DiagnosisState:      state.phase(),
		utils.AgenticRunName:      state.RunName,
		utils.AgenticRunNamespace: state.RunNamespace,
	}
	patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"uid": string(app.GetUID()), "resourceVersion": app.GetResourceVersion(), "annotations": annotations}})
	if err != nil {
		return err
	}
	_, err = s.dynClient.Resource(applicationGVR).Namespace(app.GetNamespace()).Patch(ctx, app.GetName(), types.MergePatchType, patch, metav1.PatchOptions{})
	return err
}

// closeIncident persists a closed incident state and removes the three visual
// annotations the UI reads, so the recommendation is no longer surfaced after
// recovery. The internal IncidentAnnotation is kept so the reconciler can
// correctly treat the next degradation as a new episode.
func (s *TriggerService) closeIncident(ctx context.Context, app *unstructured.Unstructured, state *incidentState) error {
	b, err := json.Marshal(state)
	if err != nil {
		return err
	}
	annotations := map[string]any{
		utils.IncidentAnnotation:  string(b),
		utils.DiagnosisState:      nil,
		utils.AgenticRunName:      nil,
		utils.AgenticRunNamespace: nil,
	}
	patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"uid": string(app.GetUID()), "resourceVersion": app.GetResourceVersion(), "annotations": annotations}})
	if err != nil {
		return err
	}
	_, err = s.dynClient.Resource(applicationGVR).Namespace(app.GetNamespace()).Patch(ctx, app.GetName(), types.MergePatchType, patch, metav1.PatchOptions{})
	return err
}

func deterministicRunName(uid, incident string, attempt int) string {
	sum := sha256.Sum256([]byte(uid + "/" + incident))
	return fmt.Sprintf("gitops-diag-%s-%d", hex.EncodeToString(sum[:16]), attempt)
}

func (s *TriggerService) prepareRunAttempt(app *unstructured.Unstructured, state *incidentState) {
	state.RunName = deterministicRunName(string(app.GetUID()), state.ID, state.Attempt)
	state.RunNamespace = s.config.RunNamespace
	state.RunCreated, state.RetryAt = false, nil
}

func (s *TriggerService) buildIncidentRun(app *unstructured.Unstructured, state *incidentState) *unstructured.Unstructured {
	syncStatus, _, _ := unstructured.NestedString(app.Object, "status", "sync", "status")
	run := buildAgenticRunObject(state.RunName, state.RunNamespace, AgenticRunParams{
		AppName: app.GetName(), AppNamespace: app.GetNamespace(), AppUID: string(app.GetUID()),
		SyncStatus: syncStatus, HealthStatus: utils.Health(app), Revision: state.Revision,
		SkillsImage: s.config.SkillsImage, SkillsPaths: s.config.SkillsPaths,
		Conditions: extractConditions(app), UnhealthyResources: extractUnhealthyResources(app),
	})
	annotations := run.GetAnnotations()
	annotations[incidentIDAnnotation] = state.ID
	annotations[instanceUIDAnnotation] = state.InstanceUID
	annotations[utils.AppAttempt] = fmt.Sprint(state.Attempt)
	annotations[utils.AppTriggeredAt] = s.currentTime().UTC().Format(time.RFC3339Nano)
	run.SetAnnotations(annotations)
	return run
}

func terminalAnalysisFailure(run *unstructured.Unstructured) (bool, time.Time) {
	if diagnosisStateForRun(run) != "failed" {
		return false, time.Time{}
	}
	conditions, _, _ := unstructured.NestedSlice(run.Object, "status", "conditions")
	for _, raw := range conditions {
		c, _ := raw.(map[string]any)
		if (stringFromMap(c, "type") == "Denied" || stringFromMap(c, "type") == "EmergencyStopped") && stringFromMap(c, "status") == "True" {
			return false, time.Time{}
		}
	}
	for _, raw := range conditions {
		c, _ := raw.(map[string]any)
		if stringFromMap(c, "type") == "Analyzed" && stringFromMap(c, "status") == "False" {
			t, _ := time.Parse(time.RFC3339Nano, stringFromMap(c, "lastTransitionTime"))
			return true, t
		}
	}
	return false, time.Time{}
}

func (s *TriggerService) validRunAttemptIdentity(app *unstructured.Unstructured, state *incidentState) bool {
	return state.RunName == deterministicRunName(string(app.GetUID()), state.ID, state.Attempt) &&
		state.RunNamespace == s.config.RunNamespace
}

// An existing run proves an ambiguous Create succeeded only when all durable
// correlation fields agree. Its spec may reflect an earlier Application state.
func runBelongsToIncident(run, app *unstructured.Unstructured, state *incidentState) bool {
	if run.GetName() != state.RunName || run.GetNamespace() != state.RunNamespace ||
		!runBelongsToApplication(run, app.GetName(), app.GetNamespace(), string(app.GetUID())) ||
		run.GetLabels()[utils.ManagedBy] != utils.Manager {
		return false
	}
	annotations := run.GetAnnotations()
	return state.InstanceUID != "" && annotations[instanceUIDAnnotation] == state.InstanceUID &&
		annotations[incidentIDAnnotation] == state.ID &&
		annotations[utils.AppAttempt] == fmt.Sprint(state.Attempt) &&
		annotations[utils.AppRevision] == state.Revision
}

// terminalDiagnosisError reports persisted state or ownership that cannot be
// repaired by retrying the same Application without an external change.
type terminalDiagnosisError struct{ err error }

func (e *terminalDiagnosisError) Error() string { return e.err.Error() }
func (e *terminalDiagnosisError) Unwrap() error { return e.err }

func terminalDiagnosis(err error) error { return &terminalDiagnosisError{err: err} }

// markRunCreated accepts a run only when it belongs to the persisted attempt.
// The Application patch remains the single durable transition for confirmation.
func (s *TriggerService) markRunCreated(ctx context.Context, app *unstructured.Unstructured, state *incidentState, run *unstructured.Unstructured) error {
	if !runBelongsToIncident(run, app, state) {
		return terminalDiagnosis(fmt.Errorf("existing run conflicts with incident identity"))
	}
	state.RunCreated = true
	return s.saveIncident(ctx, app, state)
}

// Confirm a persisted attempt before applying current Application policy.
// NotFound means Create did not leave a run; the caller may continue normally.
func (s *TriggerService) confirmExistingRun(ctx context.Context, app *unstructured.Unstructured, state *incidentState, previous *unstructured.Unstructured) (*unstructured.Unstructured, bool, error) {
	if !s.validRunAttemptIdentity(app, state) {
		return nil, false, terminalDiagnosis(fmt.Errorf("invalid incident run identity"))
	}
	if previous == nil {
		var err error
		previous, err = s.getAgenticRun(ctx, state.RunNamespace, state.RunName)
		if apierrors.IsNotFound(err) {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
	}
	if err := s.markRunCreated(ctx, app, state, previous); err != nil {
		return nil, false, err
	}
	return previous, true, nil
}

// reconcileCreatedAttempt either waits for a failed analysis cooldown or
// records the next attempt. Each call persists at most one incident transition.
func (s *TriggerService) reconcileCreatedAttempt(ctx context.Context, app *unstructured.Unstructured, state *incidentState, previous *unstructured.Unstructured) (*unstructured.Unstructured, string, time.Duration, bool, error) {
	maxAttempts := s.config.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = maxDiagnosisAttempts
	}
	if state.Attempt >= maxAttempts {
		return nil, "incident completed or retry budget exhausted", 0, false, nil
	}
	// A persisted deadline already proves a retryable failure. Honor it even
	// if the failed run has since been deleted, without restoring its budget.
	if state.RetryAt == nil {
		if previous == nil {
			return nil, "previous run removed; retry budget retained", 0, false, nil
		}
		failed, failedAt := terminalAnalysisFailure(previous)
		if !failed {
			return nil, "incident completed or retry budget exhausted", 0, false, nil
		}
		if failedAt.IsZero() {
			failedAt = s.currentTime()
		}
		retryAt := failedAt.Add(s.config.Cooldown)
		state.RetryAt = &retryAt
		if err := s.saveIncident(ctx, app, state); err != nil {
			return nil, "", 0, false, err
		}
		if remaining := retryAt.Sub(s.currentTime()); remaining > 0 {
			return nil, "waiting for failed diagnosis cooldown", remaining, false, nil
		}
		return nil, "retry deadline recorded", 0, true, nil
	}
	if remaining := state.RetryAt.Sub(s.currentTime()); remaining > 0 {
		return nil, "waiting for failed diagnosis cooldown", remaining, false, nil
	}
	state.Attempt++
	s.prepareRunAttempt(app, state)
	if err := s.saveIncident(ctx, app, state); err != nil {
		return nil, "", 0, false, err
	}
	return nil, "next diagnosis attempt recorded", 0, true, nil
}

// reconcileDiagnosis runs in the Application workqueue worker.
// All durable decisions are conditional on the live Application resourceVersion.
// The bool requests another queue pass after a successful persisted transition;
// it does not consume an infrastructure retry.
func (s *TriggerService) reconcileDiagnosis(ctx context.Context, namespace, name string, expectedInstanceUID ...string) (*unstructured.Unstructured, string, time.Duration, bool, error) {
	queuedUID := ""
	if len(expectedInstanceUID) > 0 {
		queuedUID = expectedInstanceUID[0]
	}
	appKey := diagnosisQueueKey(namespace, name, queuedUID)
	// A queued notification or run event is only a hint. The instance may have
	// opted out, been deleted, or lost its operator enrollment while work waited.
	instance, err := s.resolveManagingInstance(ctx, namespace)
	if err != nil {
		if isAuthorizationDenied(err) {
			s.clearHealthTimers(appKey)
			err = terminalDiagnosis(err)
		}
		return nil, "", 0, false, err
	}
	if len(expectedInstanceUID) > 0 && queuedUID != instance.UID {
		s.clearHealthTimers(appKey)
		return nil, "", 0, false, terminalDiagnosis(fmt.Errorf("queued diagnosis belongs to a different ArgoCD instance"))
	}
	appKey = diagnosisQueueKey(namespace, name, instance.UID)
	app, err := s.getApplication(ctx, namespace, name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			s.clearHealthTimers(appKey)
			err = terminalDiagnosis(err)
		}
		return nil, "", 0, false, err
	}
	if utils.Health(app) != "Healthy" {
		// Record interrupted recovery before any later operation can fail.
		// Preserve the diagnosis countdown and persisted incident retry budget.
		delete(s.recoveryStarted, appKey)
	}
	uid := string(app.GetUID())
	runs, err := s.listAgenticRuns(ctx, name, namespace, uid)
	if err != nil {
		return nil, "", 0, false, err
	}
	// A changed UID invalidates the run list; writes use the fresh resourceVersion.
	app, err = s.getApplication(ctx, namespace, name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			s.clearHealthTimers(appKey)
			err = terminalDiagnosis(err)
		}
		return nil, "", 0, false, err
	}
	if utils.Health(app) != "Healthy" {
		// Health can change between reads, even if decoding the incident fails.
		delete(s.recoveryStarted, appKey)
	}
	if string(app.GetUID()) != uid {
		s.clearHealthTimers(appKey)
		return nil, "", 0, false, errors.New("application UID changed while listing AgenticRuns")
	}
	state, err := decodeIncident(app)
	if err != nil {
		return nil, "", 0, false, terminalDiagnosis(err)
	}
	// Incidents without an instance UID cannot prove authorization. Keep
	// their retry budget intact rather than assigning them to the current CR.
	if state != nil && (state.InstanceUID == "" || state.InstanceUID != instance.UID) {
		s.clearHealthTimers(appKey)
		return nil, "", 0, false, terminalDiagnosis(fmt.Errorf("persisted diagnosis does not belong to the current ArgoCD instance"))
	}
	revision, health := utils.Revision(app), utils.Health(app)
	if entry, exists := s.graceStarted[appKey]; exists && (entry.appUID != uid || entry.revision != revision) {
		s.clearHealthTimers(appKey)
	}
	var active, previous, adoptable *unstructured.Unstructured
	for _, run := range runs {
		if diagnosisStateForRun(run) == "running" {
			active = run
		}
		if state != nil && run.GetName() == state.RunName && run.GetNamespace() == state.RunNamespace {
			previous = run
		}
		if state == nil && adoptable == nil && run.GetAnnotations()[utils.AppRevision] == revision {
			adoptable = run
		}
	}
	if previous != nil && previous.GetAnnotations()[instanceUIDAnnotation] != instance.UID {
		s.clearHealthTimers(appKey)
		return nil, "", 0, false, terminalDiagnosis(fmt.Errorf("previous run does not belong to the current ArgoCD instance"))
	}
	// An existing run for this revision must be adopted rather than duplicated.
	if adoptable != nil {
		if adoptable.GetAnnotations()[instanceUIDAnnotation] != instance.UID {
			s.clearHealthTimers(appKey)
			return nil, "", 0, false, terminalDiagnosis(fmt.Errorf("existing run does not belong to the current ArgoCD instance"))
		}
		// Carry over the run's own incident identity. Resetting these would
		// restore the retry budget, letting a lost annotation turn the last
		// attempt into the first and exceed MaxAttempts.
		adoptedAnnotations := adoptable.GetAnnotations()
		id := adoptedAnnotations[incidentIDAnnotation]
		attempt, err := strconv.Atoi(adoptedAnnotations[utils.AppAttempt])
		if id == "" || strings.TrimSpace(id) != id || err != nil || attempt < 1 {
			s.clearHealthTimers(appKey)
			return nil, "", 0, false, terminalDiagnosis(fmt.Errorf("existing run %s/%s has an invalid incident ID or attempt number", adoptable.GetNamespace(), adoptable.GetName()))
		}
		state = &incidentState{ID: id, Revision: revision, Attempt: attempt, RunCreated: true, RunName: adoptable.GetName(), RunNamespace: adoptable.GetNamespace(), InstanceUID: instance.UID}
		if !s.validRunAttemptIdentity(app, state) || !runBelongsToIncident(adoptable, app, state) {
			s.clearHealthTimers(appKey)
			return nil, "", 0, false, terminalDiagnosis(fmt.Errorf("existing run %s/%s conflicts with incident identity", adoptable.GetNamespace(), adoptable.GetName()))
		}
		if err := s.saveIncident(ctx, app, state); err != nil {
			return nil, "", 0, false, err
		}
		return nil, "existing run adopted", 0, true, nil
	}
	// Resolve an ambiguous Create before applying current Application policy.
	if state != nil && !state.RunCreated {
		run, confirmed, err := s.confirmExistingRun(ctx, app, state, previous)
		if err != nil {
			return nil, "", 0, false, err
		}
		if confirmed {
			return run, "creation confirmed", 0, true, nil
		}
	}
	if utils.Recovered(health) {
		_, awaitingDiagnosis := s.graceStarted[appKey]
		if health == "Healthy" && (awaitingDiagnosis || (state != nil && !state.Closed)) {
			if wait := s.recoveryWait(appKey, revision, app); wait > 0 {
				return nil, "waiting for sustained application recovery", wait, false, nil
			}
		}
		if state != nil && !state.Closed {
			state.Closed = true
			if err := s.closeIncident(ctx, app, state); err != nil {
				return nil, "", 0, false, err
			}
		}
		// Retain the completed recovery window until closing succeeds, so a
		// temporary API failure does not start another full recovery wait.
		s.clearHealthTimers(appKey)
		return nil, "application recovered", 0, false, nil
	}
	if state != nil && !state.RunCreated && !state.Closed && state.Revision != revision {
		s.clearHealthTimers(appKey)
		state.Closed = true
		if err := s.saveIncident(ctx, app, state); err != nil {
			return nil, "", 0, false, err
		}
		return nil, "stale incident closed", 0, true, nil
	}
	needed, eligWait := utils.Eligibility(app, s.currentTime(), s.config.AutoDiagnosisGracePeriod)
	if !needed {
		s.clearHealthTimers(appKey)
		if state != nil && !state.RunCreated && !state.Closed {
			state.Closed = true
			if err := s.saveIncident(ctx, app, state); err != nil {
				return nil, "", 0, false, err
			}
		}
		return nil, "health does not qualify", 0, false, nil
	}
	// Use a stable grace clock so ArgoCD sync retries that reset lastTransitionTime
	// do not extend the grace period indefinitely.
	wait := s.graceClock(appKey, uid, revision, eligWait)
	if wait > 0 {
		return nil, "waiting for diagnosis grace period", wait, false, nil
	}
	newIncident := state == nil || state.Closed || state.Revision != revision
	if active != nil {
		return nil, "diagnosis already running", 0, false, nil
	}
	if newIncident {
		state = &incidentState{ID: uuid.NewString(), Revision: revision, Attempt: 1, InstanceUID: instance.UID}
		s.prepareRunAttempt(app, state)
		if err := s.saveIncident(ctx, app, state); err != nil {
			return nil, "", 0, false, err
		}
		return nil, "incident recorded", 0, true, nil
	}
	if !state.RunCreated {
		// Build the absent deterministic run from the current Application.
		previous, err = s.createAgenticRun(ctx, s.buildIncidentRun(app, state))
		if apierrors.IsAlreadyExists(err) {
			previous, err = s.getAgenticRun(ctx, state.RunNamespace, state.RunName)
		}
		if err != nil {
			return nil, "", 0, false, err
		}
		if err := s.markRunCreated(ctx, app, state, previous); err != nil {
			return nil, "", 0, false, err
		}
		s.logger.Info("AgenticRun creation confirmed", "app", namespace+"/"+name, "incident", state.ID, "attempt", state.Attempt, "run", state.RunName)
		return previous, "created", 0, false, nil
	}
	return s.reconcileCreatedAttempt(ctx, app, state, previous)
}
