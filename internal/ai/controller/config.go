package controller

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/redhat-developer/gitops-operator/internal/ai/utils"
	"k8s.io/apimachinery/pkg/labels"
)

// LoadServiceConfig builds ServiceConfig from environment variables.
func LoadServiceConfig() (ServiceConfig, error) {
	cooldown, err := utils.Duration(os.Getenv("GITOPS_AI_DIAGNOSIS_COOLDOWN"), 10*time.Minute, false)
	if err != nil {
		return ServiceConfig{}, err
	}
	autoDiagnosisGracePeriod, err := utils.Duration(os.Getenv("GITOPS_AI_DIAGNOSIS_UNHEALTHY_DURATION"), 10*time.Minute, true)
	if err != nil {
		return ServiceConfig{}, err
	}

	namespace := os.Getenv("GITOPS_AI_AGENTIC_RUN_NAMESPACE")
	if namespace == "" {
		namespace = "openshift-lightspeed"
	}

	skillsImage := os.Getenv("GITOPS_AI_SKILLS_IMAGE")
	if skillsImage == "" {
		skillsImage = "ghcr.io/redhat-developer/agentic-skills:latest"
	}
	var skillsPaths []string
	for _, entry := range strings.Split(os.Getenv(utils.SkillsPathsEnvName), ",") {
		if path := strings.TrimSpace(entry); path != "" {
			skillsPaths = append(skillsPaths, path)
		}
	}
	if len(skillsPaths) == 0 {
		skillsPaths = strings.Split(utils.DefaultSkillsPaths, ",")
	}

	operatorNamespace := os.Getenv("GITOPS_AI_OPERATOR_NAMESPACE")
	if operatorNamespace == "" {
		operatorNamespace = "openshift-gitops-operator"
	}
	selector := os.Getenv("ARGOCD_LABEL_SELECTOR")
	if _, err := labels.Parse(selector); err != nil {
		return ServiceConfig{}, fmt.Errorf("invalid ArgoCD label selector: %w", err)
	}

	maxAttempts := 2
	if v := os.Getenv("GITOPS_AI_DIAGNOSIS_MAX_ATTEMPTS"); v != "" {
		n := 0
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			maxAttempts = n
		}
	}

	return ServiceConfig{
		RunNamespace:             namespace,
		Cooldown:                 cooldown,
		SkillsImage:              skillsImage,
		SkillsPaths:              skillsPaths,
		OperatorNamespace:        operatorNamespace,
		ArgoCDLabelSelector:      selector,
		AutoDiagnosisGracePeriod: autoDiagnosisGracePeriod,
		MaxAttempts:              maxAttempts,
	}, nil
}
