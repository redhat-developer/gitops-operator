package controller

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Exercise the smoke test's queue-driving sequence without a live cluster or
// AgenticRun controller. Cover both a persisted future cooldown and a failure
// event whose cooldown has already elapsed before it reaches the queue.
func TestSmokeQueueDrainsDiagnosisLifecycle(t *testing.T) {
	for _, failureEventDelay := range []time.Duration{0, 2 * time.Second} {
		t.Run(failureEventDelay.String(), func(t *testing.T) {
			s, dyn, now := incidentFixture(t)
			s.config.AutoDiagnosisGracePeriod = 0
			s.config.Cooldown = time.Second
			s.config.MaxAttempts = 2
			ctx := context.Background()
			notify := func() {
				sendDiagnosisNotification(t, s)
				drainSmokeQueue(t, ctx, s)
			}
			notify()
			countRuns(t, s, 1)
			first := recoveryTestIncident(t, s)
			require.True(t, first.RunCreated)
			notify()
			countRuns(t, s, 1)

			run := mustGetRun(t, s, first.RunName)
			finishRun(t, dyn, run, false, *now)
			*now = now.Add(failureEventDelay)
			s.enqueueRunOwner(ctx, mustGetRun(t, s, first.RunName))
			drainSmokeQueue(t, ctx, s)
			if failureEventDelay == 0 {
				countRuns(t, s, 1)
				require.NotNil(t, recoveryTestIncident(t, s).RetryAt)
				*now = now.Add(2 * time.Second)
				s.enqueueRunOwner(ctx, mustGetRun(t, s, first.RunName))
				drainSmokeQueue(t, ctx, s)
			}
			countRuns(t, s, 2)
			second := recoveryTestIncident(t, s)
			require.Equal(t, first.ID, second.ID)
			require.Equal(t, 2, second.Attempt)
			require.True(t, second.RunCreated)

			finishRun(t, dyn, mustGetRun(t, s, second.RunName), false, *now)
			*now = now.Add(time.Minute)
			notify()
			countRuns(t, s, 2)
			setRecoveryTestHealth(t, dyn, "Healthy", *now)
			notify()
			require.True(t, recoveryTestIncident(t, s).Closed)
			setRecoveryTestHealth(t, dyn, "Degraded", *now)
			notify()
			countRuns(t, s, 3)
			fresh := recoveryTestIncident(t, s)
			require.NotEqual(t, first.ID, fresh.ID)
			require.Equal(t, 1, fresh.Attempt)
			require.True(t, fresh.RunCreated)
		})
	}
}
