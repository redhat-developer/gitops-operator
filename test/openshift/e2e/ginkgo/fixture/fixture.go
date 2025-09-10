package fixture

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	//lint:ignore ST1001 "This is a common practice in Gomega tests for readability."
	. "github.com/onsi/ginkgo/v2" //nolint:all
	//lint:ignore ST1001 "This is a common practice in Gomega tests for readability."
	. "github.com/onsi/gomega" //nolint:all
	securityv1 "github.com/openshift/api/security/v1"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rolloutmanagerv1alpha1 "github.com/argoproj-labs/argo-rollouts-manager/api/v1alpha1"
	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	olmv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"

	"github.com/onsi/gomega/format"
	gitopsoperatorv1alpha1 "github.com/redhat-developer/gitops-operator/api/v1alpha1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	deploymentFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	osFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/os"
	subscriptionFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/subscription"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	crdv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// E2ETestLabelsKey and E2ETestLabelsValue are added to cluster-scoped resources (e.g. Namespaces) created by E2E tests (where possible). On startup (and before each test for sequential tests), any resources with this label will be deleted.
	E2ETestLabelsKey   = "app"
	E2ETestLabelsValue = "test-argo-app"
)

var NamespaceLabels = map[string]string{E2ETestLabelsKey: E2ETestLabelsValue}

// Retrieve installation namespace
func GetInstallationNamespace() string {

	k8sClient, _ := utils.GetE2ETestKubeClient()
	installationNamespace := "openshift-operators"

	sub := &olmv1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "openshift-gitops-operator",
			Namespace: installationNamespace,
		},
	}

	if err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(sub), sub); err != nil {

		installationNamespace = "openshift-gitops-operator"

		sub = &olmv1alpha1.Subscription{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-operator", Namespace: "openshift-gitops-operator"}}

		if err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(sub), sub); err != nil {
			return ""
		}
	}
	return installationNamespace
}

func EnsureParallelCleanSlate() {

	// Increase the maximum length of debug output, for when tests fail
	format.MaxLength = 64 * 1024
	SetDefaultEventuallyTimeout(time.Second * 60)
	SetDefaultEventuallyPollingInterval(time.Second * 3)
	SetDefaultConsistentlyDuration(time.Second * 10)
	SetDefaultConsistentlyPollingInterval(time.Second * 1)

	k8sClient, _ := utils.GetE2ETestKubeClient()

	// Finally, wait for default openshift-gitops instance to be ready
	// - Parallel tests should not write to any resources in 'openshift-gitops' namespace (sequential only), but they are allowed to read from them.
	defaultOpenShiftGitOpsArgoCD := &argov1beta1api.ArgoCD{
		ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops", Namespace: "openshift-gitops"},
	}
	err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(defaultOpenShiftGitOpsArgoCD), defaultOpenShiftGitOpsArgoCD)
	Expect(err).ToNot(HaveOccurred())

	Eventually(defaultOpenShiftGitOpsArgoCD, "5m", "5s").Should(argocd.BeAvailableWithCustomSleepTime(3 * time.Second))

	// Unlike sequential clean slate, parallel clean slate cannot assume that there are no other tests running. This limits our ability to clean up old test artifacts.
}

// EnsureSequentialCleanSlate will clean up resources that were created during previous sequential tests
// - Deletes namespaces that were created by previous tests
// - Deletes other cluster-scoped resources that were created
// - Reverts changes made to Subscription CR
// - etc
func EnsureSequentialCleanSlate() {
	Expect(EnsureSequentialCleanSlateWithError()).To(Succeed())
}

func EnsureSequentialCleanSlateWithError() error {

	// With sequential tests, we are always safe to assume that there is no other test running. That allows us to clean up old test artifacts before new test starts.

	// Increase the maximum length of debug output, for when tests fail
	format.MaxLength = 64 * 1024
	SetDefaultEventuallyTimeout(time.Second * 60)
	SetDefaultEventuallyPollingInterval(time.Second * 3)
	SetDefaultConsistentlyDuration(time.Second * 10)
	SetDefaultConsistentlyPollingInterval(time.Second * 1)

	ctx := context.Background()
	k8sClient, _ := utils.GetE2ETestKubeClient()

	// If the CSV in 'openshift-gitops-operator' NS exists, make sure the CSV does not contain the dynamic plugin env var
	if err := RemoveDynamicPluginFromCSV(ctx, k8sClient); err != nil {
		return err
	}

	RestoreSubcriptionToDefault()

	// ensure namespaces created during test are deleted
	err := ensureTestNamespacesDeleted(ctx, k8sClient)
	if err != nil {
		return err
	}

	// wait for openshift-gitops ArgoCD to exist, if it doesn't already
	defaultOpenShiftGitOpsArgoCD := &argov1beta1api.ArgoCD{
		ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops", Namespace: "openshift-gitops"},
	}
	Eventually(defaultOpenShiftGitOpsArgoCD, "3m", "5s").Should(k8s.ExistByName())

	// Ensure that default state of ArgoCD CR in openshift-gitops is restored
	if err := updateWithoutConflict(defaultOpenShiftGitOpsArgoCD, func(obj client.Object) {
		argocdObj, ok := obj.(*argov1beta1api.ArgoCD)
		Expect(ok).To(BeTrue())

		// HA should be disabled by default
		argocdObj.Spec.HA.Enabled = false

		// .spec.monitoring.disableMetrics should be nil by default
		argocdObj.Spec.Monitoring.DisableMetrics = nil

		// Ensure that api server route has not been disabled, nor exposed via different settings
		argocdObj.Spec.Server.Route = argov1beta1api.ArgoCDRouteSpec{
			Enabled: true,
			TLS:     nil,
			// TLS: &routev1.TLSConfig{
			// 	Termination:                   routev1.TLSTerminationReencrypt,
			// 	InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyRedirect,
			// },
		}

		// Reset app controller processors to default
		argocdObj.Spec.Controller.Processors = argov1beta1api.ArgoCDApplicationControllerProcessorsSpec{}

		// Reset repo server replicas to default
		argocdObj.Spec.Repo.Replicas = nil

		// Reset source namespaces
		argocdObj.Spec.SourceNamespaces = nil
		argocdObj.Spec.ApplicationSet.SourceNamespaces = nil

	}); err != nil {
		return err
	}

	gitopsService := &gitopsoperatorv1alpha1.GitopsService{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
	}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(gitopsService), gitopsService); err != nil {
		return err
	}

	// Ensure that run on infra is disabled: some tests will enable it
	if err := updateWithoutConflict(gitopsService, func(obj client.Object) {
		goObj, ok := obj.(*gitopsoperatorv1alpha1.GitopsService)
		Expect(ok).To(BeTrue())

		goObj.Spec.NodeSelector = nil
		goObj.Spec.RunOnInfra = false
		goObj.Spec.Tolerations = nil
	}); err != nil {
		return err
	}

	// Clean up old cluster-scoped role from 1-034
	_ = k8sClient.Delete(ctx, &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "custom-argocd-role"}})

	// Delete all existing RolloutManagers in openshift-gitops Namespace
	var rolloutManagerList rolloutmanagerv1alpha1.RolloutManagerList
	if err := k8sClient.List(ctx, &rolloutManagerList, client.InNamespace("openshift-gitops")); err != nil {
		return err
	}
	for _, rm := range rolloutManagerList.Items {
		if err := k8sClient.Delete(ctx, &rm); err != nil {
			return err
		}
	}

	// Delete 'restricted-dropcaps' which is created by at least one test
	scc := &securityv1.SecurityContextConstraints{
		ObjectMeta: metav1.ObjectMeta{
			Name: "restricted-dropcaps",
		},
	}
	if err := k8sClient.Delete(ctx, scc); err != nil {
		if !apierr.IsNotFound(err) {
			return err
		}
		// Otherwise, expected error if it doesn't exist.
	}

	// Finally, wait for default openshift-gitops instance to be ready
	Eventually(defaultOpenShiftGitOpsArgoCD, "5m", "5s").Should(argocd.BeAvailable())

	return nil
}

// RemoveDynamicPluginFromCSV ensures that if the CSV in 'openshift-gitops-operator' NS exists, that the CSV does not contain the dynamic plugin env var
func RemoveDynamicPluginFromCSV(ctx context.Context, k8sClient client.Client) error {

	if EnvNonOLM() || EnvLocalRun() {
		// Skipping as CSV does exist when not using OLM, nor does it exist when running locally
		return nil
	}

	var csv *olmv1alpha1.ClusterServiceVersion
	var csvList olmv1alpha1.ClusterServiceVersionList
	installationNamespace := GetInstallationNamespace()
	Expect(installationNamespace).ToNot(BeNil(), "if you see this, it likely means, either: A) the operator is not installed via OLM (and you meant to install it), OR B) you are running the operator locally via 'make run', and thus should specify LOCAL_RUN=true env var when calling the test")
	Expect(k8sClient.List(ctx, &csvList, client.InNamespace(installationNamespace))).To(Succeed())

	for idx := range csvList.Items {
		idxCSV := csvList.Items[idx]
		if strings.Contains(idxCSV.Name, "gitops-operator") {
			csv = &idxCSV
			break
		}
	}
	Expect(csv).ToNot(BeNil(), "if you see this, it likely means, either: A) the operator is not installed via OLM (and you meant to install it), OR B) you are running the operator locally via 'make run', and thus should specify LOCAL_RUN=true env var when calling the test")

	if err := updateWithoutConflict(csv, func(obj client.Object) {

		csvObj, ok := obj.(*olmv1alpha1.ClusterServiceVersion)
		Expect(ok).To(BeTrue())

		envList := csvObj.Spec.InstallStrategy.StrategySpec.DeploymentSpecs[0].Spec.Template.Spec.Containers[0].Env

		newEnvList := []corev1.EnvVar{}
		for idx := range envList {
			idxEnv := envList[idx]
			if idxEnv.Name == "DYNAMIC_PLUGIN_START_OCP_VERSION" {
				continue
			} else {
				newEnvList = append(newEnvList, idxEnv)
			}
		}
		csvObj.Spec.InstallStrategy.StrategySpec.DeploymentSpecs[0].Spec.Template.Spec.Containers[0].Env = newEnvList

	}); err != nil {
		return err
	}
	return nil
}

func CreateRandomE2ETestNamespace() *corev1.Namespace {

	randomVal := string(uuid.NewUUID())
	randomVal = randomVal[0:13] // Only use 14 characters of randomness. If we use more, then we start to hit limits on parts of code which limit # of characters to 63

	testNamespaceName := "gitops-e2e-test-" + randomVal

	ns := CreateNamespace(testNamespaceName)
	return ns
}

func CreateRandomE2ETestNamespaceWithCleanupFunc() (*corev1.Namespace, func()) {

	ns := CreateRandomE2ETestNamespace()
	return ns, nsDeletionFunc(ns)
}

// Create namespace for tests having a specific label for identification
// - If the namespace already exists, it will be deleted first
func CreateNamespace(name string) *corev1.Namespace {

	k8sClient, _ := utils.GetE2ETestKubeClient()

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	// If the Namespace already exists, delete it first
	if err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(ns), ns); err == nil {
		// Namespace exists, so delete it first
		Expect(deleteNamespaceAndVerify(context.Background(), ns.Name, k8sClient)).To(Succeed())
	}

	ns = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name:   name,
		Labels: NamespaceLabels,
	}}

	err := k8sClient.Create(context.Background(), ns)
	Expect(err).ToNot(HaveOccurred())

	return ns
}

func CreateNamespaceWithCleanupFunc(name string) (*corev1.Namespace, func()) {

	ns := CreateNamespace(name)
	return ns, nsDeletionFunc(ns)
}

// Create a namespace 'name' that is managed by another namespace 'managedByNamespace', via managed-by label.
func CreateManagedNamespace(name string, managedByNamespace string) *corev1.Namespace {
	k8sClient, _ := utils.GetE2ETestKubeClient()

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}

	// If the Namespace already exists, delete it first
	if err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(ns), ns); err == nil {
		// Namespace exists, so delete it first
		Expect(deleteNamespaceAndVerify(context.Background(), ns.Name, k8sClient)).To(Succeed())
	}

	ns = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name: name,
		Labels: map[string]string{
			E2ETestLabelsKey:                E2ETestLabelsValue,
			"argocd.argoproj.io/managed-by": managedByNamespace,
		},
	}}

	Expect(k8sClient.Create(context.Background(), ns)).To(Succeed())

	return ns

}

func CreateManagedNamespaceWithCleanupFunc(name string, managedByNamespace string) (*corev1.Namespace, func()) {
	ns := CreateManagedNamespace(name, managedByNamespace)
	return ns, nsDeletionFunc(ns)
}

// nsDeletionFunc is a convenience function that returns a function that deletes a namespace. This is used for Namespace cleanup by other functions.
func nsDeletionFunc(ns *corev1.Namespace) func() {

	return func() {
		DeleteNamespace(ns)
	}

}

func DeleteNamespace(ns *corev1.Namespace) {
	// If you are debugging an E2E test and want to prevent its namespace from being deleted when the test ends (so that you can examine the state of resources in the namespace) you can set E2E_DEBUG_SKIP_CLEANUP env var.
	if os.Getenv("E2E_DEBUG_SKIP_CLEANUP") != "" {
		GinkgoWriter.Println("Skipping namespace cleanup as E2E_DEBUG_SKIP_CLEANUP is set")
		return
	}

	k8sClient, _, err := utils.GetE2ETestKubeClientWithError()
	Expect(err).ToNot(HaveOccurred())

	err = deleteNamespaceAndVerify(context.Background(), ns.Name, k8sClient)
	Expect(err).ToNot(HaveOccurred())

}

// EnvNonOLM checks if NON_OLM var is set; this variable is set when testing on GitOps operator that is not installed via OLM
func EnvNonOLM() bool {
	_, exists := os.LookupEnv("NON_OLM")
	return exists
}

func EnvLocalRun() bool {
	_, exists := os.LookupEnv("LOCAL_RUN")
	return exists
}

// EnvCI checks if CI env var is set; this variable is set when testing on GitOps Operator running via CI pipeline (and using an OLM Subscription)
func EnvCI() bool {
	_, exists := os.LookupEnv("CI")
	return exists
}

// GetEnvInOperatorSubscriptionOrDeployment will return the value of an environment variable, in either operator Subscription or operator Deployment, depending on which mode the test is running in.
func GetEnvInOperatorSubscriptionOrDeployment(key string) (*string, error) {

	k8sClient, _, err := utils.GetE2ETestKubeClientWithError()
	if err != nil {
		return nil, nil
	}

	installationNamespace := GetInstallationNamespace()

	if EnvNonOLM() {
		depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-operator-controller-manager", Namespace: installationNamespace}}

		return deploymentFixture.GetEnv(depl, "manager", key)

	} else if EnvCI() {

		sub, err := GetSubscriptionInEnvCIEnvironment(k8sClient)
		if err != nil {
			return nil, err
		}
		if sub == nil {
			return nil, nil
		}

		envVal, err := subscriptionFixture.GetEnv(sub, key)

		return envVal, err

	} else {

		sub := &olmv1alpha1.Subscription{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-operator", Namespace: installationNamespace}}
		if err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(sub), sub); err != nil {
			return nil, err
		}

		return subscriptionFixture.GetEnv(sub, key)

	}

}

// SetEnvInOperatorSubscriptionOrDeployment will set the value of an environment variable, in either operator Subscription (under .spec.config.env) or operator Deployment (under template spec), depending on which mode the test is running in.
func SetEnvInOperatorSubscriptionOrDeployment(key string, value string) {

	k8sClient, _ := utils.GetE2ETestKubeClient()

	installationNamespace := GetInstallationNamespace()

	if EnvNonOLM() {
		depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-operator-controller-manager", Namespace: installationNamespace}}

		deploymentFixture.SetEnv(depl, "manager", key, value)

		WaitForAllDeploymentsInTheNamespaceToBeReady(installationNamespace, k8sClient)

	} else if EnvCI() {

		sub, err := GetSubscriptionInEnvCIEnvironment(k8sClient)
		Expect(err).ToNot(HaveOccurred())
		Expect(sub).ToNot(BeNil())

		subscriptionFixture.SetEnv(sub, key, value)

		WaitForAllDeploymentsInTheNamespaceToBeReady(sub.Namespace, k8sClient)

	} else {

		sub := &olmv1alpha1.Subscription{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-operator", Namespace: installationNamespace}}
		Expect(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(sub), sub)).To(Succeed())

		subscriptionFixture.SetEnv(sub, key, value)

		WaitForAllDeploymentsInTheNamespaceToBeReady(sub.Namespace, k8sClient)

	}
}

// RemoveEnvFromOperatorSubscriptionOrDeployment will delete an environment variable from either operator Subscription or operator Deployment, depending on which mode the test is running in.
func RemoveEnvFromOperatorSubscriptionOrDeployment(key string) error {

	k8sClient, _, err := utils.GetE2ETestKubeClientWithError()
	if err != nil {
		return err
	}

	installationNamespace := GetInstallationNamespace()

	if EnvNonOLM() {
		depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-operator-controller-manager", Namespace: installationNamespace}}

		deploymentFixture.RemoveEnv(depl, "manager", key)

		WaitForAllDeploymentsInTheNamespaceToBeReady(installationNamespace, k8sClient)

	} else if EnvCI() {

		sub, err := GetSubscriptionInEnvCIEnvironment(k8sClient)
		if err != nil {
			return err
		}
		if sub == nil {
			return nil
		}

		subscriptionFixture.RemoveEnv(sub, key)

		WaitForAllDeploymentsInTheNamespaceToBeReady(sub.Namespace, k8sClient)

	} else {

		sub := &olmv1alpha1.Subscription{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-operator", Namespace: installationNamespace}}
		if err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(sub), sub); err != nil {
			return err
		}

		subscriptionFixture.RemoveEnv(sub, key)

		WaitForAllDeploymentsInTheNamespaceToBeReady(sub.Namespace, k8sClient)

	}
	return nil
}

func GetSubscriptionInEnvCIEnvironment(k8sClient client.Client) (*olmv1alpha1.Subscription, error) {
	subscriptionList := olmv1alpha1.SubscriptionList{}

	if err := k8sClient.List(context.Background(), &subscriptionList, client.InNamespace(GetInstallationNamespace())); err != nil {
		return nil, err
	}

	var sub *olmv1alpha1.Subscription

	for idx := range subscriptionList.Items {
		currsub := subscriptionList.Items[idx]

		if strings.HasPrefix(currsub.Name, "gitops-operator-") {
			sub = &currsub
		}
	}

	return sub, nil

}

// RestoreSubcriptionToDefault ensures that the Subscription (or Deployment env vars) are restored to a default state before each test.
func RestoreSubcriptionToDefault() {

	k8sClient, _, err := utils.GetE2ETestKubeClientWithError()
	Expect(err).ToNot(HaveOccurred())

	installationNamespace := GetInstallationNamespace()

	// optionalEnvVarsToRemove is a non-exhaustive list of environment variables that are known to be added to Subscription or operator Deployment by tests
	optionalEnvVarsToRemove := []string{"DISABLE_DEFAULT_ARGOCD_CONSOLELINK", "CONTROLLER_CLUSTER_ROLE", "SERVER_CLUSTER_ROLE", "ARGOCD_LABEL_SELECTOR", "ALLOW_NAMESPACE_MANAGEMENT_IN_NAMESPACE_SCOPED_INSTANCES", "IMAGE_PULL_POLICY"}

	if EnvNonOLM() {

		depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-operator-controller-manager", Namespace: installationNamespace}}

		for _, envKey := range optionalEnvVarsToRemove {
			deploymentFixture.RemoveEnv(depl, "manager", envKey)
		}

		err := waitForAllEnvVarsToBeRemovedFromDeployments(depl.Namespace, optionalEnvVarsToRemove, k8sClient)
		Expect(err).ToNot(HaveOccurred())

		Eventually(depl, "3m", "1s").Should(deploymentFixture.HaveReadyReplicas(1))

	} else if EnvCI() {

		sub, err := GetSubscriptionInEnvCIEnvironment(k8sClient)
		Expect(err).ToNot(HaveOccurred())

		if sub != nil {
			subscriptionFixture.RemoveSpecConfig(sub)
		}

		err = waitForAllEnvVarsToBeRemovedFromDeployments(installationNamespace, optionalEnvVarsToRemove, k8sClient)
		Expect(err).ToNot(HaveOccurred())

		WaitForAllDeploymentsInTheNamespaceToBeReady(installationNamespace, k8sClient)

	} else if EnvLocalRun() {
		// When running locally, there are no cluster resources to clean up

	} else {

		sub := &olmv1alpha1.Subscription{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-operator", Namespace: installationNamespace}}
		err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(sub), sub)
		Expect(err).ToNot(HaveOccurred())

		subscriptionFixture.RemoveSpecConfig(sub)

		err = waitForAllEnvVarsToBeRemovedFromDeployments(installationNamespace, optionalEnvVarsToRemove, k8sClient)
		Expect(err).ToNot(HaveOccurred())

		WaitForAllDeploymentsInTheNamespaceToBeReady(installationNamespace, k8sClient)

	}

}

// waitForAllEnvVarsToBeRemovedFromDeployments checks all Deployments in the Namespace, to ensure that none of those Deployments contain environment variables defined within envVarKeys.
// This can be used before a test starts to ensure that Operator or Argo CD containers are back to default state.
func waitForAllEnvVarsToBeRemovedFromDeployments(ns string, envVarKeys []string, k8sClient client.Client) error {

	Eventually(func() bool {
		var deplList appsv1.DeploymentList

		if err := k8sClient.List(context.Background(), &deplList, client.InNamespace(ns)); err != nil {
			GinkgoWriter.Println(err)
			return false
		}

		// For each Deployment in the list...
		for _, depl := range deplList.Items {

			// If at least one of the Deployments has not been observed, wait and try again
			if depl.Generation != depl.Status.ObservedGeneration {
				return false
			}

			// For each container of the deployment
			for _, container := range depl.Spec.Template.Spec.Containers {

				// For each env var we are looking for
				for _, envVarKey := range envVarKeys {

					for _, containerEnvKey := range container.Env {

						if containerEnvKey.Name == envVarKey {
							GinkgoWriter.Println("Waiting:", containerEnvKey, "is still present in Deployment ", depl.Name)
							return false
						}

					}
				}
			}
		}

		// All Deployments in NS are reconciled and ready
		return true

	}, "3m", "1s").Should(BeTrue())

	return nil
}

func WaitForAllDeploymentsInTheNamespaceToBeReady(ns string, k8sClient client.Client) {

	Eventually(func() bool {
		var deplList appsv1.DeploymentList

		if err := k8sClient.List(context.Background(), &deplList, client.InNamespace(ns)); err != nil {
			GinkgoWriter.Println(err)
			return false
		}

		for _, depl := range deplList.Items {

			// If at least one of the Deployments has not been observed, wait and try again
			if depl.Generation != depl.Status.ObservedGeneration {
				return false
			}

			if int64(depl.Status.Replicas) != int64(depl.Status.ReadyReplicas) {
				return false
			}

		}

		// All Deployments in NS are reconciled and ready
		return true

	}, "3m", "1s").Should(BeTrue())

	// The above logic will successfully wait for Deployments to be ready. However, this does not mean that the operator's controller logic has completed it's initial cluster reconciliation logic (starting a watch then reconciling existing resources)
	// - I'm not aware of a way to detect when this has completed, so instead I am inserting a 15 second pause.
	// - If anyone has a better way of doing this, let us know.
	// time.Sleep(15 * time.Second)
	// TODO: Uncomment this once the sequential test suite timeout has increased.
}

func WaitForAllStatefulSetsInTheNamespaceToBeReady(ns string, k8sClient client.Client) {

	Eventually(func() bool {
		var ssList appsv1.StatefulSetList

		if err := k8sClient.List(context.Background(), &ssList, client.InNamespace(ns)); err != nil {
			GinkgoWriter.Println(err)
			return false
		}

		for _, ss := range ssList.Items {

			// If at least one of the StatefulSets has not been observed, wait and try again
			if ss.Generation != ss.Status.ObservedGeneration {
				return false
			}

			if int64(ss.Status.Replicas) != int64(ss.Status.ReadyReplicas) {
				return false
			}

		}

		// All StatefulSets in NS are reconciled and ready
		return true

	}, "3m", "1s").Should(BeTrue())

}

func WaitForAllPodsInTheNamespaceToBeReady(ns string, k8sClient client.Client) {

	Eventually(func() bool {
		var podList corev1.PodList

		if err := k8sClient.List(context.Background(), &podList, client.InNamespace(ns)); err != nil {
			GinkgoWriter.Println(err)
			return false
		}

		for _, pod := range podList.Items {
			for _, containerStatus := range pod.Status.ContainerStatuses {

				if !containerStatus.Ready {
					GinkgoWriter.Println(pod.Name, "has container", containerStatus.Name, "which is not ready")
					return false
				}
			}

		}

		// All Pod in NS are ready
		return true

	}, "3m", "1s").Should(BeTrue())

}

// Delete all namespaces having a specific label used to identify namespaces that are created by e2e tests.
func ensureTestNamespacesDeleted(ctx context.Context, k8sClient client.Client) error {

	// fetch all namespaces having given label
	nsList, err := listE2ETestNamespaces(ctx, k8sClient)
	if err != nil {
		return fmt.Errorf("unable to delete test namespace: %w", err)
	}

	// delete selected namespaces
	for _, namespace := range nsList.Items {
		if err := deleteNamespaceAndVerify(ctx, namespace.Name, k8sClient); err != nil {
			return fmt.Errorf("unable to delete namespace '%s': %w", namespace.Name, err)
		}
	}
	return nil
}

// deleteNamespaceAndVerify deletes a namespace, and waits for it to be reported as deleted.
func deleteNamespaceAndVerify(ctx context.Context, namespaceParam string, k8sClient client.Client) error {

	GinkgoWriter.Println("Deleting Namespace", namespaceParam)

	// Delete the namespace:
	// - Issue a request to Delete the namespace
	// - Finally, we check if it has been deleted.
	if err := wait.PollUntilContextTimeout(ctx, time.Second*5, time.Minute*6, true, func(ctx context.Context) (done bool, err error) {
		// Delete the namespace, if it exists
		namespace := corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespaceParam,
			},
		}
		if err := k8sClient.Delete(ctx, &namespace); err != nil {
			if !apierr.IsNotFound(err) {
				GinkgoWriter.Printf("Unable to delete namespace '%s': %v\n", namespaceParam, err)
				return false, nil
			}
		}

		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(&namespace), &namespace); err != nil {
			if apierr.IsNotFound(err) {
				return true, nil
			} else {
				GinkgoWriter.Printf("Unable to Get namespace '%s': %v\n", namespaceParam, err)
				return false, nil
			}
		}

		return false, nil
	}); err != nil {
		return fmt.Errorf("namespace was never deleted, after delete was issued. '%s':%v", namespaceParam, err)
	}

	return nil
}

// Retrieve list of namespaces having a specific label used to identify namespaces that are created by e2e tests.
func listE2ETestNamespaces(ctx context.Context, k8sClient client.Client) (corev1.NamespaceList, error) {
	nsList := corev1.NamespaceList{}

	// set e2e label
	req, err := labels.NewRequirement(E2ETestLabelsKey, selection.Equals, []string{E2ETestLabelsValue})
	if err != nil {
		return nsList, fmt.Errorf("unable to set labels while fetching list of test namespace: %w", err)
	}

	// fetch all namespaces having given label
	err = k8sClient.List(ctx, &nsList, &client.ListOptions{LabelSelector: labels.NewSelector().Add(*req)})
	if err != nil {
		return nsList, fmt.Errorf("unable to fetch list of test namespace: %w", err)
	}
	return nsList, nil
}

// Update will keep trying to update object until it succeeds, or times out.
func updateWithoutConflict(obj client.Object, modify func(client.Object)) error {
	k8sClient, _, err := utils.GetE2ETestKubeClientWithError()
	if err != nil {
		return err
	}

	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Retrieve the latest version of the object
		err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(obj), obj)
		if err != nil {
			return err
		}

		modify(obj)

		// Attempt to update the object
		return k8sClient.Update(context.Background(), obj)
	})

	return err
}

type testReportEntry struct {
	isOutputted bool
}

var testReportLock sync.Mutex
var testReportMap = map[string]testReportEntry{} // acquire testReportLock before reading/writing to this map, or any values within this map

// OutputDebugOnFail can be used to debug a failing test: it will output the operator logs and namespace info
// Parameters:
// - Will output debug information on namespaces specified as parameters.
// - Namespace parameter may be a string, *Namespace, or Namespace
func OutputDebugOnFail(namespaceParams ...any) {

	// Convert parameter to string of namespace name:
	// - You can specify Namespace, *Namespae, or string, and we will convert it to string namespace
	namespaces := []string{}
	for _, param := range namespaceParams {

		if param == nil {
			continue
		}

		if str, isString := (param).(string); isString {
			namespaces = append(namespaces, str)

		} else if nsPtr, isNsPtr := (param).(*corev1.Namespace); isNsPtr {
			namespaces = append(namespaces, nsPtr.Name)

		} else if ns, isNs := (param).(corev1.Namespace); isNs {
			namespaces = append(namespaces, ns.Name)

		} else {
			Fail(fmt.Sprintf("unrecognized parameter value: %v", param))
		}
	}

	csr := CurrentSpecReport()

	if !csr.Failed() || os.Getenv("SKIP_DEBUG_OUTPUT") == "true" {
		return
	}

	testName := strings.Join(csr.ContainerHierarchyTexts, " ")
	testReportLock.Lock()
	defer testReportLock.Unlock()
	debugOutput, exists := testReportMap[testName]

	if exists && debugOutput.isOutputted {
		// Skip output if we have already outputted once for this test
		return
	}

	testReportMap[testName] = testReportEntry{
		isOutputted: true,
	}

	outputPodLog("openshift-gitops-operator-controller-manager")

	for _, namespace := range namespaces {

		kubectlOutput, err := osFixture.ExecCommandWithOutputParam(false, "kubectl", "get", "all", "-n", namespace)
		if err != nil {
			GinkgoWriter.Println("unable to list", namespace, err, kubectlOutput)
			continue
		}

		GinkgoWriter.Println("")
		GinkgoWriter.Println("----------------------------------------------------------------")
		GinkgoWriter.Println("'kubectl get all -n", namespace+"' output:")
		GinkgoWriter.Println(kubectlOutput)
		GinkgoWriter.Println("----------------------------------------------------------------")

		kubectlOutput, err = osFixture.ExecCommandWithOutputParam(false, "kubectl", "get", "deployments", "-n", namespace, "-o", "yaml")
		if err != nil {
			GinkgoWriter.Println("unable to list", namespace, err, kubectlOutput)
			continue
		}

		GinkgoWriter.Println("")
		GinkgoWriter.Println("----------------------------------------------------------------")
		GinkgoWriter.Println("'kubectl get deployments -n " + namespace + " -o yaml")
		GinkgoWriter.Println(kubectlOutput)
		GinkgoWriter.Println("----------------------------------------------------------------")

		kubectlOutput, err = osFixture.ExecCommandWithOutputParam(false, "kubectl", "get", "events", "-n", namespace)
		if err != nil {
			GinkgoWriter.Println("unable to get events for namespace", err, kubectlOutput)
		} else {
			GinkgoWriter.Println("")
			GinkgoWriter.Println("----------------------------------------------------------------")
			GinkgoWriter.Println("'kubectl get events -n " + namespace + ":")
			GinkgoWriter.Println(kubectlOutput)
			GinkgoWriter.Println("----------------------------------------------------------------")
		}

	}

	kubectlOutput, err := osFixture.ExecCommandWithOutputParam(false, "kubectl", "get", "argocds", "-A", "-o", "yaml")
	if err != nil {
		GinkgoWriter.Println("unable to output all argo cd statuses", err, kubectlOutput)
	} else {
		GinkgoWriter.Println("")
		GinkgoWriter.Println("----------------------------------------------------------------")
		GinkgoWriter.Println("'kubectl get argocds -A -o yaml':")
		GinkgoWriter.Println(kubectlOutput)
		GinkgoWriter.Println("----------------------------------------------------------------")
	}

	GinkgoWriter.Println("You can skip this debug output by setting 'SKIP_DEBUG_OUTPUT=true'")

}

// EnsureRunningOnOpenShift should be called if a test requires OpenShift (for example, it uses Route CR).
func EnsureRunningOnOpenShift() {

	runningOnOpenShift := RunningOnOpenShift()

	if !runningOnOpenShift {
		Skip("This test requires the cluster to be OpenShift")
		return
	}

	Expect(runningOnOpenShift).To(BeTrueBecause("this test is marked as requiring an OpenShift cluster, and we have detected the cluster is OpenShift"))

}

// RunningOnOpenShift returns true if the cluster is an OpenShift cluster, false otherwise.
func RunningOnOpenShift() bool {
	k8sClient, _ := utils.GetE2ETestKubeClient()

	crdList := crdv1.CustomResourceDefinitionList{}
	Expect(k8sClient.List(context.Background(), &crdList)).To(Succeed())

	openshiftAPIsFound := 0
	for _, crd := range crdList.Items {
		if strings.Contains(crd.Spec.Group, "openshift.io") {
			openshiftAPIsFound++
		}
	}
	return openshiftAPIsFound > 5 // I picked 5 as an arbitrary number, could also just be 1
}

//nolint:unused
func outputPodLog(podSubstring string) {
	k8sClient, _, err := utils.GetE2ETestKubeClientWithError()
	if err != nil {
		GinkgoWriter.Println(err)
		return
	}

	// List all pods on the cluster
	var podList corev1.PodList
	if err := k8sClient.List(context.Background(), &podList); err != nil {
		GinkgoWriter.Println(err)
		return
	}

	// Look specifically for operator pod
	matchingPods := []corev1.Pod{}
	for idx := range podList.Items {
		pod := podList.Items[idx]
		if strings.Contains(pod.Name, podSubstring) {
			matchingPods = append(matchingPods, pod)
		}
	}

	if len(matchingPods) == 0 {
		// This can happen when the operator is not running on the cluster
		GinkgoWriter.Println("DebugOutputOperatorLogs was called, but no pods were found.")
		return
	}

	if len(matchingPods) != 1 {
		GinkgoWriter.Println("unexpected number of operator pods", matchingPods)
		return
	}

	// Extract operator logs
	kubectlLogOutput, err := osFixture.ExecCommandWithOutputParam(false, "kubectl", "logs", "pod/"+matchingPods[0].Name, "manager", "-n", matchingPods[0].Namespace)
	if err != nil {
		GinkgoWriter.Println("unable to extract operator logs", err)
		return
	}

	// Output only the last 500 lines
	lines := strings.Split(kubectlLogOutput, "\n")

	startIndex := max(len(lines)-500, 0)

	GinkgoWriter.Println("")
	GinkgoWriter.Println("----------------------------------------------------------------")
	GinkgoWriter.Println("Log output from operator pod:")
	for _, line := range lines[startIndex:] {
		GinkgoWriter.Println(">", line)
	}
	GinkgoWriter.Println("----------------------------------------------------------------")

}

func IsUpstreamOperatorTests() bool {
	return false // This function should return true if running from argocd-operator repo, false if running from gitops-operator repo. This is to distinguish between tests in upstream argocd-operator and downstream gitops-operator repos.
}
