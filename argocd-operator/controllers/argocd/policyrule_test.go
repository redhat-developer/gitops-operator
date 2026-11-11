package argocd

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	argoproj "github.com/redhat-developer/gitops-operator/argocd-operator/api/v1beta1"
	"github.com/redhat-developer/gitops-operator/argocd-operator/common"
)

func hasPodsExecRule(rules []v1.PolicyRule) bool {
	for _, r := range rules {
		if len(r.Resources) == 1 &&
			r.Resources[0] == "pods/exec" {
			return true
		}
	}
	return false
}

func hasServiceAccountRules(rules []v1.PolicyRule) bool {
	for _, r := range rules {
		expectedVerbs := []string{"get", "list", "impersonate"}
		if len(r.Resources) == 1 && r.Resources[0] == "serviceaccounts" {
			return slices.Equal(r.Verbs, expectedVerbs)
		}
	}
	return false
}

func TestPolicyRuleForServer_WebTerminal(t *testing.T) {
	webTerminalEnabled := true
	tests := []struct {
		name string
		cr   *argoproj.ArgoCD
		want bool
	}{
		{
			name: "default",
			cr: &argoproj.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "example",
					Namespace: "default",
				},
				Spec: argoproj.ArgoCDSpec{},
			},
			want: false,
		},
		{
			name: "disabled",
			cr:   &argoproj.ArgoCD{},
			want: false,
		},
		{
			name: "enabled",
			cr: &argoproj.ArgoCD{
				Spec: argoproj.ArgoCDSpec{
					WebTerminalEnabled: &webTerminalEnabled,
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rules := policyRuleForServer(tt.cr)
			assert.Equal(t, tt.want, hasPodsExecRule(rules))
		})
	}
}

func TestPolicyRuleForServer_Impersonation(t *testing.T) {
	tests := []struct {
		name string
		cr   *argoproj.ArgoCD
		want bool
	}{
		{
			name: "default",
			cr:   &argoproj.ArgoCD{},
			want: false,
		},
		{
			name: "impersonation enabled",
			cr: &argoproj.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Spec: argoproj.ArgoCDSpec{
					ExtraConfig: map[string]string{
						common.ArgoCDImpersonationEnabledCMKey: "true",
					},
				},
			},
			want: true,
		},
		{
			name: "impersonation disabled",
			cr: &argoproj.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Spec: argoproj.ArgoCDSpec{
					ExtraConfig: map[string]string{
						common.ArgoCDImpersonationEnabledCMKey: "false",
					},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			namespacedRules := policyRuleForServer(tt.cr)
			clusterRules := policyRuleForServerClusterRole(tt.cr)
			assert.Equal(t, tt.want, hasServiceAccountRules(namespacedRules))
			assert.Equal(t, tt.want, hasServiceAccountRules(clusterRules))
		})
	}
}
