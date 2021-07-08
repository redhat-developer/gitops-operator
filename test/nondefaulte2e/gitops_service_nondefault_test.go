package nondefaulte2e

import (
	"fmt"
	"testing"
	"time"

	"context"

	argoapi "github.com/argoproj-labs/argocd-operator/pkg/apis"
	argoapp "github.com/argoproj-labs/argocd-operator/pkg/apis/argoproj/v1alpha1"
	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	configv1 "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/redhat-developer/gitops-operator/common"
	"github.com/redhat-developer/gitops-operator/pkg/apis"
	operator "github.com/redhat-developer/gitops-operator/pkg/apis/pipelines/v1alpha1"
	"github.com/redhat-developer/gitops-operator/test/helper"

	kubeerrors "k8s.io/apimachinery/pkg/api/errors"
)

// TestGitOpsServiceNonDefaultInstall runs the operator with 'DISABLE_DEFAULT_ARGOCD_INSTANCE'
// set to true, so that the default ArgoCD instance is not created.
func TestGitOpsServiceNonDefaultInstall(t *testing.T) {

	err := framework.AddToFrameworkScheme(apis.AddToScheme, &operator.GitopsServiceList{})
	assertNoError(t, err)

	helper.EnsureCleanSlate(t)
	helper.DeleteNamespace("openshift-gitops", t)

	t.Run("Validate Namespace-scoped install", validateNamespaceScopedInstall)
	t.Run("Validate no default install", validateNoDefaultInstall)

}

// Wait up to 60 seconds to make sure the operator never creates an ArgoCD instance in openshift-gitops,
// and verify the other openshift-gitops resources are still created.
func validateNoDefaultInstall(t *testing.T) {

	framework.AddToFrameworkScheme(argoapi.AddToScheme, &argoapp.ArgoCD{})
	framework.AddToFrameworkScheme(configv1.AddToScheme, &configv1.ClusterVersion{})
	framework.AddToFrameworkScheme(routev1.AddToScheme, &routev1.Route{})

	f := framework.Global
	ctx := framework.NewContext(t)
	defer ctx.Cleanup()

	existingArgoInstance := &argoapp.ArgoCD{
		ObjectMeta: v1.ObjectMeta{
			Name:      common.ArgoCDInstanceName,
			Namespace: "openshift-gitops",
		},
	}

	// Validate that the cluster and kam resources are still created in 'openshift-gitops'
	resourceList := []helper.ResourceList{
		{
			Resource: &appsv1.Deployment{},
			ExpectedResources: []string{
				"cluster",
				"kam",
			},
		},
		{
			Resource: &routev1.Route{},
			ExpectedResources: []string{
				"cluster",
				"kam",
			},
		},
	}
	err := helper.WaitForResourcesByName(resourceList, existingArgoInstance.Namespace, time.Second*180, t)
	assertNoError(t, err)

	// Wait 60 seconds to give operator a chance to create the ArgoCD instance
	// (if an ArgoCD instance IS created, the test should fail, as this is not desired)
	err = wait.Poll(5*time.Second, 1*time.Minute, func() (done bool, err error) {

		err = f.Client.Get(context.Background(),
			types.NamespacedName{Name: existingArgoInstance.Name, Namespace: existingArgoInstance.Namespace},
			existingArgoInstance)

		if err == nil {
			err = fmt.Errorf("argoCD instance in 'openshift-gitops' should not be found")
			return true, err
		}

		return false, nil

	})

	// A timeout error is expected here: an ArgoCD instance in 'openshift-gitops' should not be found in this timeframe.
	if err != wait.ErrWaitTimeout {
		assertNoError(t, err)
	}

}

func validateNamespaceScopedInstall(t *testing.T) {

	framework.AddToFrameworkScheme(argoapi.AddToScheme, &argoapp.ArgoCD{})
	framework.AddToFrameworkScheme(configv1.AddToScheme, &configv1.ClusterVersion{})

	ctx := framework.NewContext(t)
	cleanupOptions := &framework.CleanupOptions{TestContext: ctx, Timeout: time.Second * 60, RetryInterval: time.Second * 1}
	defer ctx.Cleanup()

	f := framework.Global

	// Create new namespace
	newNamespace := &corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: helper.StandaloneArgoCDNamespace,
		},
	}
	err := f.Client.Create(context.TODO(), newNamespace, cleanupOptions)
	if !kubeerrors.IsAlreadyExists(err) {
		assertNoError(t, err)
		return
	}

	// Create new ArgoCD instance in the test namespace
	name := "standalone-argocd-instance"
	existingArgoInstance := &argoapp.ArgoCD{
		ObjectMeta: v1.ObjectMeta{
			Name:      name,
			Namespace: newNamespace.Name,
		},
	}
	err = f.Client.Create(context.TODO(), existingArgoInstance, cleanupOptions)
	assertNoError(t, err)

	// Verify that a subset of resources are created
	resourceList := []helper.ResourceList{
		{
			Resource: &appsv1.Deployment{},
			ExpectedResources: []string{
				name + "-dex-server",
				name + "-redis",
				name + "-repo-server",
				name + "-server",
			},
		},
		{
			Resource: &corev1.ConfigMap{},
			ExpectedResources: []string{
				"argocd-cm",
				"argocd-gpg-keys-cm",
				"argocd-rbac-cm",
				"argocd-ssh-known-hosts-cm",
				"argocd-tls-certs-cm",
			},
		},
		{
			Resource: &corev1.ServiceAccount{},
			ExpectedResources: []string{
				name + "-argocd-application-controller",
				name + "-argocd-server",
			},
		},
		{
			Resource: &rbacv1.Role{},
			ExpectedResources: []string{
				name + "-argocd-application-controller",
				name + "-argocd-server",
			},
		},
		{
			Resource: &rbacv1.RoleBinding{},
			ExpectedResources: []string{
				name + "-argocd-application-controller",
				name + "-argocd-server",
			},
		},
		{
			Resource: &monitoringv1.ServiceMonitor{},
			ExpectedResources: []string{
				name,
				name + "-repo-server",
				name + "-server",
			},
		},
	}

	err = helper.WaitForResourcesByName(resourceList, existingArgoInstance.Namespace, time.Second*180, t)
	assertNoError(t, err)

}

func assertNoError(t *testing.T, err error) {

	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
