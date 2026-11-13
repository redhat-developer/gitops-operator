package controller

import (
	"testing"

	"github.com/redhat-developer/gitops-operator/internal/ai/utils"
)

func TestBuildAgenticRunObject_LabelsPreserved(t *testing.T) {
	obj := buildAgenticRunObject("diagnosis", "openshift-lightspeed", AgenticRunParams{
		AppName:      "my-app",
		AppNamespace: "openshift-gitops",
		AppUID:       "uid-123",
		SyncStatus:   "OutOfSync",
		HealthStatus: "Degraded",
	})

	labels := obj.GetLabels()
	expectedLabels := map[string]string{
		"gitops.openshift.io/application-namespace": "openshift-gitops",
		"gitops.openshift.io/application-uid":       "uid-123",
		"gitops.openshift.io/managed-by":            "gitops-ai-controller",
	}
	if obj.GetAnnotations()["gitops.openshift.io/application-name"] != "my-app" {
		t.Fatal("full name annotation missing")
	}
	for k, v := range expectedLabels {
		if labels[k] != v {
			t.Errorf("label %q: expected %q, got %v", k, v, labels[k])
		}
	}
}

func TestBuildAgenticRunObject_AnnotationsPreserved(t *testing.T) {
	obj := buildAgenticRunObject("diagnosis", "openshift-lightspeed", AgenticRunParams{
		AppName:      "my-app",
		AppNamespace: "openshift-gitops",
		SyncStatus:   "OutOfSync",
		HealthStatus: "Degraded",
		Revision:     "abc123",
	})

	annotations := obj.GetAnnotations()
	if annotations[utils.AppSyncStatus] != "OutOfSync" {
		t.Errorf("sync-status annotation wrong: %v", annotations[utils.AppSyncStatus])
	}
	if annotations[utils.AppHealthStatus] != "Degraded" {
		t.Errorf("health-status annotation wrong: %v", annotations[utils.AppHealthStatus])
	}
	if annotations[utils.AppRevision] != "abc123" {
		t.Errorf("revision annotation wrong: %v", annotations[utils.AppRevision])
	}
	if annotations[utils.AppNamespace] != "openshift-gitops" {
		t.Errorf("namespace annotation wrong: %v", annotations[utils.AppNamespace])
	}
}

