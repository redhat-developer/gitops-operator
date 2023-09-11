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
	"testing"

	argoapp "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	"gotest.tools/assert"
	v1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/apimachinery/pkg/api/resource"
)

func TestArgoCD(t *testing.T) {
	testArgoCD, _ := NewCR("openshift-gitops", "openshift-gitops")

	testApplicationSetResources := &v1.ResourceRequirements{
		Requests: v1.ResourceList{
			v1.ResourceMemory: resourcev1.MustParse("512Mi"),
			v1.ResourceCPU:    resourcev1.MustParse("250m"),
		},
		Limits: v1.ResourceList{
			v1.ResourceMemory: resourcev1.MustParse("1024Mi"),
			v1.ResourceCPU:    resourcev1.MustParse("2000m"),
		},
	}
	assert.DeepEqual(t, testArgoCD.Spec.ApplicationSet.Resources, testApplicationSetResources)

	testControllerResources := &v1.ResourceRequirements{
		Requests: v1.ResourceList{
			v1.ResourceMemory: resourcev1.MustParse("1024Mi"),
			v1.ResourceCPU:    resourcev1.MustParse("250m"),
		},
		Limits: v1.ResourceList{
			v1.ResourceMemory: resourcev1.MustParse("2048Mi"),
			v1.ResourceCPU:    resourcev1.MustParse("2000m"),
		},
	}
	assert.DeepEqual(t, testArgoCD.Spec.Controller.Resources, testControllerResources)

	testDexResources := &v1.ResourceRequirements{
		Requests: v1.ResourceList{
			v1.ResourceMemory: resourcev1.MustParse("128Mi"),
			v1.ResourceCPU:    resourcev1.MustParse("250m"),
		},
		Limits: v1.ResourceList{
			v1.ResourceMemory: resourcev1.MustParse("256Mi"),
			v1.ResourceCPU:    resourcev1.MustParse("500m"),
		},
	}
	assert.DeepEqual(t, testArgoCD.Spec.SSO.Dex.Resources, testDexResources)

	testGrafanaResources := &v1.ResourceRequirements{
		Requests: v1.ResourceList{
			v1.ResourceMemory: resourcev1.MustParse("128Mi"),
			v1.ResourceCPU:    resourcev1.MustParse("250m"),
		},
		Limits: v1.ResourceList{
			v1.ResourceMemory: resourcev1.MustParse("256Mi"),
			v1.ResourceCPU:    resourcev1.MustParse("500m"),
		},
	}
	assert.DeepEqual(t, testArgoCD.Spec.Grafana.Resources, testGrafanaResources)

	testHAResources := &v1.ResourceRequirements{
		Requests: v1.ResourceList{
			v1.ResourceMemory: resourcev1.MustParse("128Mi"),
			v1.ResourceCPU:    resourcev1.MustParse("250m"),
		},
		Limits: v1.ResourceList{
			v1.ResourceMemory: resourcev1.MustParse("256Mi"),
			v1.ResourceCPU:    resourcev1.MustParse("500m"),
		},
	}
	assert.DeepEqual(t, testArgoCD.Spec.HA.Resources, testHAResources)
	assert.Equal(t, testArgoCD.Spec.HA.Enabled, false)

	testRedisResources := &v1.ResourceRequirements{
		Requests: v1.ResourceList{
			v1.ResourceMemory: resourcev1.MustParse("128Mi"),
			v1.ResourceCPU:    resourcev1.MustParse("250m"),
		},
		Limits: v1.ResourceList{
			v1.ResourceMemory: resourcev1.MustParse("256Mi"),
			v1.ResourceCPU:    resourcev1.MustParse("500m"),
		},
	}
	assert.DeepEqual(t, testArgoCD.Spec.Redis.Resources, testRedisResources)

	testRepoResources := &v1.ResourceRequirements{
		Requests: v1.ResourceList{
			v1.ResourceMemory: resourcev1.MustParse("256Mi"),
			v1.ResourceCPU:    resourcev1.MustParse("250m"),
		},
		Limits: v1.ResourceList{
			v1.ResourceMemory: resourcev1.MustParse("1024Mi"),
			v1.ResourceCPU:    resourcev1.MustParse("1000m"),
		},
	}
	assert.DeepEqual(t, testArgoCD.Spec.Repo.Resources, testRepoResources)

	testServerResources := &v1.ResourceRequirements{
		Requests: v1.ResourceList{
			v1.ResourceMemory: resourcev1.MustParse("128Mi"),
			v1.ResourceCPU:    resourcev1.MustParse("125m"),
		},
		Limits: v1.ResourceList{
			v1.ResourceMemory: resourcev1.MustParse("256Mi"),
			v1.ResourceCPU:    resourcev1.MustParse("500m"),
		},
	}
	assert.DeepEqual(t, testArgoCD.Spec.Server.Resources, testServerResources)
}

func TestDexConfiguration(t *testing.T) {
	testArgoCD, _ := NewCR("openshift-gitops", "openshift-gitops")

	// Verify Dex OpenShift Configuration
	assert.Equal(t, testArgoCD.Spec.SSO.Dex.OpenShiftOAuth, true)

	// Verify the default RBAC
	testAdminPolicy := "g, system:cluster-admins, role:admin\ng, cluster-admins, role:admin\n"
	testDefaultScope := "[groups]"
	testDefaultArgoCDRole := ""
	testRBAC := argoapp.ArgoCDRBACSpec{
		Policy:        &testAdminPolicy,
		Scopes:        &testDefaultScope,
		DefaultPolicy: &testDefaultArgoCDRole,
	}
	assert.DeepEqual(t, testArgoCD.Spec.RBAC, testRBAC)
}
