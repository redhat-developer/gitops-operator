package ai

import (
	"fmt"
	"strings"
)

// NotificationTrigger returns the YAML definition for the on-gitops-ai-diagnose notification trigger.
// The epoch-based oncePer key allows redelivery when Notifications re-evaluates an
// Application after a lost webhook, without scheduling that evaluation itself.
func NotificationTrigger() string {
	once := "  oncePer: app.metadata.uid + ':' + toJSON(app.status.sync.revisions ?? [app.status.sync.revision ?? '']) + ':' + (app.status.health.lastTransitionTime ?? '') + ':' + string(int(time.Now().Unix() / 60))\n"
	var b strings.Builder
	for _, h := range []string{"Degraded", "Unknown", "Missing", "Progressing", "Healthy", "Suspended"} {
		fmt.Fprintf(&b, "- when: app.status.health.status == '%s'\n", h)
		b.WriteString(once)
		b.WriteString("  send: [gitops-ai-diagnose]\n")
	}
	return b.String()
}
