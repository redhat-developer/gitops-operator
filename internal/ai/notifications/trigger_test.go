package notifications

import (
	"strings"
	"testing"
	"time"

	"github.com/argoproj/notifications-engine/pkg/triggers"
	"sigs.k8s.io/yaml"
)

func TestTriggerDeliversHealthWithoutGraceFiltering(t *testing.T) {
	got := Trigger()
	for _, health := range []string{"Degraded", "Unknown", "Missing", "Progressing", "Healthy", "Suspended"} {
		if !strings.Contains(got, "app.status.health.status == '"+health+"'") {
			t.Errorf("trigger missing health clause %s:\n%s", health, got)
		}
	}
	if strings.Contains(got, "creationTimestamp") || strings.Contains(got, "time.Parse") || strings.Contains(got, "time.Now().Sub") {
		t.Fatalf("notification trigger still filters by grace: %s", got)
	}
	if strings.Count(got, "  send: [gitops-ai-diagnose]") != 6 {
		t.Fatalf("missing notification delivery clause: %s", got)
	}
}

func TestTriggerEngineDeliversEarlyHealthAndAllowsRedelivery(t *testing.T) {
	var clauses []triggers.Condition
	if err := yaml.Unmarshal([]byte(Trigger()), &clauses); err != nil {
		t.Fatal(err)
	}
	engine, err := triggers.NewService(map[string][]triggers.Condition{"diagnose": clauses})
	if err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	now := created.Add(time.Second)
	app := map[string]any{
		"metadata": map[string]any{"uid": "app-uid", "creationTimestamp": created.Format(time.RFC3339)},
		"status": map[string]any{
			"health": map[string]any{"status": "Degraded", "lastTransitionTime": created.Format(time.RFC3339)},
			"sync":   map[string]any{"revisions": []any{"a", "b"}},
		},
	}
	vars := map[string]any{"app": app, "time": map[string]any{"Now": func() time.Time { return now }}}
	for i, health := range []string{"Degraded", "Unknown", "Missing", "Progressing", "Healthy", "Suspended"} {
		app["status"].(map[string]any)["health"].(map[string]any)["status"] = health
		result, err := engine.Run("diagnose", vars)
		if err != nil {
			t.Fatal(err)
		}
		if len(result) != len(clauses) || !result[i].Triggered || result[i].OncePer == "" {
			t.Fatalf("%s was not delivered immediately: %+v", health, result)
		}
		for j, clause := range result {
			if j != i && clause.Triggered {
				t.Fatalf("%s also triggered clause %d", health, j)
			}
		}
	}
	app["status"].(map[string]any)["health"].(map[string]any)["status"] = "Degraded"
	first, err := engine.Run("diagnose", vars)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	retry, err := engine.Run("diagnose", vars)
	if err != nil {
		t.Fatal(err)
	}
	if !retry[0].Triggered || retry[0].OncePer == first[0].OncePer {
		t.Fatal("a later notification evaluation cannot redeliver")
	}
}

func TestMergeSubscription(t *testing.T) {
	original := "- recipients: [slack:ops, gitops-ai-controller]\n  triggers: [on-sync-failed, on-gitops-ai-diagnose]\n  selector: team=platform\n"
	enabled, err := MergeSubscription(original, true)
	if err != nil {
		t.Fatal(err)
	}
	again, err := MergeSubscription(enabled, true)
	if err != nil || again != enabled {
		t.Fatal("merge not idempotent")
	}
	disabled, err := MergeSubscription(enabled, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(disabled, "slack:ops") || !strings.Contains(disabled, "on-sync-failed") || !strings.Contains(disabled, "team=platform") {
		t.Fatal("user configuration lost")
	}
	for _, invalid := range []string{"not: a-list", "- recipients: slack:ops", "[broken"} {
		if _, err := MergeSubscription(invalid, true); err == nil {
			t.Fatalf("accepted %q", invalid)
		}
	}
}
