package controller

import (
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// recoveryWait requires sustained Healthy status before forgetting an unhealthy
// episode. It leaves the diagnosis timer intact so a brief restart between OOMs
// cannot keep postponing diagnosis or replenish an incident's retry budget.
func (s *TriggerService) recoveryWait(key, revision string, app *unstructured.Unstructured) time.Duration {
	grace := s.config.AutoDiagnosisGracePeriod
	if grace <= 0 {
		return 0
	}
	now := s.currentTime()
	value, _, _ := unstructured.NestedString(app.Object, "status", "health", "lastTransitionTime")
	start, err := time.Parse(time.RFC3339, value)
	if err == nil && !start.After(now) {
		// A replacement Application cannot inherit health predating its creation.
		if created := app.GetCreationTimestamp().Time; created.After(start) {
			start = created
		}
		delete(s.recoveryStarted, key)
	} else {
		// Missing, malformed, or future timestamps cannot establish sustained
		// health. Start from observation and retain it across delayed rechecks.
		if s.recoveryStarted == nil {
			s.recoveryStarted = map[string]recoveryEntry{}
		}
		entry, exists := s.recoveryStarted[key]
		if !exists || entry.appUID != string(app.GetUID()) || entry.revision != revision {
			entry = recoveryEntry{start: now, appUID: string(app.GetUID()), revision: revision}
			s.recoveryStarted[key] = entry
		}
		start = entry.start
	}
	return max(0, grace-now.Sub(start))
}
