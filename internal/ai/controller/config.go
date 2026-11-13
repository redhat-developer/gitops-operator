package controller

import (
	"fmt"
	"os"
	"time"

	"github.com/redhat-developer/gitops-operator/internal/ai/utils"
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

	argocdNamespace := os.Getenv("ARGOCD_NAMESPACE")
	if argocdNamespace == "" {
		argocdNamespace = "openshift-gitops"
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
		ArgocdNamespace:          argocdNamespace,
		AutoDiagnosisGracePeriod: autoDiagnosisGracePeriod,
		MaxAttempts:              maxAttempts,
	}, nil
}
