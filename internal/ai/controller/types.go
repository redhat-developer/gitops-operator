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
	// argocdGVR resolves the operator-managed ArgoCD instance that owns a
	// verified caller's namespace. v1beta1 is the only served version.
	argocdGVR = schema.GroupVersionResource{
		Group:    "argoproj.io",
		Version:  "v1beta1",
		Resource: "argocds",
	}
)

// ServiceConfig holds runtime configuration for the AI controller service.
type ServiceConfig struct {
	RunNamespace             string
	Cooldown                 time.Duration
	SkillsImage              string
	SkillsPaths              []string
	OperatorNamespace        string
	ArgoCDLabelSelector      string
	AutoDiagnosisGracePeriod time.Duration
	MaxAttempts              int
}

// graceEntry records when the controller first observed an app as qualifying for
// diagnosis. Keyed by "namespace/name/instanceUID" and bound to the Application
// UID so a recreated Application cannot inherit the previous object's timer.
type graceEntry struct {
	start    time.Time
	appUID   string
	revision string
}

// recoveryEntry tracks Healthy observations when ArgoCD supplies no usable
// transition timestamp. Like the diagnosis timer, it belongs to one app/revision.
type recoveryEntry struct {
	start    time.Time
	appUID   string
	revision string
}

// TriggerService is the HTTP service that receives ArgoCD notification
// webhooks and drives AgenticRun lifecycle management.
type TriggerService struct {
	kubeClient      kubernetes.Interface
	dynClient       dynamic.Interface
	config          ServiceConfig
	logger          logr.Logger
	now             func() time.Time
	queue           workqueue.TypedRateLimitingInterface[string]
	graceStarted    map[string]graceEntry    // accessed only from the queue worker goroutine
	recoveryStarted map[string]recoveryEntry // accessed only from the queue worker goroutine
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
