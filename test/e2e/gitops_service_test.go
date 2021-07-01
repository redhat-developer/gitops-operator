package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"context"

	argoapi "github.com/argoproj-labs/argocd-operator/pkg/apis"
	argoapp "github.com/argoproj-labs/argocd-operator/pkg/apis/argoproj/v1alpha1"
	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	configv1 "github.com/openshift/api/config/v1"
	console "github.com/openshift/api/console/v1"
	routev1 "github.com/openshift/api/route/v1"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"gotest.tools/assert"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/operator-framework/operator-sdk/pkg/test/e2eutil"
	"github.com/redhat-developer/gitops-operator/pkg/apis"
	operator "github.com/redhat-developer/gitops-operator/pkg/apis/pipelines/v1alpha1"
	"github.com/redhat-developer/gitops-operator/pkg/controller/argocd"
	"github.com/redhat-developer/gitops-operator/pkg/controller/gitopsservice"
	"github.com/redhat-developer/gitops-operator/test/helper"

	kubeerrors "k8s.io/apimachinery/pkg/api/errors"
)

var (
	retryInterval             = time.Second * 5
	timeout                   = time.Minute * 2
	cleanupRetryInterval      = time.Second * 1
	cleanupTimeout            = time.Second * 5
	insecure             bool = false
)

const (
	operatorName                          = "gitops-operator"
	argoCDConfigMapName                   = "argocd-cm"
	argoCDRouteName                       = "openshift-gitops-server"
	argoCDNamespace                       = "openshift-gitops"
	authURL                               = "/auth/realms/master/protocol/openid-connect/token"
	depracatedArgoCDNamespace             = "openshift-pipelines-app-delivery"
	consoleLinkName                       = "argocd"
	argoCDInstanceName                    = "openshift-gitops"
	defaultKeycloakIdentifier             = "keycloak"
	defaultTemplateIdentifier             = "rhsso"
	realmURL                              = "/auth/admin/realms/argocd"
	rhssosecret                           = "keycloak-secret"
	argocdNonDefaultNamespaceInstanceName = "argocd-non-default-namespace-instance"
	argocdNonDefaultNamespace             = "argocd-non-default-source"
	standaloneArgoCDNamespace             = "gitops-standalone-test"
)

func TestGitOpsService(t *testing.T) {
	err := framework.AddToFrameworkScheme(apis.AddToScheme, &operator.GitopsServiceList{})
	assertNoError(t, err)

	ensureCleanSlate(t)

	if !skipOperatorDeployment() {
		deployOperator(t)
	}

	// run subtests
	t.Run("Validate kam service", validateKamService)
	t.Run("Validate GitOps Backend", validateGitOpsBackend)
	t.Run("Validate ConsoleLink", validateConsoleLink)
	t.Run("Validate ArgoCD Installation", validateArgoCDInstallation)
	t.Run("Validate ArgoCD Metrics Configuration", validateArgoCDMetrics)
	t.Run("Validate machine config updates", validateMachineConfigUpdates)
	t.Run("Validate non-default argocd namespace management", validateNonDefaultArgocdNamespaceManagement)
	t.Run("Validate cluster config updates", validateClusterConfigChange)
	t.Run("Validate Redhat Single sign-on Installation", verifyRHSSOInstallation)
	t.Run("Validate Redhat Single sign-on Configuration", verifyRHSSOConfiguration)
	t.Run("Validate Redhat Single sign-on Uninstallation", verifyRHSSOUnInstallation)
	t.Run("Validate Namespace-scoped install", validateNamespaceScopedInstall)
	t.Run("Validate tear down of ArgoCD Installation", tearDownArgoCD)

}

func validateGitOpsBackend(t *testing.T) {
	framework.AddToFrameworkScheme(routev1.AddToScheme, &routev1.Route{})
	framework.AddToFrameworkScheme(configv1.AddToScheme, &configv1.ClusterVersion{})
	ctx := framework.NewContext(t)
	defer ctx.Cleanup()

	name := "cluster"
	f := framework.Global
	namespace, err := gitopsservice.GetBackendNamespace(f.Client.Client)
	assertNoError(t, err)

	// check backend deployment
	err = e2eutil.WaitForDeployment(t, f.KubeClient, namespace, name, 1, retryInterval, timeout)
	assertNoError(t, err)

	// check backend service
	err = f.Client.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: namespace}, &corev1.Service{})
	assertNoError(t, err)

	// check backend route
	err = f.Client.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: namespace}, &routev1.Route{})
	assertNoError(t, err)
}

func validateConsoleLink(t *testing.T) {
	framework.AddToFrameworkScheme(routev1.AddToScheme, &routev1.Route{})
	framework.AddToFrameworkScheme(console.AddToScheme, &console.ConsoleLink{})
	framework.AddToFrameworkScheme(configv1.AddToScheme, &configv1.ClusterVersion{})
	ctx := framework.NewContext(t)
	defer ctx.Cleanup()
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
	ctx := framework.NewContext(t)
	defer ctx.Cleanup()

	err := ctx.InitializeClusterResources(&framework.CleanupOptions{TestContext: ctx, Timeout: cleanupTimeout, RetryInterval: cleanupRetryInterval})
	assertNoError(t, err)
	t.Log("Initialized Cluster resources")

	namespace, err := ctx.GetNamespace()
	assertNoError(t, err)

	err = e2eutil.WaitForOperatorDeployment(t, framework.Global.KubeClient, namespace, operatorName, 1, retryInterval, timeout)
	assertNoError(t, err)
}

func validateArgoCDInstallation(t *testing.T) {
	framework.AddToFrameworkScheme(argoapi.AddToScheme, &argoapp.ArgoCD{})
	framework.AddToFrameworkScheme(configv1.AddToScheme, &configv1.ClusterVersion{})
	ctx := framework.NewContext(t)
	defer ctx.Cleanup()
	f := framework.Global

	// Check if argocd namespace is created
	err := f.Client.Get(context.TODO(), types.NamespacedName{Name: argoCDNamespace}, &corev1.Namespace{})
	assertNoError(t, err)

	// Check if ArgoCD instance is created
	existingArgoInstance := &argoapp.ArgoCD{}
	err = f.Client.Get(context.TODO(), types.NamespacedName{Name: argoCDInstanceName, Namespace: argoCDNamespace}, existingArgoInstance)
	assertNoError(t, err)

	// modify the ArgoCD instance "manually"
	// and ensure that a manual modification of the
	// ArgoCD CR is allowed, and not overwritten
	// by the reconciler

	existingArgoInstance.Spec.DisableAdmin = true
	err = f.Client.Update(context.TODO(), existingArgoInstance)
	if err != nil {
		t.Fatal(err)
	}

	// assumption that an attempt to reconcile would have happened within 5 seconds.
	// This can definitely be improved.
	time.Sleep(5 * time.Second)

	// Check if ArgoCD CR was overwritten
	existingArgoInstance = &argoapp.ArgoCD{}
	err = f.Client.Get(context.TODO(), types.NamespacedName{Name: argoCDInstanceName, Namespace: argoCDNamespace}, existingArgoInstance)
	assertNoError(t, err)

	// check that this has not been overwritten
	assert.Equal(t, existingArgoInstance.Spec.DisableAdmin, true)

}

func assertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func validateArgoCDMetrics(t *testing.T) {
	framework.AddToFrameworkScheme(rbacv1.AddToScheme, &rbacv1.Role{})
	framework.AddToFrameworkScheme(rbacv1.AddToScheme, &rbacv1.RoleBinding{})
	framework.AddToFrameworkScheme(monitoringv1.AddToScheme, &monitoringv1.ServiceMonitor{})
	framework.AddToFrameworkScheme(monitoringv1.AddToScheme, &monitoringv1.PrometheusRule{})
	ctx := framework.NewContext(t)
	defer ctx.Cleanup()
	f := framework.Global

	// Check the role was created
	role := rbacv1.Role{}
	readRoleName := fmt.Sprintf("%s-read", argoCDNamespace)
	err := f.Client.Get(context.TODO(),
		types.NamespacedName{Name: readRoleName, Namespace: argoCDNamespace}, &role)
	assertNoError(t, err)

	// Check the role binding was created
	roleBinding := rbacv1.RoleBinding{}
	roleBindingName := fmt.Sprintf("%s-prometheus-k8s-read-binding", argoCDNamespace)
	err = f.Client.Get(context.TODO(),
		types.NamespacedName{Name: roleBindingName, Namespace: argoCDNamespace},
		&roleBinding)
	assertNoError(t, err)

	// Check the application service monitor was created
	serviceMonitor := monitoringv1.ServiceMonitor{}
	serviceMonitorName := argoCDInstanceName
	err = f.Client.Get(context.TODO(),
		types.NamespacedName{Name: serviceMonitorName, Namespace: argoCDNamespace},
		&serviceMonitor)
	assertNoError(t, err)

	// Check the api server service monitor was created
	serviceMonitor = monitoringv1.ServiceMonitor{}
	serviceMonitorName = fmt.Sprintf("%s-server", argoCDInstanceName)
	err = f.Client.Get(context.TODO(),
		types.NamespacedName{Name: serviceMonitorName, Namespace: argoCDNamespace},
		&serviceMonitor)
	assertNoError(t, err)

	// Check the repo server service monitor was created
	serviceMonitor = monitoringv1.ServiceMonitor{}
	serviceMonitorName = fmt.Sprintf("%s-repo-server", argoCDInstanceName)
	err = f.Client.Get(context.TODO(),
		types.NamespacedName{Name: serviceMonitorName, Namespace: argoCDNamespace},
		&serviceMonitor)
	assertNoError(t, err)

	// Check the prometheus rule was created
	rule := monitoringv1.PrometheusRule{}
	err = f.Client.Get(context.TODO(),
		types.NamespacedName{Name: "gitops-operator-argocd-alerts", Namespace: argoCDNamespace},
		&rule)
	assertNoError(t, err)
}

func tearDownArgoCD(t *testing.T) {
	framework.AddToFrameworkScheme(argoapi.AddToScheme, &argoapp.ArgoCD{})
	framework.AddToFrameworkScheme(configv1.AddToScheme, &configv1.ClusterVersion{})
	ctx := framework.NewContext(t)
	defer ctx.Cleanup()
	f := framework.Global

	existingArgoInstance := &argoapp.ArgoCD{}
	err := f.Client.Get(context.TODO(), types.NamespacedName{Name: argoCDInstanceName, Namespace: argoCDNamespace}, existingArgoInstance)
	assertNoError(t, err)

	// Tear down Argo CD instance
	err = f.Client.Delete(context.TODO(), existingArgoInstance, &client.DeleteOptions{})
	assertNoError(t, err)

	err = e2eutil.WaitForDeletion(t, f.Client.Client, existingArgoInstance, retryInterval, timeout)
	assertNoError(t, err)

}

func validateMachineConfigUpdates(t *testing.T) {

	framework.AddToFrameworkScheme(configv1.AddToScheme, &configv1.Image{})
	ctx := framework.NewContext(t)
	defer ctx.Cleanup()
	f := framework.Global

	imageAppCr := filepath.Join("test", "appcrs", "image_appcr.yaml")
	ocPath, err := exec.LookPath("oc")
	if err != nil {
		t.Fatal(err)
	}

	// 'When GitOps operator is run locally (not installed via OLM), it does not correctly setup
	// the 'argoproj.io' Role rules for the 'argocd-application-controller'
	// Thus, applying missing rules for 'argocd-application-controller'
	// TODO: Remove once https://github.com/redhat-developer/gitops-operator/issues/148 is fixed
	if err := applyMissingPermissions(f, ocPath); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(ocPath, "apply", "-f", imageAppCr)
	err = cmd.Run()
	if err != nil {
		t.Fatal(err)
	}

	err = wait.Poll(time.Second*1, time.Minute*10, func() (bool, error) {

		if err := helper.ApplicationHealthStatus("image", "openshift-gitops"); err != nil {
			t.Log(err)
			return false, nil
		}

		if err := helper.ApplicationSyncStatus("image", "openshift-gitops"); err != nil {
			t.Log(err)
			return false, nil
		}

		return true, nil

	})
	if err != nil {
		t.Fatal(err)
	}

	existingImage := &configv1.Image{
		ObjectMeta: v1.ObjectMeta{
			Name: "cluster",
		},
	}

	err = f.Client.Get(context.TODO(), types.NamespacedName{Name: existingImage.Name}, existingImage)
	assertNoError(t, err)
}

// ensureCleanSlate runs before the tests, to ensure that the cluster is in the expected pre-test state
func ensureCleanSlate(t *testing.T) {
	f := framework.Global

	t.Log("Running ensureCleanSlate")

	// Delete the standaloneArgoCDNamespace namespace and wait for it to not exist
	nsGitopsStandaloneTest := &corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: standaloneArgoCDNamespace,
		},
	}
	f.Client.Delete(context.Background(), nsGitopsStandaloneTest)

	err := wait.Poll(1*time.Second, 60*time.Second, func() (bool, error) {
		if err := f.Client.Get(context.Background(), types.NamespacedName{Name: nsGitopsStandaloneTest.Name},
			nsGitopsStandaloneTest); kubeerrors.IsNotFound(err) {
			t.Logf("Namespace '%s' no longer exists", nsGitopsStandaloneTest.Name)
			return true, nil
		}

		t.Logf("Namespace '%s' still exists", nsGitopsStandaloneTest.Name)

		return false, nil
	})

	if err != nil {
		assertNoError(t, fmt.Errorf("Namespace was not deleted: %v", err))
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
			Name: standaloneArgoCDNamespace,
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
	resourceList := []resourceList{
		{
			resource: &appsv1.Deployment{},
			expectedResources: []string{
				name + "-dex-server",
				name + "-redis",
				name + "-repo-server",
				name + "-server",
			},
		},
		{
			resource: &corev1.ConfigMap{},
			expectedResources: []string{
				"argocd-cm",
				"argocd-gpg-keys-cm",
				"argocd-rbac-cm",
				"argocd-ssh-known-hosts-cm",
				"argocd-tls-certs-cm",
			},
		},
		{
			resource: &corev1.ServiceAccount{},
			expectedResources: []string{
				name + "-argocd-application-controller",
				name + "-argocd-server",
			},
		},
		{
			resource: &rbacv1.Role{},
			expectedResources: []string{
				name + "-argocd-application-controller",
				name + "-argocd-server",
			},
		},
		{
			resource: &rbacv1.RoleBinding{},
			expectedResources: []string{
				name + "-argocd-application-controller",
				name + "-argocd-server",
			},
		},
		{
			resource: &monitoringv1.ServiceMonitor{},
			expectedResources: []string{
				name,
				name + "-repo-server",
				name + "-server",
			},
		},
	}

	err = waitForResourcesByName(resourceList, existingArgoInstance.Namespace, time.Second*180, t)
	assertNoError(t, err)

}

// waitForResourcesByName will wait up to 'timeout' minutes for a set of resources to exist; the resources
// should be of the given type (Deployment, Service, etc) and name(s).
// Returns error if the resources could not be found within the given time frame.
func waitForResourcesByName(resourceList []resourceList, namespace string, timeout time.Duration, t *testing.T) error {

	f := framework.Global

	// Wait X seconds for all the resources to be created
	err := wait.Poll(time.Second*1, timeout, func() (bool, error) {

		for _, resourceListEntry := range resourceList {

			for _, resourceName := range resourceListEntry.expectedResources {

				resource := resourceListEntry.resource.DeepCopyObject()
				namespacedName := types.NamespacedName{Name: resourceName, Namespace: namespace}
				if err := f.Client.Get(context.TODO(), namespacedName, resource); err != nil {
					t.Logf("Unable to retrieve expected resource %s: %v", resourceName, err)
					return false, nil
				} else {
					t.Logf("Able to retrieve %s", resourceName)
				}
			}

		}

		return false, nil
	})

	return err
}

func validateNonDefaultArgocdNamespaceManagement(t *testing.T) {
	framework.AddToFrameworkScheme(argoapi.AddToScheme, &argoapp.ArgoCD{})
	framework.AddToFrameworkScheme(configv1.AddToScheme, &configv1.ClusterVersion{})

	ctx := framework.NewContext(t)
	cleanupOptions := &framework.CleanupOptions{TestContext: ctx, Timeout: time.Second * 60, RetryInterval: time.Second * 1}
	defer ctx.Cleanup()
	f := framework.Global

	// Create non-default argocd source namespace
	argocdNonDefaultNamespaceObj := &corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: argocdNonDefaultNamespace,
		},
	}

	err := f.Client.Create(context.TODO(), argocdNonDefaultNamespaceObj, cleanupOptions)
	if !kubeerrors.IsAlreadyExists(err) {
		assertNoError(t, err)
		return
	}

	// Create argocd instance in non-default namespace
	argocdNonDefaultNamespaceInstance, err := argocd.NewCR(argocdNonDefaultNamespaceInstanceName, argocdNonDefaultNamespace)
	err = f.Client.Create(context.TODO(), argocdNonDefaultNamespaceInstance, cleanupOptions)
	assertNoError(t, err)

	identityProviderAppCr := filepath.Join("test", "appcrs", "identity-provider_appcr.yaml")
	ocPath, err := exec.LookPath("oc")
	if err != nil {
		t.Fatal(err)
	}

	// apply argocd application CR
	cmd := exec.Command(ocPath, "apply", "-f", identityProviderAppCr)
	err = cmd.Run()
	if err != nil {
		t.Fatal(err)
	}

	err = wait.Poll(time.Second*1, time.Second*60, func() (bool, error) {
		if err := helper.ApplicationHealthStatus("identity-provider", argocdNonDefaultNamespace); err != nil {
			t.Log(err)
			return false, nil
		}
		if err := helper.ApplicationSyncStatus("identity-provider", argocdNonDefaultNamespace); err != nil {
			t.Log(err)
			return false, nil
		}

		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}

}

// resourceList is used by waitForResourcesByName
type resourceList struct {
	// resource is the type of resource to verify that it exists
	resource runtime.Object

	// expectedResources are the names of the resources of the above type
	expectedResources []string
}

func skipOperatorDeployment() bool {
	return os.Getenv("SKIP_OPERATOR_DEPLOYMENT") == "true"
}

func validateClusterConfigChange(t *testing.T) {
	framework.AddToFrameworkScheme(rbacv1.AddToScheme, &rbacv1.Role{})
	framework.AddToFrameworkScheme(rbacv1.AddToScheme, &rbacv1.RoleBinding{})

	ctx := framework.NewContext(t)
	defer ctx.Cleanup()
	f := framework.Global

	ocPath, err := exec.LookPath("oc")
	if err != nil {
		t.Fatal(err)
	}

	// 'When GitOps operator is run locally (not installed via OLM), it does not correctly setup
	// the 'argoproj.io' Role rules for the 'argocd-application-controller'
	// Thus, applying missing rules for 'argocd-application-controller'
	// TODO: Remove once https://github.com/redhat-developer/gitops-operator/issues/148 is fixed
	if err := applyMissingPermissions(f, ocPath); err != nil {
		t.Fatal(err)
	}

	// Wait for the default project to exist; this avoids a race condition where the Application
	// can be created before the Project that it targets.
	if err := wait.Poll(time.Second*5, time.Minute*5, func() (bool, error) {
		if status, err := helper.ProjectExists("default", "openshift-gitops"); !status {
			t.Log(err)
			return false, nil
		}
		return true, nil
	}); err != nil {
		t.Fatalf("project never existed %v", err)
	}

	schedulerYAML := filepath.Join("test", "appcrs", "scheduler_appcr.yaml")

	cmd := exec.Command(ocPath, "apply", "-f", schedulerYAML)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatal(err, string(output))
	}

	err = wait.Poll(time.Second*5, time.Minute*2, func() (bool, error) {
		if err := helper.ApplicationHealthStatus("policy-configmap", "openshift-gitops"); err != nil {
			t.Log(err)
			return false, nil
		}
		if err := helper.ApplicationSyncStatus("policy-configmap", "openshift-gitops"); err != nil {
			t.Log(err)
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	namespacedName := types.NamespacedName{Name: "policy-configmap", Namespace: "openshift-config"}
	existingConfigMap := &corev1.ConfigMap{}

	err = wait.Poll(time.Second*1, time.Minute*1, func() (bool, error) {
		if err := f.Client.Get(context.TODO(), namespacedName, existingConfigMap); err != nil {
			t.Log(err)
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}

}

func applyMissingPermissions(f *framework.Framework, ocPath string) error {

	// Check the role was created. If not, create a new role
	role := rbacv1.Role{}
	roleName := fmt.Sprintf("%s-openshift-gitops-argocd-application-controller", argoCDNamespace)
	err := f.Client.Get(context.TODO(),
		types.NamespacedName{Name: roleName, Namespace: argoCDNamespace}, &role)
	if err != nil {
		roleYAML := filepath.Join("test", "rolebindings", "role.yaml")
		cmd := exec.Command(ocPath, "apply", "-f", roleYAML)
		_, err := cmd.CombinedOutput()
		if err != nil {
			return err
		}
	}

	// Check the role binding was created. If not, create a new role binding
	roleBinding := rbacv1.RoleBinding{}
	roleBindingName := fmt.Sprintf("%s-openshift-gitops-argocd-application-controller", argoCDNamespace)
	err = f.Client.Get(context.TODO(),
		types.NamespacedName{Name: roleBindingName, Namespace: argoCDNamespace},
		&roleBinding)
	if err != nil {
		roleBindingYAML := filepath.Join("test", "rolebindings", "role-binding.yaml")
		cmd := exec.Command(ocPath, "apply", "-f", roleBindingYAML)
		_, err = cmd.CombinedOutput()
		if err != nil {
			return err
		}
	}

	return nil
}
