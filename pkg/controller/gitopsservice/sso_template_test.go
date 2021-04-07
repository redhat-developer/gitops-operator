package gitopsservice

import (
	"testing"

	appsv1 "github.com/openshift/api/apps/v1"

	"gotest.tools/assert"
)

var (
	testSelector = map[string]string{
		"deploymentConfig": "${APPLICATION_NAME}",
	}
	testStrategy = appsv1.DeploymentStrategy{
		Type: "Recreate",
	}
)

func TestSSODeploymentConfig(t *testing.T) {
	testSSODeploymentConfig := getSSODeploymentConfigTemplate()
	assert.Equal(t, testSSODeploymentConfig.Namespace, serviceNamespace)
	assert.Equal(t, testSSODeploymentConfig.Name, "${APPLICATION_NAME}")
	assert.DeepEqual(t, testSSODeploymentConfig.Spec.Selector, testSelector)
	assert.DeepEqual(t, testSSODeploymentConfig.Spec.Strategy, testStrategy)
	assert.DeepEqual(t, testSSODeploymentConfig.Spec.Template.Spec.Containers[0], getRHSSOContainer())
}

func TestSSOInstanceCreation(t *testing.T) {
	testTemplateInstance := newSSOTemplateInstance()
	assert.Equal(t, testTemplateInstance.Namespace, serviceNamespace)
	assert.Equal(t, testTemplateInstance.Name, "rhsso")
}
