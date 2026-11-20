package controller

import (
	"os"
	"testing"

	"github.com/redhat-developer/gitops-operator/internal/ai/utils"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestConfiguredSkillsPathsReachAgenticRun(t *testing.T) {
	defaultPaths := []string{
		"/skills/gitops/argo-cluster-debug",
		"/skills/gitops/argo-knowledge",
	}
	for _, tt := range []struct {
		name  string
		value string
		unset bool
		want  []string
	}{
		{name: "unset retains default skills", unset: true, want: defaultPaths},
		{name: "empty retains default skills", want: defaultPaths},
		{name: "blank list retains default skills", value: " , \t, , ", want: defaultPaths},
		{name: "custom skills replace defaults", value: "/custom/triage,/custom/operator", want: []string{"/custom/triage", "/custom/operator"}},
		{name: "trim whitespace and ignore empty entries", value: " /custom/triage, ,\t/custom/operator ,", want: []string{"/custom/triage", "/custom/operator"}},
		{name: "single skill", value: "/custom/triage", want: []string{"/custom/triage"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(utils.SkillsPathsEnvName, tt.value)
			if tt.unset {
				require.NoError(t, os.Unsetenv(utils.SkillsPathsEnvName))
			}
			t.Setenv("GITOPS_AI_SKILLS_IMAGE", "example.com/skills:custom")
			t.Setenv("GITOPS_AI_OPERATOR_NAMESPACE", testOperatorNamespace)
			t.Setenv("GITOPS_AI_AGENTIC_RUN_NAMESPACE", "lightspeed")
			t.Setenv("GITOPS_AI_DIAGNOSIS_COOLDOWN", "10m")
			t.Setenv("GITOPS_AI_DIAGNOSIS_UNHEALTHY_DURATION", "10m")
			t.Setenv("ARGOCD_LABEL_SELECTOR", "")
			cfg, err := LoadServiceConfig()
			require.NoError(t, err)
			s, _, _ := incidentFixture(t)
			s.config = cfg

			// Exercise diagnosis reconciliation, not just environment parsing:
			// the configured paths must reach the actual created AgenticRun.
			run := reconcileApp(t, s)
			require.NotNil(t, run)
			skills, found, err := unstructured.NestedSlice(run.Object, "spec", "tools", "skills")
			require.NoError(t, err)
			require.True(t, found)
			require.Len(t, skills, 1)
			skill := skills[0].(map[string]any)
			require.Equal(t, "example.com/skills:custom", skill["image"])
			paths, found, err := unstructured.NestedStringSlice(skill, "paths")
			require.NoError(t, err)
			require.True(t, found)
			require.Equal(t, tt.want, paths)
		})
	}
}
