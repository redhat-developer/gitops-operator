package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/redhat-developer/gitops-operator/internal/ai/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func (s *TriggerService) HandleDiagnose(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := s.logger.WithName("diagnose")

	token := extractBearerToken(r.Header.Get("Authorization"))
	if token == "" {
		writeJSON(w, http.StatusUnauthorized, DiagnoseResponse{Accepted: false, Reason: "missing authorization"})
		return
	}
	authResult := authenticateCaller(ctx, s.kubeClient, token, s.config.ArgocdNamespace, logger)
	if authResult == nil {
		writeJSON(w, http.StatusInternalServerError, DiagnoseResponse{Accepted: false, Reason: "authentication service unavailable"})
		return
	}
	if !authResult.Authenticated {
		writeJSON(w, http.StatusUnauthorized, DiagnoseResponse{Accepted: false, Reason: "invalid or expired token"})
		return
	}
	if !authResult.Authorized {
		writeJSON(w, http.StatusForbidden, DiagnoseResponse{Accepted: false, Reason: "caller not authorized"})
		return
	}
	var req DiagnoseRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, DiagnoseResponse{Accepted: false, Reason: "invalid request body"})
			return
		}
	}

	name := req.Application
	namespace := req.ApplicationNamespace
	if name == "" || namespace == "" {
		writeJSON(w, http.StatusBadRequest, DiagnoseResponse{Accepted: false, Reason: "application and applicationNamespace are required"})
		return
	}

	logger.Info("diagnose request received",
		"app", namespace+"/"+name,
		"trigger", triggerAutomatic,
	)

	if s.queue == nil || s.queue.ShuttingDown() {
		writeJSON(w, http.StatusServiceUnavailable, DiagnoseResponse{Reason: "diagnosis queue unavailable"})
		return
	}
	s.enqueueApplication(namespace, name)
	writeJSON(w, http.StatusAccepted, DiagnoseResponse{Accepted: true, Reason: "diagnosis queued"})
}

func (s *TriggerService) getApplication(ctx context.Context, namespace, name string) (*unstructured.Unstructured, error) {
	return s.dynClient.Resource(applicationGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
}

func (s *TriggerService) createAgenticRun(ctx context.Context, run *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	ns := run.GetNamespace()
	return s.dynClient.Resource(agenticRunGVR).Namespace(ns).Create(ctx, run, metav1.CreateOptions{})
}

func (s *TriggerService) getAgenticRun(ctx context.Context, namespace, name string) (*unstructured.Unstructured, error) {
	return s.dynClient.Resource(agenticRunGVR).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
}

func (s *TriggerService) listAgenticRuns(ctx context.Context, appName, appNamespace, appUID string) ([]*unstructured.Unstructured, error) {
	labelSelector := fmt.Sprintf(
		"%s=%s,%s=%s",
		utils.AppNamespace, appNamespace, utils.AppUID, appUID,
	)

	result, err := s.dynClient.Resource(agenticRunGVR).Namespace(s.config.RunNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, err
	}

	runs := make([]*unstructured.Unstructured, 0, len(result.Items))
	for i := range result.Items {
		if result.Items[i].GetLabels()[utils.ManagedBy] == utils.Manager && runBelongsToApplication(&result.Items[i], appName, appNamespace, appUID) {
			runs = append(runs, &result.Items[i])
		}
	}

	sort.Slice(runs, func(i, j int) bool {
		ti := runs[i].GetAnnotations()[utils.AppTriggeredAt]
		tj := runs[j].GetAnnotations()[utils.AppTriggeredAt]
		first, firstErr := time.Parse(time.RFC3339Nano, ti)
		second, secondErr := time.Parse(time.RFC3339Nano, tj)
		if firstErr == nil && secondErr == nil && !first.Equal(second) {
			return first.After(second)
		}
		return runs[i].GetCreationTimestamp().After(runs[j].GetCreationTimestamp().Time)
	})

	return runs, nil
}

type AgenticRunParams struct {
	AppName            string
	AppNamespace       string
	AppUID             string
	SyncStatus         string
	HealthStatus       string
	Revision           string
	SkillsImage        string
	Conditions         []AppCondition
	UnhealthyResources []UnhealthyResource
}

func buildAgenticRunObject(name, namespace string, p AgenticRunParams) *unstructured.Unstructured {
	requestText := fmt.Sprintf(
		"Diagnose this Red Hat OpenShift GitOps Application. Analysis only; do not change the cluster.\n\n"+
			"Application: %s\n"+
			"Namespace: %s\n"+
			"Sync status: %s\n"+
			"Health status: %s\n"+
			"%s\n"+
			"Report the root cause, affected resources, and a remediation through the ArgoCD custom resource or the Application Git source.\n",
		p.AppName,
		p.AppNamespace,
		p.SyncStatus,
		p.HealthStatus,
		buildConditionsContext(&p),
	)

	run := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "agentic.openshift.io/v1alpha1",
			"kind":       "AgenticRun",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
				"labels": map[string]any{
					utils.AppNamespace: p.AppNamespace,
					utils.AppUID:       p.AppUID,
					utils.ManagedBy:    utils.Manager,
				},
				"annotations": map[string]any{
					utils.AppName:        p.AppName,
					utils.AppNamespace:   p.AppNamespace,
					utils.AppSyncStatus:  p.SyncStatus,
					utils.AppHealthStatus: p.HealthStatus,
					utils.AppRevision:    p.Revision,
				},
			},
			"spec": buildAgenticRunSpec(requestText, p),
		},
	}

	return run
}

func buildAgenticRunSpec(requestText string, p AgenticRunParams) map[string]any {
	spec := map[string]any{
		"request":  requestText,
		"analysis": map[string]any{"agent": "default"},
	}
	if tools := buildToolsSpec(p.SkillsImage); len(tools) > 0 {
		spec["tools"] = tools
	}
	return spec
}

// Each path is one skill directory; the agent routes between them on its own.
// The triage pair covers the health statuses that make an Application eligible
// and the sync state reported alongside them, argo-operator covers the fixes
// that are operator inputs rather than the reconciled object, and
// argo-cluster-debug retains Rollouts and multi-instance debugging.
func buildToolsSpec(skillsImage string) map[string]any {
	tools := map[string]any{}
	if skillsImage != "" {
		tools["skills"] = []any{
			map[string]any{
				"image": skillsImage,
				"paths": []any{
					"/skills/gitops/argo-app-health-triage",
					"/skills/gitops/argo-app-sync-triage",
					"/skills/gitops/argo-operator",
					"/skills/gitops/argo-cluster-debug",
					"/skills/gitops/argo-knowledge",
				},
			},
		}
	}
	return tools
}

func extractConditions(app *unstructured.Unstructured) []AppCondition {
	raw, found, _ := unstructured.NestedSlice(app.Object, "status", "conditions")
	if !found {
		return nil
	}
	var conditions []AppCondition
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		t, _, _ := unstructured.NestedString(m, "type")
		msg, _, _ := unstructured.NestedString(m, "message")
		if t != "" {
			conditions = append(conditions, AppCondition{Type: t, Message: msg})
		}
	}
	return conditions
}

func extractUnhealthyResources(app *unstructured.Unstructured) []UnhealthyResource {
	raw, found, _ := unstructured.NestedSlice(app.Object, "status", "resources")
	if !found {
		return nil
	}
	var resources []UnhealthyResource
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		health, _, _ := unstructured.NestedString(m, "health", "status")
		sync, _, _ := unstructured.NestedString(m, "status")
		if health == "Degraded" || health == "Missing" || health == "Unknown" || sync == "OutOfSync" {
			msg, _, _ := unstructured.NestedString(m, "health", "message")
			resources = append(resources, UnhealthyResource{
				Group:         stringFromMap(m, "group"),
				Kind:          stringFromMap(m, "kind"),
				Namespace:     stringFromMap(m, "namespace"),
				Name:          stringFromMap(m, "name"),
				HealthStatus:  health,
				SyncStatus:    sync,
				HealthMessage: msg,
			})
		}
	}
	return resources
}

func stringFromMap(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

// Application names live in annotations because they may exceed label limits.
func runBelongsToApplication(run *unstructured.Unstructured, appName, appNamespace, appUID string) bool {
	labels := run.GetLabels()
	return run.GetAnnotations()[utils.AppName] == appName &&
		labels[utils.AppNamespace] == appNamespace &&
		labels[utils.AppUID] == appUID
}

func buildConditionsContext(p *AgenticRunParams) string {
	var sb strings.Builder

	// ArgoCD conditions (ComparisonError, SyncError, etc.)
	if len(p.Conditions) > 0 {
		sb.WriteString("\nReported conditions:\n")
		for _, c := range p.Conditions {
			sb.WriteString(fmt.Sprintf("  - [%s] %s\n", c.Type, c.Message))
		}
	}

	// Unhealthy/OutOfSync resources from status.resources[]
	if len(p.UnhealthyResources) > 0 {
		sb.WriteString("\nUnhealthy or OutOfSync resources reported by ArgoCD:\n")
		for _, r := range p.UnhealthyResources {
			sb.WriteString(fmt.Sprintf("  - %s/%s (%s) in %s — health: %s, sync: %s\n",
				r.Kind, r.Name, r.Group, r.Namespace, r.HealthStatus, r.SyncStatus))
			if r.HealthMessage != "" {
				sb.WriteString(fmt.Sprintf("    message: %s\n", r.HealthMessage))
			}
		}
	}

	return sb.String()
}

// diagnosisStateForRun reports the trigger's diagnosis state, not the
// AgenticRun's derived phase. Analysis completes this service's analysis-only
// runs even though the upstream phase may be Proposed or NoActionRequired.
func diagnosisStateForRun(run *unstructured.Unstructured) string {
	conditions, _, _ := unstructured.NestedSlice(run.Object, "status", "conditions")
	byType := map[string]map[string]any{}
	for _, raw := range conditions {
		c, _ := raw.(map[string]any)
		byType[stringFromMap(c, "type")] = c
	}
	// Terminal failures win even when earlier successful conditions remain.
	for _, key := range []string{"EmergencyStopped", "Denied", "Escalated"} {
		if stringFromMap(byType[key], "status") == "True" {
			return "failed"
		}
	}
	// Conditions accumulate. A completed diagnosis must beat an older
	// execution condition, regardless of their order in the status slice.
	for _, key := range []string{"Verified", "Analyzed"} {
		if stringFromMap(byType[key], "status") == "True" {
			return "completed"
		}
	}
	for _, key := range []string{"Escalated", "Verified", "Executed", "Analyzed"} {
		c, exists := byType[key]
		if !exists {
			continue
		}
		switch stringFromMap(c, "status") {
		case "True":
			return "running" // Executed=True: later steps may still be active.
		case "False":
			if key == "Verified" && stringFromMap(c, "reason") == "RetryingExecution" {
				return "running"
			}
			return "failed"
		default:
			return "running"
		}
	}
	return "running" // Unknown or absent conditions cannot justify another run.
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
