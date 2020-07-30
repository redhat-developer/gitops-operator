package e2e

import (
	"fmt"
	"testing"
	"time"

	goctx "context"

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
	timeout              = time.Second * 60
	cleanupRetryInterval = time.Second * 1
	cleanupTimeout       = time.Second * 5
)

func TestGitOpsService(t *testing.T) {
	gitopsServiceList := &operator.GitopsServiceList{}
	err := framework.AddToFrameworkScheme(apis.AddToScheme, gitopsServiceList)
	if err != nil {
		t.Fatalf("failed to add custom resource scheme to framework: %v", err)
	}
	// run subtests
	t.Run("gitops-service-group", func(t *testing.T) {
		t.Run("Cluster", GitopsCluster)
	})
}

func newGitopsServiceTest(t *testing.T, f *framework.Framework, ctx *framework.TestCtx) error {

	framework.AddToFrameworkScheme(apis.AddToScheme, &routev1.Route{})
	namespace, err := ctx.GetNamespace()
	if err != nil {
		return fmt.Errorf("could not get namespace: %v", err)
	}
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
	err = f.Client.Create(goctx.TODO(), cr, &framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval})
	if err != nil {
		return err
	}

	err = e2eutil.WaitForDeployment(t, f.KubeClient, namespace, crName, 1, retryInterval, timeout)
	if err != nil {
		return err
	}

	existingServiceRef := &corev1.Service{}
	return f.Client.Get(goctx.TODO(), types.NamespacedName{Name: crName, Namespace: namespace}, existingServiceRef)
}

func GitopsCluster(t *testing.T) {
	t.Parallel()
	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup()
	err := ctx.InitializeClusterResources(&framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval})
	if err != nil {
		t.Fatalf("failed to initialize cluster resources: %v", err)
	}
	t.Log("Initialized cluster resources")
	namespace, err := ctx.GetNamespace()
	if err != nil {
		t.Fatal(err)
	}
	// get global framework variables
	f := framework.Global

	// wait for gitops-operator to be ready
	err = e2eutil.WaitForOperatorDeployment(t, f.KubeClient, namespace, "gitops-operator", 1, retryInterval, timeout)
	if err != nil {
		t.Fatal(err)
	}
	if err = newGitopsServiceTest(t, f, ctx); err != nil {
		t.Fatal(err)
	}

}
