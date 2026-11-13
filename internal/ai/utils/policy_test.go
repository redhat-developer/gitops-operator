package utils

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"testing"
	"time"
)

func TestDurations(t *testing.T) {
	for _, value := range []string{"-1m", "bogus"} {
		if _, err := Duration(value, time.Minute, true); err == nil {
			t.Fatalf("accepted %s", value)
		}
	}
	if _, err := Duration("0s", time.Minute, false); err == nil {
		t.Fatal("accepted zero cooldown")
	}
	if _, err := Duration("0s", time.Minute, true); err != nil {
		t.Fatal(err)
	}
}

func TestEligibilityBoundaries(t *testing.T) {
	for _, grace := range []time.Duration{90 * time.Second, 10*time.Minute + 30*time.Second} {
		created := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
		for _, health := range []string{"Degraded", "Unknown", "Missing", "Progressing", "Healthy", "Suspended"} {
			app := &unstructured.Unstructured{Object: map[string]any{"status": map[string]any{"health": map[string]any{"status": health, "lastTransitionTime": created.Format(time.RFC3339)}}}}
			app.SetCreationTimestamp(metav1.NewTime(created))
			needed, wait := Eligibility(app, created.Add(grace-time.Second), grace)
			if Recovered(health) {
				if needed {
					t.Fatal("recovered app eligible")
				}
				continue
			}
			if !needed || wait != time.Second {
				t.Fatalf("%s %s: needed=%v wait=%v", health, grace, needed, wait)
			}
			_, wait = Eligibility(app, created.Add(grace), grace)
			if wait != 0 {
				t.Fatal("boundary rejected")
			}
		}
	}
}
