package operator

import (
	"context"
	"os"
	"testing"

	"github.com/redhat-developer/gitops-operator/internal/ai/utils"
	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestAgenticSkillsPathsEnvironmentReconcilesDeployment(t *testing.T) {
	cl := agenticOperatorFixture(t)
	t.Setenv(utils.SkillsPathsEnvName, "")
	if err := os.Unsetenv(utils.SkillsPathsEnvName); err != nil {
		t.Fatal(err)
	}
	t.Setenv(SkillsImageEnvName, "example.com/skills:custom")
	const defaults = "/skills/gitops/argo-cluster-debug,/skills/gitops/argo-knowledge"
	for _, step := range []struct {
		name  string
		set   bool
		value string
		want  string
	}{
		{name: "unset keeps existing skills", want: defaults},
		{name: "custom paths forwarded", set: true, value: "/skills/custom/health, /skills/custom/sync", want: "/skills/custom/health, /skills/custom/sync"},
		{name: "changed paths replace previous value", set: true, value: "/skills/team/diagnose", want: "/skills/team/diagnose"},
		{name: "empty value restores defaults", set: true, want: defaults},
	} {
		t.Run(step.name, func(t *testing.T) {
			if step.set {
				t.Setenv(utils.SkillsPathsEnvName, step.value)
			}
			doReconcile(t, cl, true, testServiceNamespace)
			deployment := &appsv1.Deployment{}
			if err := cl.Get(context.Background(), client.ObjectKey{Name: ServiceName, Namespace: testServiceNamespace}, deployment); err != nil {
				t.Fatal(err)
			}
			env := map[string]string{}
			for _, entry := range deployment.Spec.Template.Spec.Containers[0].Env {
				env[entry.Name] = entry.Value
			}
			if got := env[utils.SkillsPathsEnvName]; got != step.want {
				t.Fatalf("skills paths=%q, want %q", got, step.want)
			}
			if env[SkillsImageEnvName] != "example.com/skills:custom" || env["GITOPS_AI_OPERATOR_NAMESPACE"] != testServiceNamespace {
				t.Fatalf("skills path update changed other controller settings: %v", env)
			}
		})
	}
}
