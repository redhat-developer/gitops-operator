// Copyright 2026 ArgoCD Operator Developers
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package gitopspromoter

import (
	"fmt"
	"os"

	rbacv1 "k8s.io/api/rbac/v1"

	argoproj "github.com/redhat-developer/gitops-operator/argocd-operator/api/v1beta1"
	"github.com/redhat-developer/gitops-operator/argocd-operator/common"
)

// policyRuleConfig provides relevant data for Cluster Roles and Roles that need to be created
type policyRuleConfig struct {
	name        string
	roleRefName string
	policyRule  []rbacv1.PolicyRule
	// skipCreateWhenEmpty indicates an empty policyRule means "use a custom/external role"
	// (do not create). When false, an empty policyRule with enabled=false deletes the role.
	skipCreateWhenEmpty bool
	// enabled controls whether the ClusterRole/Binding should exist. Defaults to true when unset
	// by callers that leave this falsey only when intentionally disabling a companion role.
	enabled bool
}

// buildPolicyRulesForControllerClusterRoles creates a list of configs for the ClusterRoles that are needed by the controller
// The default ClusterRule can be replaced by your own custom ClusterRole if the env variable is set
func buildPolicyRulesForControllerClusterRoles(compName string, cr *argoproj.ArgoCD) []policyRuleConfig {
	name := os.Getenv(common.GitOpsPromoterControllerClusterRoleEnvName)
	policyRule := []rbacv1.PolicyRule{}
	if name == "" {
		name = generatePromoterResourceNameWithNamespace(compName, cr)
		policyRule = buildPolicyRuleForControllerClusterRole()
	}

	configs := []policyRuleConfig{
		{
			name:                generatePromoterResourceNameWithNamespace(compName, cr),
			roleRefName:         name,
			policyRule:          policyRule,
			skipCreateWhenEmpty: true,
			enabled:             true,
		},
	}

	// Extra ClusterRole for built-in apiserver serving-cert rotation (APIService caBundle +
	// serving-cert Secret update). Always included so disable paths can delete leftovers.
	certRotationName := generatePromoterAPIServerCertRotationClusterRoleName(compName, cr)
	certRotationEnabled := usesBuiltInAPIServerCerts(cr) && isAPIServerEnabled(cr)
	certRotationRules := []rbacv1.PolicyRule{}
	if certRotationEnabled {
		certRotationRules = buildPolicyRuleForControllerAPIServerCertRotation()
	}
	configs = append(configs, policyRuleConfig{
		name:        certRotationName,
		roleRefName: certRotationName,
		policyRule:  certRotationRules,
		enabled:     certRotationEnabled,
	})

	return configs
}

// generatePromoterAPIServerCertRotationClusterRoleName returns the ClusterRole/Binding name for
// built-in apiserver cert rotation RBAC.
func generatePromoterAPIServerCertRotationClusterRoleName(compName string, cr *argoproj.ArgoCD) string {
	return fmt.Sprintf("%s-%s", generatePromoterResourceNameWithNamespace(compName, cr), "apiserver-cert-rotation")
}

// buildPolicyRuleForControllerAPIServerCertRotation mirrors upstream
// config/apiserver/certs-builtin-controller/rbac.yaml permissions needed by cert-controller.
func buildPolicyRuleForControllerAPIServerCertRotation() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"secrets"},
			ResourceNames: []string{
				DefaultAPIServerServingCertSecretName,
			},
			Verbs: []string{"update"},
		},
		{
			APIGroups: []string{"apiregistration.k8s.io"},
			Resources: []string{"apiservices"},
			Verbs:     []string{"get", "list", "watch"},
		},
		{
			APIGroups: []string{"apiregistration.k8s.io"},
			Resources: []string{"apiservices"},
			ResourceNames: []string{
				DefaultAPIServerAPIServiceName,
			},
			Verbs: []string{"update"},
		},
	}
}

// buildPolicyRulesForControllerClusterRoles creates a list of configs for the ClusterRoles that are needed by the api server
// The default ClusterRule can be replaced by your own custom ClusterRole if the env variable is set
func buildPolicyRulesForAPIServerClusterRoles(compName string, cr *argoproj.ArgoCD) []policyRuleConfig {
	name := os.Getenv(common.GitOpsPromoterAPIServerClusterRoleEnvName)
	policyRule := []rbacv1.PolicyRule{}
	if name == "" {
		name = generatePromoterResourceNameWithNamespace(compName, cr)
		policyRule = buildPolicyRuleForAPIServerClusterRole()
	}

	return []policyRuleConfig{
		{
			name:                generatePromoterResourceNameWithNamespace(compName, cr),
			roleRefName:         name,
			policyRule:          policyRule,
			skipCreateWhenEmpty: true,
			enabled:             true,
		},
		{
			name:        fmt.Sprintf("%s-%s", generatePromoterResourceNameWithNamespace(compName, cr), "promotionstrategydetails-viewer"),
			roleRefName: fmt.Sprintf("%s-%s", generatePromoterResourceNameWithNamespace(compName, cr), "promotionstrategydetails-viewer"),
			policyRule:  buildPolicyRuleForAPIServerPromotionStrategyDetailsViewer(),
			enabled:     true,
		},
	}
}

// buildPolicyRuleForControllerClusterRole creates the default policy rule for the Controller's cluster role.
// Rules mirror upstream gitops-promoter config/rbac/role.yaml for the pinned release.
func buildPolicyRuleForControllerClusterRole() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{
				"",
			},
			Resources: []string{
				"namespaces",
			},
			Verbs: []string{
				"get",
				"list",
				"watch",
			},
		},
		{
			APIGroups: []string{
				"",
			},
			Resources: []string{
				"secrets",
			},
			Verbs: []string{
				"get",
				"list",
				"patch",
				"watch",
			},
		},
		{
			APIGroups: []string{
				"",
				"events.k8s.io",
			},
			Resources: []string{
				"events",
			},
			Verbs: []string{
				"create",
				"patch",
			},
		},
		{
			APIGroups: []string{
				"argoproj.io",
			},
			Resources: []string{
				"applications",
			},
			Verbs: []string{
				"get",
				"list",
				"watch",
			},
		},
		{
			APIGroups: []string{
				"promoter.argoproj.io",
			},
			Resources: []string{
				"argocdcommitstatuses",
				"dependentssuccessfulcommitstatuses",
				"gitcommitstatuses",
				"promotionstrategies",
				"scheduledcommitstatuses",
				"timedcommitstatuses",
				"webrequestcommitstatuses",
			},
			Verbs: []string{
				"get",
				"list",
				"watch",
			},
		},
		{
			APIGroups: []string{
				"promoter.argoproj.io",
			},
			Resources: []string{
				"argocdcommitstatuses/finalizers",
				"changetransferpolicies/finalizers",
				"changetransferpolicyhistories/finalizers",
				"clusterscmproviders/finalizers",
				"dependentssuccessfulcommitstatuses/finalizers",
				"gitcommitstatuses/finalizers",
				"gitrepositories/finalizers",
				"promotionstrategies/finalizers",
				"pullrequests/finalizers",
				"scheduledcommitstatuses/finalizers",
				"scmproviders/finalizers",
				"timedcommitstatuses/finalizers",
				"webrequestcommitstatuses/finalizers",
			},
			Verbs: []string{
				"update",
			},
		},
		{
			APIGroups: []string{
				"promoter.argoproj.io",
			},
			Resources: []string{
				"argocdcommitstatuses/status",
				"changetransferpolicies/status",
				"changetransferpolicyhistories/status",
				"clusterscmproviders/status",
				"commitstatuses/status",
				"dependentssuccessfulcommitstatuses/status",
				"gitcommitstatuses/status",
				"gitrepositories/status",
				"promotionstrategies/status",
				"pullrequests/status",
				"restoreactivecommits/status",
				"scheduledcommitstatuses/status",
				"scmproviders/status",
				"timedcommitstatuses/status",
				"webrequestcommitstatuses/status",
			},
			Verbs: []string{
				"get",
				"patch",
				"update",
			},
		},
		{
			APIGroups: []string{
				"promoter.argoproj.io",
			},
			Resources: []string{
				"changetransferpolicies",
				"commitstatuses",
				"pullrequests",
				"restoreactivecommits",
			},
			Verbs: []string{
				"create",
				"delete",
				"get",
				"list",
				"patch",
				"update",
				"watch",
			},
		},
		{
			APIGroups: []string{
				"promoter.argoproj.io",
			},
			Resources: []string{
				"changetransferpolicyhistories",
			},
			Verbs: []string{
				"create",
				"delete",
				"get",
				"list",
				"patch",
				"watch",
			},
		},
		{
			APIGroups: []string{
				"promoter.argoproj.io",
			},
			Resources: []string{
				"clusterscmproviders",
				"gitrepositories",
				"scmproviders",
			},
			Verbs: []string{
				"get",
				"list",
				"update",
				"watch",
			},
		},
		{
			APIGroups: []string{
				"promoter.argoproj.io",
			},
			ResourceNames: []string{
				"promoter-controller-configuration",
			},
			Resources: []string{
				"controllerconfigurations",
			},
			Verbs: []string{
				"get",
				"list",
				"watch",
			},
		},
		{
			APIGroups: []string{
				"promoter.argoproj.io",
			},
			ResourceNames: []string{
				"promoter-controller-configuration",
			},
			Resources: []string{
				"controllerconfigurations/status",
			},
			Verbs: []string{
				"get",
				"patch",
				"update",
			},
		},
	}
}

// buildPolicyRuleForAPIServerClusterRole builds the default policy rule for the API Server.
// Rules mirror upstream gitops-promoter config/apiserver/base/rbac.yaml for the pinned release.
func buildPolicyRuleForAPIServerClusterRole() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{
				"promoter.argoproj.io",
			},
			Resources: []string{
				"promotionstrategies",
				"changetransferpolicyhistories",
				"changetransferpolicies",
				"restoreactivecommits",
				"pullrequests",
				"commitstatuses",
				"argocdcommitstatuses",
				"gitcommitstatuses",
				"timedcommitstatuses",
				"webrequestcommitstatuses",
				"scheduledcommitstatuses",
				"dependentssuccessfulcommitstatuses",
				"gitrepositories",
				"scmproviders",
				"clusterscmproviders",
			},
			Verbs: []string{
				"get",
				"list",
				"watch",
			},
		},
		{
			APIGroups: []string{
				"",
			},
			Resources: []string{
				"namespaces",
			},
			Verbs: []string{
				"get",
				"list",
				"watch",
			},
		},
		// Read at startup for spec.scope and spec.clusterScmProvider.mode.
		{
			APIGroups: []string{
				"promoter.argoproj.io",
			},
			ResourceNames: []string{
				"promoter-controller-configuration",
			},
			Resources: []string{
				"controllerconfigurations",
			},
			Verbs: []string{
				"get",
			},
		},
	}
}

// buildPolicyRuleForAPIServerPromotionStrategyDetailsViewer creates the PolicyRule for the view.promoter.argoproj.io resource which is managed by
// the API Server
func buildPolicyRuleForAPIServerPromotionStrategyDetailsViewer() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{
				"view.promoter.argoproj.io",
			},
			Resources: []string{
				"promotionstrategydetails",
			},
			Verbs: []string{
				"get",
				"list",
				"watch",
			},
		},
	}
}
