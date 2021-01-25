package e2e

import (
	"context"
	"strings"
	"testing"

	console "github.com/openshift/api/console/v1"
	routev1 "github.com/openshift/api/route/v1"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/operator-framework/operator-sdk/pkg/test/e2eutil"
	"gotest.tools/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

func validateKamService(t *testing.T) {

	framework.AddToFrameworkScheme(routev1.AddToScheme, &routev1.Route{})
	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup()
	namespace := "openshift-gitops"
	name := "kam"
	f := framework.Global

	// check backend deployment
	err := e2eutil.WaitForDeployment(t, f.KubeClient, namespace, name, 1, retryInterval, timeout)
	assertNoError(t, err)

	// check backend service
	err = f.Client.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: namespace}, &corev1.Service{})
	assertNoError(t, err)

	// check backend route
	route := &routev1.Route{}
	err = f.Client.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: namespace}, route)
	assertNoError(t, err)

	// check ConsoleLink
	consoleCLIDownoad := &console.ConsoleCLIDownload{}
	err = f.Client.Get(context.TODO(), types.NamespacedName{Name: consoleLinkName}, consoleCLIDownoad)
	assertNoError(t, err)

	got := strings.TrimLeft(consoleCLIDownoad.Spec.Links[0].Href, "https://")
	if got != route.Spec.Host {
		t.Fatalf("Host mismatch: got %s, want %s", got, route.Spec.Host)
	}

	assert.Equal(t, consoleCLIDownoad.OwnerReferences[0].Name, route.OwnerReferences[0].Name)
}
