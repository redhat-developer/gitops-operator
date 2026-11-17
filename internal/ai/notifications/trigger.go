package notifications

import (
	"fmt"
	"strings"

	"sigs.k8s.io/yaml"
)

// Trigger returns the YAML definition for the on-gitops-ai-diagnose notification trigger.
// The epoch-based oncePer key allows redelivery when Notifications re-evaluates an
// Application after a lost webhook, without scheduling that evaluation itself.
func Trigger() string {
	once := "  oncePer: app.metadata.uid + ':' + toJSON(app.status.sync.revisions ?? [app.status.sync.revision ?? '']) + ':' + (app.status.health.lastTransitionTime ?? '') + ':' + string(int(time.Now().Unix() / 60))\n"
	var b strings.Builder
	for _, h := range []string{"Degraded", "Unknown", "Missing", "Progressing", "Healthy", "Suspended"} {
		fmt.Fprintf(&b, "- when: app.status.health.status == '%s'\n", h)
		b.WriteString(once + "  send: [gitops-ai-diagnose]\n")
	}
	return b.String()
}

// MergeSubscription idempotently inserts or removes the gitops-ai-controller recipient/trigger
// pair from a YAML subscription list, preserving all user-managed entries.
// It strips any gitops-ai-controller recipient and on-gitops-ai-diagnose trigger from every
// entry, then appends a standalone agentic entry when enabled.
func MergeSubscription(value string, enabled bool) (string, error) {
	var entries []map[string]any
	if err := yaml.UnmarshalStrict([]byte(value), &entries); err != nil {
		return "", fmt.Errorf("invalid notification subscriptions: %w", err)
	}
	var result []map[string]any
	for _, entry := range entries {
		recipients, rok := entry["recipients"].([]any)
		if !rok {
			return "", fmt.Errorf("subscription recipients must be a list")
		}
		triggers, tok := entry["triggers"].([]any)
		if entry["triggers"] != nil && !tok {
			return "", fmt.Errorf("subscription triggers must be a list")
		}
		var filteredRecipients, filteredTriggers []any
		for _, r := range recipients {
			if _, ok := r.(string); !ok {
				return "", fmt.Errorf("subscription recipient must be a string")
			}
			if r != ServiceName {
				filteredRecipients = append(filteredRecipients, r)
			}
		}
		for _, t := range triggers {
			if _, ok := t.(string); !ok {
				return "", fmt.Errorf("subscription trigger must be a string")
			}
			if t != "on-gitops-ai-diagnose" {
				filteredTriggers = append(filteredTriggers, t)
			}
		}
		if len(filteredRecipients) == 0 {
			continue
		}
		cp := map[string]any{}
		for k, v := range entry {
			cp[k] = v
		}
		cp["recipients"] = filteredRecipients
		if len(filteredTriggers) > 0 {
			cp["triggers"] = filteredTriggers
		} else {
			delete(cp, "triggers")
		}
		result = append(result, cp)
	}
	if enabled {
		result = append(result, map[string]any{"recipients": []any{ServiceName}, "triggers": []any{"on-gitops-ai-diagnose"}})
	}
	if len(result) == 0 {
		return "", nil
	}
	b, err := yaml.Marshal(result)
	return string(b), err
}
