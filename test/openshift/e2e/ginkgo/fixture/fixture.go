package fixture

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	routev1 "github.com/openshift/api/route/v1"
	securityv1 "github.com/openshift/api/security/v1"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rolloutmanagerv1alpha1 "github.com/argoproj-labs/argo-rollouts-manager/api/v1alpha1"
	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	olmv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"

	"github.com/onsi/gomega/format"
	gitopsoperatorv1alpha1 "github.com/redhat-developer/gitops-operator/api/v1alpha1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	deploymentFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	subscriptionFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/subscription"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	LabelsKey   = "app"
	LabelsValue = "test-argo-app"
)

var NamespaceLabels = map[string]string{LabelsKey: LabelsValue}

func EnsureParallelCleanSlate() {

	// Increase the maximum length of debug output, for when tests fail
	format.MaxLength = 16 * 1024
	SetDefaultEventuallyTimeout(time.Second * 30)
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
	format.MaxLength = 16 * 1024
	SetDefaultEventuallyTimeout(time.Second * 30)
	SetDefaultEventuallyPollingInterval(time.Second * 3)
	SetDefaultConsistentlyDuration(time.Second * 10)
	SetDefaultConsistentlyPollingInterval(time.Second * 1)

	ctx := context.Background()
	k8sClient, _ := utils.GetE2ETestKubeClient()

	// If the CSV in 'openshift-gitops-operator' NS exists, make sure the CSV does not contain the dynamic plugin env var
	if err := RemoveDynamicPluginFromCSV(ctx, k8sClient); err != nil {
		return err
	}

	if err := RestoreSubcriptionToDefault(); err != nil {
		return err
	}

	// ensure namespaces created during test are deleted
	err := ensureTestNamespacesDeleted(ctx, k8sClient)
	if err != nil {
		return err
	}

	defaultOpenShiftGitOpsArgoCD := &argov1beta1api.ArgoCD{
		ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops", Namespace: "openshift-gitops"},
	}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(defaultOpenShiftGitOpsArgoCD), defaultOpenShiftGitOpsArgoCD); err != nil {
		return err
	}
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
			TLS: &routev1.TLSConfig{
				Termination:                   routev1.TLSTerminationReencrypt,
				InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyRedirect,
			},
		}

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
	Expect(k8sClient.List(ctx, &csvList, client.InNamespace("openshift-gitops-operator"))).To(Succeed())

	for idx := range csvList.Items {
		idxCSV := csvList.Items[idx]
		if strings.Contains(idxCSV.Name, "gitops-operator") {
			csv = &idxCSV
			break
		}
	}
	Expect(csv).ToNot(BeNil())

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

func CreateRandomE2ETestNamespace() corev1.Namespace {

	randomVal := string(uuid.NewUUID())
	randomVal = randomVal[0:13] // Only use 14 characters of randomness. If we use more, then we start to hit limits on parts of code which limit # of characters to 63

	testNamespaceName := "gitops-e2e-test-" + randomVal

	ns := CreateNamespace(string(testNamespaceName))
	return ns
}

func CreateRandomE2ETestNamespaceWithCleanupFunc() (corev1.Namespace, func()) {

	ns := CreateRandomE2ETestNamespace()
	return ns, nsDeletionFunc(&ns)
}

// Create namespace for tests having a specific label for identification
// - If the namespace already exists, it will be deleted first
func CreateNamespace(name string) corev1.Namespace {

	k8sClient, _ := utils.GetE2ETestKubeClient()

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	// If the Namespace already exists, delete it first
	if err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(ns), ns); err == nil {
		// Namespace exists, so delete it first
		Expect(deleteNamespace(context.Background(), ns.Name, k8sClient)).To(Succeed())
	}

	ns = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name:   name,
		Labels: NamespaceLabels,
	}}

	err := k8sClient.Create(context.Background(), ns)
	Expect(err).ToNot(HaveOccurred())

	return *ns
}

func CreateNamespaceWithCleanupFunc(name string) (corev1.Namespace, func()) {

	ns := CreateNamespace(name)
	return ns, nsDeletionFunc(&ns)
}

// Create a namespace 'name' that is managed by another namespace 'managedByNamespace', via managed-by label.
func CreateManagedNamespace(name string, managedByNamespace string) corev1.Namespace {
	k8sClient, _ := utils.GetE2ETestKubeClient()

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}

	// If the Namespace already exists, delete it first
	if err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(ns), ns); err == nil {
		// Namespace exists, so delete it first
		Expect(deleteNamespace(context.Background(), ns.Name, k8sClient)).To(Succeed())
	}

	ns = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name: name,
		Labels: map[string]string{
			LabelsKey:                       LabelsValue,
			"argocd.argoproj.io/managed-by": managedByNamespace,
		},
	}}

	Expect(k8sClient.Create(context.Background(), ns)).To(Succeed())

	return *ns

}

func CreateManagedNamespaceWithCleanupFunc(name string, managedByNamespace string) (corev1.Namespace, func()) {
	ns := CreateManagedNamespace(name, managedByNamespace)
	return ns, nsDeletionFunc(&ns)
}

// nsDeletionFunc is a convenience function that returns a function that deletes a namespace. This is used for Namespace cleanup by other functions.
func nsDeletionFunc(ns *corev1.Namespace) func() {

	return func() {

		// If you are debugging an E2E test and want to prevent its namespace from being deleted when the test ends (so that you can examine the state of resources in the namespace) you can set E2E_DEBUG_SKIP_CLEANUP env var.
		if os.Getenv("E2E_DEBUG_SKIP_CLEANUP") != "" {
			GinkgoWriter.Println("Skipping namespace cleanup as E2E_DEBUG_SKIP_CLEANUP is set")
			return
		}

		k8sClient, _, err := utils.GetE2ETestKubeClientWithError()
		Expect(err).ToNot(HaveOccurred())
		err = k8sClient.Delete(context.Background(), ns, &client.DeleteOptions{PropagationPolicy: ptr.To(metav1.DeletePropagationForeground)})

		// Error shouldn't occur, UNLESS it's because the NS no longer exists
		if err != nil && !apierr.IsNotFound(err) {
			Expect(err).ToNot(HaveOccurred())
		}
	}

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

	if EnvNonOLM() {
		depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-operator-controller-manager", Namespace: "openshift-gitops-operator"}}

		return deploymentFixture.GetEnv(depl, key)

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

		sub := &olmv1alpha1.Subscription{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-operator", Namespace: "openshift-gitops-operator"}}
		if err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(sub), sub); err != nil {
			return nil, err
		}

		return subscriptionFixture.GetEnv(sub, key)

	}

}

// SetEnvInOperatorSubscriptionOrDeployment will set the value of an environment variable, in either operator Subscription or operator Deployment, depending on which mode the test is running in.
func SetEnvInOperatorSubscriptionOrDeployment(key string, value string) {

	k8sClient, _ := utils.GetE2ETestKubeClient()

	if EnvNonOLM() {
		depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-operator-controller-manager", Namespace: "openshift-gitops-operator"}}

		deploymentFixture.SetEnv(depl, key, value)

	} else if EnvCI() {

		sub, err := GetSubscriptionInEnvCIEnvironment(k8sClient)
		Expect(err).ToNot(HaveOccurred())
		Expect(sub).ToNot(BeNil())

		subscriptionFixture.SetEnv(sub, key, value)

	} else {

		sub := &olmv1alpha1.Subscription{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-operator", Namespace: "openshift-gitops-operator"}}
		Expect(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(sub), sub)).To(Succeed())

		subscriptionFixture.SetEnv(sub, key, value)
	}
}

// RemoveEnvFromOperatorSubscriptionOrDeployment will delete an environment variable from either operator Subscription or operator Deployment, depending on which mode the test is running in.
func RemoveEnvFromOperatorSubscriptionOrDeployment(key string) error {

	k8sClient, _, err := utils.GetE2ETestKubeClientWithError()
	if err != nil {
		return err
	}

	if EnvNonOLM() {
		depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-operator-controller-manager", Namespace: "openshift-gitops-operator"}}

		deploymentFixture.RemoveEnv(depl, key)

	} else if EnvCI() {

		sub, err := GetSubscriptionInEnvCIEnvironment(k8sClient)
		if err != nil {
			return err
		}
		if sub == nil {
			return nil
		}

		subscriptionFixture.RemoveEnv(sub, key)

	} else {

		sub := &olmv1alpha1.Subscription{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-operator", Namespace: "openshift-gitops-operator"}}
		if err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(sub), sub); err != nil {
			return err
		}

		subscriptionFixture.RemoveEnv(sub, key)

	}
	return nil
}

func GetSubscriptionInEnvCIEnvironment(k8sClient client.Client) (*olmv1alpha1.Subscription, error) {
	subscriptionList := olmv1alpha1.SubscriptionList{}
	if err := k8sClient.List(context.Background(), &subscriptionList, client.InNamespace("openshift-gitops-operator")); err != nil {
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
func RestoreSubcriptionToDefault() error {

	k8sClient, _, err := utils.GetE2ETestKubeClientWithError()
	if err != nil {
		return err
	}

	// optionalEnvVarsToRemove is a non-exhaustive list of environment variables that are known to be added to Subscription or operator Deployment by tests
	optionalEnvVarsToRemove := []string{"DISABLE_DEFAULT_ARGOCD_CONSOLELINK", "CONTROLLER_CLUSTER_ROLE", "SERVER_CLUSTER_ROLE", "ARGOCD_LABEL_SELECTOR"}

	if EnvNonOLM() {

		depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-operator-controller-manager", Namespace: "openshift-gitops-operator"}}

		for _, envKey := range optionalEnvVarsToRemove {
			deploymentFixture.RemoveEnv(depl, envKey)
		}

		if err := waitForAllEnvVarsToBeRemovedFromDeployments(depl.Namespace, optionalEnvVarsToRemove, k8sClient); err != nil {
			return err
		}

		Eventually(depl, "3m", "1s").Should(deploymentFixture.HaveReadyReplicas(1))

	} else if EnvCI() {

		sub, err := GetSubscriptionInEnvCIEnvironment(k8sClient)
		if err != nil {
			return err
		}

		if sub != nil {
			subscriptionFixture.RemoveSpecConfig(sub)
		}

		if err := waitForAllEnvVarsToBeRemovedFromDeployments("openshift-gitops-operator", optionalEnvVarsToRemove, k8sClient); err != nil {
			return err
		}

		WaitForAllDeploymentsInTheNamespaceToBeReady("openshift-gitops-operator", k8sClient)

	} else if EnvLocalRun() {
		// When running locally, there are no cluster resources to clean up
		return nil

	} else {

		sub := &olmv1alpha1.Subscription{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-operator", Namespace: "openshift-gitops-operator"}}
		if err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(sub), sub); err != nil {
			return err
		}

		subscriptionFixture.RemoveSpecConfig(sub)

		if err := waitForAllEnvVarsToBeRemovedFromDeployments("openshift-gitops-operator", optionalEnvVarsToRemove, k8sClient); err != nil {
			return err
		}

		WaitForAllDeploymentsInTheNamespaceToBeReady("openshift-gitops-operator", k8sClient)
	}

	return nil

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
		if err := deleteNamespace(ctx, namespace.Name, k8sClient); err != nil {
			return fmt.Errorf("unable to delete namespace '%s': %w", namespace.Name, err)
		}
	}
	return nil
}

// deleteNamespace deletes a namespace, and waits for it to be reported as deleted.
func deleteNamespace(ctx context.Context, namespaceParam string, k8sClient client.Client) error {

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
	req, err := labels.NewRequirement(LabelsKey, selection.Equals, []string{LabelsValue})
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
