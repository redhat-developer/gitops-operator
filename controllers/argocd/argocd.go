/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package argocd

import (
	argoapp "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	v1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/yaml"
)

var (
	defaultAdminPolicy = "g, system:cluster-admins, role:admin\ng, cluster-admins, role:admin\n"
	defaultScope       = "[groups]"

	//The policy.default property in the argocd-rbac-cm ConfigMap.
	defaultArgoCDRole = ""
)

// resource exclusions for the ArgoCD CR.
type resource struct {
	APIGroups []string `json:"apiGroups"`
	Kinds     []string `json:"kinds"`
	Clusters  []string `json:"clusters"`
}

func getArgoApplicationSetSpec() *argoapp.ArgoCDApplicationSet {
	return &argoapp.ArgoCDApplicationSet{
		Resources: &v1.ResourceRequirements{
			Requests: v1.ResourceList{
				v1.ResourceMemory: resourcev1.MustParse("512Mi"),
				v1.ResourceCPU:    resourcev1.MustParse("250m"),
			},
			Limits: v1.ResourceList{
				v1.ResourceMemory: resourcev1.MustParse("1024Mi"),
				v1.ResourceCPU:    resourcev1.MustParse("2000m"),
			},
		},
	}
}

func getArgoControllerSpec() argoapp.ArgoCDApplicationControllerSpec {
	return argoapp.ArgoCDApplicationControllerSpec{
		Resources: &v1.ResourceRequirements{
			Requests: v1.ResourceList{
				v1.ResourceMemory: resourcev1.MustParse("1024Mi"),
				v1.ResourceCPU:    resourcev1.MustParse("250m"),
			},
			Limits: v1.ResourceList{
				v1.ResourceMemory: resourcev1.MustParse("2048Mi"),
				v1.ResourceCPU:    resourcev1.MustParse("2000m"),
			},
		},
	}
}

func getArgoDexSpec() *argoapp.ArgoCDDexSpec {
	return &argoapp.ArgoCDDexSpec{
		OpenShiftOAuth: true,
		Resources: &v1.ResourceRequirements{
			Requests: v1.ResourceList{
				v1.ResourceMemory: resourcev1.MustParse("128Mi"),
				v1.ResourceCPU:    resourcev1.MustParse("250m"),
			},
			Limits: v1.ResourceList{
				v1.ResourceMemory: resourcev1.MustParse("256Mi"),
				v1.ResourceCPU:    resourcev1.MustParse("500m"),
			},
		},
	}
}

func getArgoSSOSpec() *argoapp.ArgoCDSSOSpec {
	return &argoapp.ArgoCDSSOSpec{
		Provider: argoapp.SSOProviderTypeDex,
		Dex:      getArgoDexSpec(),
	}
}

func getArgoGrafanaSpec() argoapp.ArgoCDGrafanaSpec {
	return argoapp.ArgoCDGrafanaSpec{
		Resources: &v1.ResourceRequirements{
			Requests: v1.ResourceList{
				v1.ResourceMemory: resourcev1.MustParse("128Mi"),
				v1.ResourceCPU:    resourcev1.MustParse("250m"),
			},
			Limits: v1.ResourceList{
				v1.ResourceMemory: resourcev1.MustParse("256Mi"),
				v1.ResourceCPU:    resourcev1.MustParse("500m"),
			},
		},
	}
}

func getArgoHASpec() argoapp.ArgoCDHASpec {
	return argoapp.ArgoCDHASpec{
		Enabled: false,
		Resources: &v1.ResourceRequirements{
			Requests: v1.ResourceList{
				v1.ResourceMemory: resourcev1.MustParse("128Mi"),
				v1.ResourceCPU:    resourcev1.MustParse("250m"),
			},
			Limits: v1.ResourceList{
				v1.ResourceMemory: resourcev1.MustParse("256Mi"),
				v1.ResourceCPU:    resourcev1.MustParse("500m"),
			},
		},
	}
}

func getArgoRedisSpec() argoapp.ArgoCDRedisSpec {
	return argoapp.ArgoCDRedisSpec{
		Resources: &v1.ResourceRequirements{
			Requests: v1.ResourceList{
				v1.ResourceMemory: resourcev1.MustParse("128Mi"),
				v1.ResourceCPU:    resourcev1.MustParse("250m"),
			},
			Limits: v1.ResourceList{
				v1.ResourceMemory: resourcev1.MustParse("256Mi"),
				v1.ResourceCPU:    resourcev1.MustParse("500m"),
			},
		},
	}
}

func getArgoRepoServerSpec() argoapp.ArgoCDRepoSpec {
	return argoapp.ArgoCDRepoSpec{
		Resources: &v1.ResourceRequirements{
			Requests: v1.ResourceList{
				v1.ResourceMemory: resourcev1.MustParse("256Mi"),
				v1.ResourceCPU:    resourcev1.MustParse("250m"),
			},
			Limits: v1.ResourceList{
				v1.ResourceMemory: resourcev1.MustParse("1024Mi"),
				v1.ResourceCPU:    resourcev1.MustParse("1000m"),
			},
		},
	}
}

func getArgoServerSpec() argoapp.ArgoCDServerSpec {
	return argoapp.ArgoCDServerSpec{
		Route: argoapp.ArgoCDRouteSpec{Enabled: true},
		Resources: &v1.ResourceRequirements{
			Requests: v1.ResourceList{
				v1.ResourceMemory: resourcev1.MustParse("128Mi"),
				v1.ResourceCPU:    resourcev1.MustParse("125m"),
			},
			Limits: v1.ResourceList{
				v1.ResourceMemory: resourcev1.MustParse("256Mi"),
				v1.ResourceCPU:    resourcev1.MustParse("500m"),
			},
		},
	}
}

func getDefaultRBAC() argoapp.ArgoCDRBACSpec {
	return argoapp.ArgoCDRBACSpec{
		Policy:        &defaultAdminPolicy,
		Scopes:        &defaultScope,
		DefaultPolicy: &defaultArgoCDRole,
	}
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
			ApplicationSet:     getArgoApplicationSetSpec(),
			Controller:         getArgoControllerSpec(),
			SSO:                getArgoSSOSpec(),
			Grafana:            getArgoGrafanaSpec(),
			HA:                 getArgoHASpec(),
			Redis:              getArgoRedisSpec(),
			Repo:               getArgoRepoServerSpec(),
			Server:             getArgoServerSpec(),
			RBAC:               getDefaultRBAC(),
			ResourceExclusions: string(b),
		},
	}, nil
}
