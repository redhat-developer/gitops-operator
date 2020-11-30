package e2e

import (
	"strings"
	"testing"
	"time"

	"context"

	console "github.com/openshift/api/console/v1"
	routev1 "github.com/openshift/api/route/v1"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/types"

	"github.com/operator-framework/operator-sdk/pkg/test/e2eutil"
	"github.com/redhat-developer/gitops-operator/pkg/apis"
	operator "github.com/redhat-developer/gitops-operator/pkg/apis/pipelines/v1alpha1"
)

var (
	retryInterval        = time.Second * 5
	timeout              = time.Minute * 2
	cleanupRetryInterval = time.Second * 1
	cleanupTimeout       = time.Second * 5
)

const (
	operatorName    = "gitops-operator"
	argoCDRouteName = "argocd-server"
	argoCDNamespace = "argocd"
	consoleLinkName = "argocd"
)

func TestGitOpsService(t *testing.T) {
	err := framework.AddToFrameworkScheme(apis.AddToScheme, &operator.GitopsServiceList{})
	assertNoError(t, err)

	deployOperator(t)

	// run subtests
	t.Run("Validate GitOps Backend", validateGitOpsBackend)
	t.Run("Validate ConsoleLink", validateConsoleLink)
}

func validateGitOpsBackend(t *testing.T) {
	framework.AddToFrameworkScheme(routev1.AddToScheme, &routev1.Route{})
	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup()
	namespace, err := ctx.GetNamespace()
	assertNoError(t, err)
	f := framework.Global

	// create custom resource
	crName := "example-gitops-service"
	cr := &operator.GitopsService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      crName,
			Namespace: namespace,
		},
		Spec: operator.GitopsServiceSpec{},
	}
	// use TestCtx's create helper to create the object and add a cleanup function for the new object
	err = f.Client.Create(context.TODO(), cr, &framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval})
	assertNoError(t, err)

	// check backend deployment
	err = e2eutil.WaitForDeployment(t, f.KubeClient, namespace, crName, 1, retryInterval, timeout)
	assertNoError(t, err)

	// check backend service
	err = f.Client.Get(context.TODO(), types.NamespacedName{Name: crName, Namespace: namespace}, &corev1.Service{})
	assertNoError(t, err)

	// check backend route
	err = f.Client.Get(context.TODO(), types.NamespacedName{Name: crName, Namespace: namespace}, &routev1.Route{})
	assertNoError(t, err)
}

func validateConsoleLink(t *testing.T) {
	framework.AddToFrameworkScheme(routev1.AddToScheme, &routev1.Route{})
	framework.AddToFrameworkScheme(console.AddToScheme, &console.ConsoleLink{})
	f := framework.Global

	route := &routev1.Route{}
	err := f.Client.Get(context.TODO(), types.NamespacedName{Name: argoCDRouteName, Namespace: argoCDNamespace}, route)
	assertNoError(t, err)

	// check ConsoleLink
	consoleLink := &console.ConsoleLink{}
	err = f.Client.Get(context.TODO(), types.NamespacedName{Name: consoleLinkName}, consoleLink)
	assertNoError(t, err)

	got := strings.TrimLeft(consoleLink.Spec.Href, "https://")
	if got != route.Spec.Host {
		t.Fatalf("Host mismatch: got %s, want %s", got, route.Spec.Host)
	}
}

func deployOperator(t *testing.T) {
	t.Helper()
	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup()

	err := ctx.InitializeClusterResources(&framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval})
	assertNoError(t, err)
	t.Log("Initialized Cluster resources")

	namespace, err := ctx.GetNamespace()
	assertNoError(t, err)

	err = e2eutil.WaitForOperatorDeployment(t, framework.Global.KubeClient, namespace, operatorName, 1, retryInterval, timeout)
	assertNoError(t, err)
}

func assertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
