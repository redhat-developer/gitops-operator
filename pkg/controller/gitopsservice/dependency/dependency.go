package dependency

import (
	"context"
	"time"

	argoapp "github.com/argoproj-labs/argocd-operator/pkg/apis/argoproj/v1alpha1"
	"github.com/go-logr/logr"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	argocdSubName   = "argocd-operator"
	argocdGroupName = "argocd-operator-group"
)

var log = logf.Log.WithName("gitops_dependencies")

// Dependency represents an instance of GitOps dependency
type Dependency struct {
	client  client.Client
	timeout time.Duration
	isReady wait.ConditionFunc
	log     logr.Logger
}

// resource exclusions for the ArgoCD CR.
type resource struct {
	APIGroups []string `json:"apiGroups"`
	Kinds     []string `json:"kinds"`
	Clusters  []string `json:"clusters"`
}

// NewClient create a new instance of GitOps dependencies
func NewClient(client client.Client, timeout time.Duration) *Dependency {
	return &Dependency{
		client:  client,
		timeout: timeout,
		log:     log.WithName("GitOps Dependencies"),
	}
}

func (d *Dependency) createResourceIfAbsent(ctx context.Context, obj runtime.Object, ns types.NamespacedName) error {
	err := d.client.Get(ctx, ns, obj)
	if err != nil {
		switch errors.ReasonForError(err) {
		case metav1.StatusReasonNotFound:
			err = d.client.Create(ctx, obj)
			if err != nil {
				d.log.Error(err, "Unable to create resource", "Resource.Kind", obj.GetObjectKind(), "Resource.Name", ns.
					Name)
				return err
			}
			d.log.Info("Successfully created resource", "Resource.Kind", obj.GetObjectKind(), "Resource.Name", ns.Name, "Resource.Namespace", ns.
				Namespace)
		case metav1.StatusReasonAlreadyExists:
			d.log.Info("Resource already exists", "Resource.Kind", obj.GetObjectKind(), "Resource.Name", ns.Name)
		default:
			return err
		}
	}
	return nil
}

func newNamespace(name string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

func addPrefixIfNecessary(prefix, name string) string {
	if prefix != "" {
		return prefix + "-" + name
	}
	return name
}

func argoCDCR(ns string) (runtime.Object, string, error) {
	name := "argocd"
	b, err := yaml.Marshal([]resource{
		{
			APIGroups: []string{"tekton.dev"},
			Kinds:     []string{"TaskRun", "PipelineRun"},
			Clusters:  []string{"*"},
		},
	})
	if err != nil {
		return nil, "", err
	}
	return &argoapp.ArgoCD{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ArgoCD",
			APIVersion: "argoproj.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: argoapp.ArgoCDSpec{
			ResourceExclusions: string(b),
			Server: argoapp.ArgoCDServerSpec{
				Route: argoapp.ArgoCDRouteSpec{Enabled: true},
			},
		},
	}, name, nil
}
