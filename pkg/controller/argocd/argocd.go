package argocd

import (
	argoapp "github.com/argoproj-labs/argocd-operator/pkg/apis/argoproj/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/yaml"
)

// resource exclusions for the ArgoCD CR.
type resource struct {
	APIGroups []string `json:"apiGroups"`
	Kinds     []string `json:"kinds"`
	Clusters  []string `json:"clusters"`
}

// NewCR returns an ArgoCD reference optimized for use in OpenShift
// with Tekton
func NewCR(name, ns string) (*argoapp.ArgoCD, error) {
	b, err := yaml.Marshal([]resource{
		{
			APIGroups: []string{"tekton.dev"},
			Kinds:     []string{"TaskRun", "PipelineRun"},
			Clusters:  []string{"*"},
		},
	})
	if err != nil {
		return nil, err
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
			ApplicationSet: &argoapp.ArgoCDApplicationSet{},
		},
	}, nil
}
