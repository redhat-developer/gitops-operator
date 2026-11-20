package controller

import (
	"context"
	"testing"
	"time"

	"github.com/redhat-developer/gitops-operator/internal/ai/utils"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
)

func setRecoveryTestHealth(t *testing.T, dyn *dynamicfake.FakeDynamicClient, health string, at time.Time) {
	t.Helper()
	changeApp(t, dyn, func(app *unstructured.Unstructured) {
		require.NoError(t, unstructured.SetNestedField(app.Object, health, "status", "health", "status"))
		require.NoError(t, unstructured.SetNestedField(app.Object, at.Format(time.RFC3339Nano), "status", "health", "lastTransitionTime"))
	})
}

func recoveryTestIncident(t *testing.T, s *TriggerService) *incidentState {
	t.Helper()
	app, err := s.getApplication(context.Background(), "gitops", "demo")
	require.NoError(t, err)
	state, err := decodeIncident(app)
	require.NoError(t, err)
	require.NotNil(t, state)
	return state
}

// The live OOM reproducer remains Synced and alternates between Progressing and
// one-second Healthy observations as the container restarts. Those observations
// must not postpone diagnosis forever.
func TestRecoveryBriefHealthyFlapsKeepOriginalDiagnosisDeadline(t *testing.T) {
	s, dyn, now := incidentFixture(t)
	s.config.AutoDiagnosisGracePeriod = 90 * time.Second
	start := *now
	setRecoveryTestHealth(t, dyn, "Progressing", *now)
	_, _, wait, _, err := s.reconcileDiagnosis(context.Background(), "gitops", "demo")
	require.NoError(t, err)
	require.Equal(t, 90*time.Second, wait)

	for _, elapsed := range []time.Duration{30 * time.Second, 60 * time.Second, 91 * time.Second} {
		*now = start.Add(elapsed)
		setRecoveryTestHealth(t, dyn, "Healthy", *now)
		run, _, wait, _, err := s.reconcileDiagnosis(context.Background(), "gitops", "demo")
		require.NoError(t, err)
		require.Nil(t, run, "even an expired diagnosis timer must not create a run while Healthy")
		require.Positive(t, wait, "brief Healthy observations require a recovery recheck")
		countRuns(t, s, 0)

		*now = now.Add(time.Second)
		setRecoveryTestHealth(t, dyn, "Progressing", *now)
		if elapsed < 90*time.Second {
			_, _, wait, _, err = s.reconcileDiagnosis(context.Background(), "gitops", "demo")
			require.NoError(t, err)
			require.Equal(t, 90*time.Second-now.Sub(start), wait, "healthy flap restarted the original grace period")
		} else {
			require.NotNil(t, reconcileApp(t, s), "expired original deadline must diagnose the still-unhealthy application")
		}
	}
	countRuns(t, s, 1)
}

func TestRecoveryStableHealthClearsGraceBeforeNextFailure(t *testing.T) {
	s, dyn, now := incidentFixture(t)
	s.config.AutoDiagnosisGracePeriod = 90 * time.Second
	setRecoveryTestHealth(t, dyn, "Progressing", *now)
	_, _, wait, _, err := s.reconcileDiagnosis(context.Background(), "gitops", "demo")
	require.NoError(t, err)
	require.Equal(t, 90*time.Second, wait)

	*now = now.Add(30 * time.Second)
	setRecoveryTestHealth(t, dyn, "Healthy", *now)
	_, _, wait, _, err = s.reconcileDiagnosis(context.Background(), "gitops", "demo")
	require.NoError(t, err)
	require.Equal(t, 90*time.Second, wait)
	*now = now.Add(90 * time.Second)
	_, reason, wait, _, err := s.reconcileDiagnosis(context.Background(), "gitops", "demo")
	require.NoError(t, err)
	require.Equal(t, "application recovered", reason)
	require.Zero(t, wait)
	countRuns(t, s, 0)

	setRecoveryTestHealth(t, dyn, "Progressing", *now)
	_, _, wait, _, err = s.reconcileDiagnosis(context.Background(), "gitops", "demo")
	require.NoError(t, err)
	require.Equal(t, 90*time.Second, wait, "a genuinely recovered application needs fresh grace on its next failure")
}

func TestRecoveryBriefHealthPreservesExhaustedIncidentBudget(t *testing.T) {
	s, dyn, now := incidentFixture(t)
	s.config.AutoDiagnosisGracePeriod = 90 * time.Second
	s.config.MaxAttempts = 1
	run := reconcileApp(t, s)
	require.NotNil(t, run)
	finishRun(t, dyn, run, false, *now)
	reconcileApp(t, s)
	original := recoveryTestIncident(t, s)

	setRecoveryTestHealth(t, dyn, "Healthy", *now)
	_, _, wait, _, err := s.reconcileDiagnosis(context.Background(), "gitops", "demo")
	require.NoError(t, err)
	require.Positive(t, wait)
	state := recoveryTestIncident(t, s)
	require.False(t, state.Closed)
	require.Equal(t, original.ID, state.ID)
	require.Equal(t, original.Attempt, state.Attempt)

	*now = now.Add(time.Second)
	setRecoveryTestHealth(t, dyn, "Progressing", *now)
	reconcileApp(t, s)
	*now = now.Add(90 * time.Second)
	reconcileApp(t, s)
	countRuns(t, s, 1)
	state = recoveryTestIncident(t, s)
	require.False(t, state.Closed)
	require.Equal(t, original.ID, state.ID)
	require.Equal(t, 1, state.Attempt)

	setRecoveryTestHealth(t, dyn, "Healthy", *now)
	reconcileApp(t, s)
	*now = now.Add(90 * time.Second)
	reconcileApp(t, s)
	require.True(t, recoveryTestIncident(t, s).Closed)
	app, err := s.getApplication(context.Background(), "gitops", "demo")
	require.NoError(t, err)
	require.Empty(t, app.GetAnnotations()[utils.DiagnosisState])
	require.Empty(t, app.GetAnnotations()[utils.AgenticRunName])
	require.Empty(t, app.GetAnnotations()[utils.AgenticRunNamespace])
}

func TestRecoveryUnusableTimestampUsesObservedHealthyPeriod(t *testing.T) {
	for _, timestamp := range []string{"", "not-a-timestamp", "2099-01-01T00:00:00Z"} {
		t.Run(timestamp, func(t *testing.T) {
			s, dyn, now := incidentFixture(t)
			s.config.AutoDiagnosisGracePeriod = 90 * time.Second
			require.NotNil(t, reconcileApp(t, s))
			changeApp(t, dyn, func(app *unstructured.Unstructured) {
				require.NoError(t, unstructured.SetNestedField(app.Object, "Healthy", "status", "health", "status"))
				require.NoError(t, unstructured.SetNestedField(app.Object, timestamp, "status", "health", "lastTransitionTime"))
			})
			_, _, wait, _, err := s.reconcileDiagnosis(context.Background(), "gitops", "demo")
			require.NoError(t, err)
			require.Equal(t, 90*time.Second, wait)
			require.False(t, recoveryTestIncident(t, s).Closed)
			*now = now.Add(30 * time.Second)
			_, _, wait, _, err = s.reconcileDiagnosis(context.Background(), "gitops", "demo")
			require.NoError(t, err)
			require.Equal(t, time.Minute, wait)
			*now = now.Add(time.Minute)
			reconcileApp(t, s)
			require.True(t, recoveryTestIncident(t, s).Closed)
			countRuns(t, s, 1)
		})
	}
}

func TestRecoveryQueueConfirmsHealthWithoutAnotherWebhook(t *testing.T) {
	s, dyn, now := incidentFixture(t)
	s.config.AutoDiagnosisGracePeriod = 80 * time.Millisecond
	require.NotNil(t, reconcileApp(t, s))
	setRecoveryTestHealth(t, dyn, "Healthy", *now)
	sendDiagnosisNotification(t, s)
	processQueuedApplication(t, s)
	require.False(t, recoveryTestIncident(t, s).Closed)

	// Process the queue's scheduled recheck, with no second notification.
	*now = now.Add(s.config.AutoDiagnosisGracePeriod)
	waitForQueuedApplication(t, s)
	processQueuedApplication(t, s)
	require.True(t, recoveryTestIncident(t, s).Closed)
	countRuns(t, s, 1)
}

func TestRecoveryObservedHealthyClockRestartsAfterUnhealthy(t *testing.T) {
	s, dyn, now := incidentFixture(t)
	s.config.AutoDiagnosisGracePeriod = 90 * time.Second
	require.NotNil(t, reconcileApp(t, s))
	setHealthyWithoutTimestamp := func() {
		changeApp(t, dyn, func(app *unstructured.Unstructured) {
			require.NoError(t, unstructured.SetNestedField(app.Object, "Healthy", "status", "health", "status"))
			unstructured.RemoveNestedField(app.Object, "status", "health", "lastTransitionTime")
		})
	}
	setHealthyWithoutTimestamp()
	reconcileApp(t, s)
	*now = now.Add(time.Minute)
	setRecoveryTestHealth(t, dyn, "Progressing", *now)
	reconcileApp(t, s)
	*now = now.Add(time.Second)
	setHealthyWithoutTimestamp()
	_, _, wait, _, err := s.reconcileDiagnosis(context.Background(), "gitops", "demo")
	require.NoError(t, err)
	require.Equal(t, 90*time.Second, wait, "interrupted recovery reused its earlier observation")

	*now = now.Add(30 * time.Second)
	_, _, wait, _, err = s.reconcileDiagnosis(context.Background(), "gitops", "demo")
	require.NoError(t, err)
	require.Equal(t, time.Minute, wait)
	require.False(t, recoveryTestIncident(t, s).Closed)
	*now = now.Add(time.Minute)
	reconcileApp(t, s)
	require.True(t, recoveryTestIncident(t, s).Closed)
	countRuns(t, s, 1)
}

func TestRecoveryUnhealthyObservationSurvivesReconcileErrors(t *testing.T) {
	for _, failure := range []string{"run listing", "second application read", "incident decoding"} {
		t.Run(failure, func(t *testing.T) {
			s, dyn, now := incidentFixture(t)
			s.config.AutoDiagnosisGracePeriod = 90 * time.Second
			s.config.MaxAttempts = 1
			run := reconcileApp(t, s)
			require.NotNil(t, run)
			finishRun(t, dyn, run, false, *now)
			reconcileApp(t, s)
			original := recoveryTestIncident(t, s)
			setHealthyWithoutTimestamp := func() {
				changeApp(t, dyn, func(app *unstructured.Unstructured) {
					require.NoError(t, unstructured.SetNestedField(app.Object, "Healthy", "status", "health", "status"))
					unstructured.RemoveNestedField(app.Object, "status", "health", "lastTransitionTime")
				})
			}
			setHealthyWithoutTimestamp()
			_, _, wait, _, err := s.reconcileDiagnosis(context.Background(), "gitops", "demo")
			require.NoError(t, err)
			require.Equal(t, 90*time.Second, wait)

			// Observe a failed recovery at t=60, even though a later operation
			// prevents this reconciliation from completing.
			*now = now.Add(time.Minute)
			if failure != "incident decoding" {
				setRecoveryTestHealth(t, dyn, "Progressing", *now)
			}
			injectFailure := true
			dyn.PrependReactor("list", "agenticruns", func(ktesting.Action) (bool, runtime.Object, error) {
				if injectFailure && failure == "run listing" {
					return true, nil, apierrors.NewServiceUnavailable("injected run listing failure")
				}
				return false, nil, nil
			})
			reads := 0
			dyn.PrependReactor("get", "applications", func(ktesting.Action) (bool, runtime.Object, error) {
				if !injectFailure {
					return false, nil, nil
				}
				reads++
				if reads == 2 {
					switch failure {
					case "second application read":
						return true, nil, apierrors.NewServiceUnavailable("injected application read failure")
					case "incident decoding":
						// The first read was Healthy. Only the second read sees
						// the unhealthy status and malformed incident annotation.
						obj, err := dyn.Tracker().Get(applicationGVR, "gitops", "demo")
						require.NoError(t, err)
						app := obj.(*unstructured.Unstructured).DeepCopy()
						require.NoError(t, unstructured.SetNestedField(app.Object, "Progressing", "status", "health", "status"))
						annotations := app.GetAnnotations()
						annotations[utils.IncidentAnnotation] = "invalid-json"
						app.SetAnnotations(annotations)
						return true, app, nil
					}
				}
				return false, nil, nil
			})
			_, _, _, _, err = s.reconcileDiagnosis(context.Background(), "gitops", "demo")
			require.Error(t, err)
			injectFailure = false

			*now = now.Add(time.Second)
			setHealthyWithoutTimestamp()
			_, _, wait, _, err = s.reconcileDiagnosis(context.Background(), "gitops", "demo")
			require.NoError(t, err)
			require.Equal(t, 90*time.Second, wait, "t=61 must start a fresh recovery window")
			*now = now.Add(29 * time.Second)
			_, _, wait, _, err = s.reconcileDiagnosis(context.Background(), "gitops", "demo")
			require.NoError(t, err)
			require.Equal(t, 61*time.Second, wait, "t=90 is too early to confirm recovery")
			state := recoveryTestIncident(t, s)
			require.False(t, state.Closed)
			require.Equal(t, original.ID, state.ID)
			require.Equal(t, original.Attempt, state.Attempt)

			// Another degradation must not restore the exhausted attempt budget.
			setRecoveryTestHealth(t, dyn, "Degraded", *now)
			reconcileApp(t, s)
			countRuns(t, s, 1)
			require.Equal(t, original.ID, recoveryTestIncident(t, s).ID)
			setHealthyWithoutTimestamp()
			reconcileApp(t, s)
			*now = now.Add(90 * time.Second)
			reconcileApp(t, s)
			require.True(t, recoveryTestIncident(t, s).Closed)
		})
	}
}

func TestRecoveryCloseFailurePreservesCompletedWindow(t *testing.T) {
	s, dyn, now := incidentFixture(t)
	s.config.AutoDiagnosisGracePeriod = 90 * time.Second
	require.NotNil(t, reconcileApp(t, s))
	changeApp(t, dyn, func(app *unstructured.Unstructured) {
		require.NoError(t, unstructured.SetNestedField(app.Object, "Healthy", "status", "health", "status"))
		unstructured.RemoveNestedField(app.Object, "status", "health", "lastTransitionTime")
	})
	reconcileApp(t, s)
	*now = now.Add(90 * time.Second)
	failClose := true
	dyn.PrependReactor("patch", "applications", func(ktesting.Action) (bool, runtime.Object, error) {
		if failClose {
			return true, nil, apierrors.NewServiceUnavailable("temporary close failure")
		}
		return false, nil, nil
	})
	_, _, _, _, err := s.reconcileDiagnosis(context.Background(), "gitops", "demo")
	require.Error(t, err)
	require.False(t, recoveryTestIncident(t, s).Closed)

	failClose = false
	*now = now.Add(time.Second)
	_, _, wait, _, err := s.reconcileDiagnosis(context.Background(), "gitops", "demo")
	require.NoError(t, err)
	require.Zero(t, wait, "retrying the close must not restart the completed recovery window")
	require.True(t, recoveryTestIncident(t, s).Closed)
	require.Empty(t, s.recoveryStarted)
	require.Empty(t, s.graceStarted)
	countRuns(t, s, 1)
}

func TestRecoverySuspendedStillClosesImmediately(t *testing.T) {
	s, dyn, now := incidentFixture(t)
	require.NotNil(t, reconcileApp(t, s))
	setRecoveryTestHealth(t, dyn, "Suspended", *now)
	_, reason, wait, _, err := s.reconcileDiagnosis(context.Background(), "gitops", "demo")
	require.NoError(t, err)
	require.Equal(t, "application recovered", reason)
	require.Zero(t, wait)
	require.True(t, recoveryTestIncident(t, s).Closed)
	countRuns(t, s, 1)
}
