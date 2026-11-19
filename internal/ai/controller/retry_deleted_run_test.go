package controller

import (
	"context"
	"testing"
	"time"

	"github.com/redhat-developer/gitops-operator/internal/ai/utils"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDeletedFailedRunRetainsScheduledRetry(t *testing.T) {
	for _, elapsed := range []time.Duration{30 * time.Second, 91 * time.Second} {
		t.Run(elapsed.String(), func(t *testing.T) {
			s, dyn, now := incidentFixture(t)
			s.config.Cooldown = 90 * time.Second
			s.config.MaxAttempts = 2
			ctx := context.Background()
			first := reconcileApp(t, s)
			require.NotNil(t, first)
			finishRun(t, dyn, first, false, *now)
			_, _, wait, _, err := s.reconcileDiagnosis(ctx, "gitops", "demo")
			require.NoError(t, err)
			require.Equal(t, s.config.Cooldown, wait)
			original := recoveryTestIncident(t, s)
			require.NotNil(t, original.RetryAt)

			*now = now.Add(elapsed)
			require.NoError(t, dyn.Resource(agenticRunGVR).Namespace(first.GetNamespace()).Delete(ctx, first.GetName(), metav1.DeleteOptions{}))
			if remaining := original.RetryAt.Sub(*now); remaining > 0 {
				run, _, wait, requeue, err := s.reconcileDiagnosis(ctx, "gitops", "demo")
				require.NoError(t, err)
				require.Nil(t, run)
				require.False(t, requeue)
				require.Equal(t, remaining, wait, "deletion must not lose the scheduled wakeup")
				require.Equal(t, original, recoveryTestIncident(t, s))
				countRuns(t, s, 0)
				*now = *original.RetryAt
			}

			second := reconcileApp(t, s)
			require.NotNil(t, second, "the persisted retry must advance even when its failed run was removed")
			require.NotEqual(t, first.GetName(), second.GetName())
			require.Equal(t, "2", second.GetAnnotations()[utils.AppAttempt])
			state := recoveryTestIncident(t, s)
			require.Equal(t, original.ID, state.ID)
			require.Equal(t, 2, state.Attempt)
			require.Nil(t, state.RetryAt)
			countRuns(t, s, 1) // Only attempt two remains in the API.
			finishRun(t, dyn, second, false, *now)
			*now = now.Add(2 * s.config.Cooldown)
			reconcileApp(t, s)
			require.Equal(t, 2, recoveryTestIncident(t, s).Attempt)
			countRuns(t, s, 1)
		})
	}
}

func TestDeletedFailedRunRetryHonorsReducedAttemptLimit(t *testing.T) {
	s, dyn, now := incidentFixture(t)
	s.config.MaxAttempts = 2
	first := reconcileApp(t, s)
	require.NotNil(t, first)
	finishRun(t, dyn, first, false, *now)
	reconcileApp(t, s)
	original := recoveryTestIncident(t, s)
	require.NotNil(t, original.RetryAt)
	require.NoError(t, dyn.Resource(agenticRunGVR).Namespace(first.GetNamespace()).Delete(context.Background(), first.GetName(), metav1.DeleteOptions{}))
	s.config.MaxAttempts = 1
	*now = original.RetryAt.Add(time.Second)
	run, _, wait, requeue, err := s.reconcileDiagnosis(context.Background(), "gitops", "demo")
	require.NoError(t, err)
	require.Nil(t, run)
	require.Zero(t, wait)
	require.False(t, requeue)
	require.Equal(t, original, recoveryTestIncident(t, s))
	countRuns(t, s, 0)
}

func TestDeletedFailedRunWithoutRecordedRetryRetainsBudget(t *testing.T) {
	s, dyn, now := incidentFixture(t)
	first := reconcileApp(t, s)
	require.NotNil(t, first)
	finishRun(t, dyn, first, false, *now)
	// No reconciliation has yet persisted evidence of a retryable failure.
	original := recoveryTestIncident(t, s)
	require.Nil(t, original.RetryAt)
	require.NoError(t, dyn.Resource(agenticRunGVR).Namespace(first.GetNamespace()).Delete(context.Background(), first.GetName(), metav1.DeleteOptions{}))
	*now = now.Add(2 * s.config.Cooldown)
	run, _, wait, requeue, err := s.reconcileDiagnosis(context.Background(), "gitops", "demo")
	require.NoError(t, err)
	require.Nil(t, run)
	require.Zero(t, wait)
	require.False(t, requeue)
	require.Equal(t, original, recoveryTestIncident(t, s))
	countRuns(t, s, 0)
}
