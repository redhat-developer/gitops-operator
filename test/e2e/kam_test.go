package e2e

import (
	goctx "context"
	"fmt"
	"testing"

	routev1 "github.com/openshift/api/route/v1"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/redhat-developer/gitops-operator/pkg/apis"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

func newKamServiceTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx) error {

	framework.AddToFrameworkScheme(apis.AddToScheme, &routev1.Route{})
	namespace, err := ctx.GetNamespace()
	if err != nil {
		return fmt.Errorf("could not get namespace: %v", err)
	}

	existingServiceRef := &corev1.Service{}
	return f.Client.Get(goctx.TODO(), types.NamespacedName{Name: crName, Namespace: namespace}, existingServiceRef)
}
