package controller

import (
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/workqueue"
)

var (
	applicationGVR = schema.GroupVersionResource{
		Group:    "argoproj.io",
		Version:  "v1alpha1",
		Resource: "applications",
	}
	agenticRunGVR = schema.GroupVersionResource{
		Group:    "agentic.openshift.io",
		Version:  "v1alpha1",
		Resource: "agenticruns",
	}
)

// ServiceConfig holds runtime configuration for the AI controller service.
type ServiceConfig struct {
	RunNamespace             string
	Cooldown                 time.Duration
	SkillsImage              string
	ArgocdNamespace          string
	AutoDiagnosisGracePeriod time.Duration
	MaxAttempts              int
}

// graceEntry records when the controller first observed an app as qualifying for
// diagnosis. Keyed by "namespace/name".
type graceEntry struct {
	start    time.Time
	revision string
}

// TriggerService is the HTTP service that receives ArgoCD notification
// webhooks and drives AgenticRun lifecycle management.
type TriggerService struct {
	kubeClient   kubernetes.Interface
	dynClient    dynamic.Interface
	config       ServiceConfig
	logger       logr.Logger
	now          func() time.Time
	queue        workqueue.TypedRateLimitingInterface[string]
	graceStarted map[string]graceEntry // accessed only from the queue worker goroutine
}

type DiagnoseRequest struct {
	Application          string `json:"application"`
	ApplicationNamespace string `json:"applicationNamespace"`
}

type DiagnoseResponse struct {
	Accepted bool   `json:"accepted"`
	Reason   string `json:"reason,omitempty"`
}

type AppCondition struct {
	Type    string
	Message string
}

type UnhealthyResource struct {
	Group         string
	Kind          string
	Namespace     string
	Name          string
	HealthStatus  string
	SyncStatus    string
	HealthMessage string
}
