package e2e

import (
	"context"
	"testing"

	template "github.com/openshift/api/template/v1"

	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"k8s.io/apimachinery/pkg/types"
)

const (
	RHSSOTemplateInstanceName = "rhsso"
)

func validateRHSSO(t *testing.T) {

	framework.AddToFrameworkScheme(template.AddToScheme, &template.Template{})
	framework.AddToFrameworkScheme(template.AddToScheme, &template.TemplateInstance{})

	f := framework.Global

	// Check if rhsso templateInstance is created
	existingTemplateInstance := &template.TemplateInstance{}
	err := f.Client.Get(context.TODO(), types.NamespacedName{Name: RHSSOTemplateInstanceName, Namespace: argoCDNamespace}, existingTemplateInstance)
	assertNoError(t, err)
}
