// test/openshift/e2e/ginkgo/sequential/1-120_validate_olm_operator_test.go
package sequential_test

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	olmv1 "github.com/operator-framework/api/pkg/operators/v1"             // Corrected import path
	olmv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1" // Corrected import path
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"

	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"              // Corrected import path
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/olm"          // Corrected import path
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/subscription" // Corrected import path
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"        // Corrected import path
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {
	Context("1-120_validate_olm_operator", Ordered, func() {
		var ctx context.Context
		var k8sClient client.Client
		var k8sScheme *k8sruntime.Scheme

		const helloworldSubscriptionName = "helloworld-operator-subscription"
		const helloworldSubscriptionNamespace = "openshift-operators"
		const helloworldOperatorDeploymentName = "helloworld-operator-controller-manager"
		const helloworldOperatorPodLabelKey = "control-plane"
		const helloworldOperatorPodLabelValue = "controller-manager"
		const helloworldOperatorPackageName = "helloworld-operator"
		const helloworldOperatorChannel = "stable"
		const helloworldOperatorSource = "redhat-operators"
		const helloworldOperatorSourceNamespace = "openshift-marketplace"
		const helloworldOperatorGroup = "global-operators"

		BeforeAll(func() {
			ctx = context.Background()
			var err error
			k8sClient, k8sScheme, err = utils.GetE2ETestKubeClientWithError()
			Expect(err).ToNot(HaveOccurred(), "Failed to get Kubernetes client in BeforeAll")

			fixture.EnsureSequentialCleanSlate()

			operatorGroup := &olmv1.OperatorGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      helloworldOperatorGroup,
					Namespace: helloworldSubscriptionNamespace,
				},
				Spec: olmv1.OperatorGroupSpec{
					TargetNamespaces: []string{},
				},
			}
			err = k8sClient.Create(ctx, operatorGroup)
			if err != nil && !apierr.IsAlreadyExists(err) {
				Expect(err).ToNot(HaveOccurred(), "Failed to create helloworld-operator OperatorGroup")
			}

			subToCreate := &olmv1alpha1.Subscription{
				ObjectMeta: metav1.ObjectMeta{
					Name:      helloworldSubscriptionName,
					Namespace: helloworldSubscriptionNamespace,
				},
				Spec: &olmv1alpha1.SubscriptionSpec{
					Channel:                helloworldOperatorChannel,
					Package:                helloworldOperatorPackageName,
					CatalogSource:          helloworldOperatorSource,
					CatalogSourceNamespace: helloworldOperatorSourceNamespace,
					InstallPlanApproval:    olmv1alpha1.ApprovalAutomatic,
				},
			}
			err = k8sClient.Create(ctx, subToCreate)
			if err != nil && !apierr.IsAlreadyExists(err) {
				Expect(err).ToNot(HaveOccurred(), "Failed to create helloworld-operator Subscription")
			}

			Eventually(func() bool {
				subKey := types.NamespacedName{Name: helloworldSubscriptionName, Namespace: helloworldSubscriptionNamespace}
				var createdSub olmv1alpha1.Subscription
				getErr := k8sClient.Get(ctx, subKey, &createdSub)
				return getErr == nil
			}).WithTimeout(30*time.Second).WithPolling(5*time.Second).Should(BeTrue(), "helloworld-operator Subscription did not appear after creation.")

			const installTimeout = 18 * time.Minute
			const pollingInterval = 5 * time.Second

			csvName := subscription.PollForSubscriptionCurrentCSV(
				ctx,
				helloworldSubscriptionNamespace,
				helloworldSubscriptionName,
				installTimeout,
				pollingInterval,
			)

			olm.WaitForClusterServiceVersion(
				ctx,
				helloworldSubscriptionNamespace,
				csvName,
				installTimeout,
				pollingInterval,
			)
		})

		BeforeEach(func() {

		})

		It("should validate the Subscription state is AtLatestKnown", func() {
			subKey := types.NamespacedName{Name: helloworldSubscriptionName, Namespace: helloworldSubscriptionNamespace}
			var sub olmv1alpha1.Subscription
			Eventually(func() olmv1alpha1.SubscriptionState {
				err := k8sClient.Get(ctx, subKey, &sub)
				Expect(err).ToNot(HaveOccurred(), "Failed to get Subscription during validation")
				return sub.Status.State
			}).WithTimeout(30*time.Second).WithPolling(5*time.Second).Should(Equal(olmv1alpha1.SubscriptionState("AtLatestKnown")),
				"Expected Subscription to be in 'AtLatestKnown' state")
		})

		It("should validate the helloworld-operator Deployment has 1 ready replica", func() {
			deploymentKey := types.NamespacedName{Name: helloworldOperatorDeploymentName, Namespace: helloworldSubscriptionNamespace}
			var deployment appsv1.Deployment
			Eventually(func() bool {
				err := k8sClient.Get(ctx, deploymentKey, &deployment)
				if err != nil {
					return false
				}
				return deployment.Status.Replicas == 1 && deployment.Status.ReadyReplicas == 1
			}).WithTimeout(2*time.Minute).WithPolling(5*time.Second).Should(BeTrue(),
				"Expected helloworld-operator Deployment to have 1 ready replica")
		})

		It("should validate at least one helloworld-operator Pod is running", func() {
			Eventually(func() bool {
				var podList corev1.PodList
				labelSelector := client.MatchingLabels{helloworldOperatorPodLabelKey: helloworldOperatorPodLabelValue}
				err := k8sClient.List(ctx, &podList, client.InNamespace(helloworldSubscriptionNamespace), labelSelector)
				if err != nil {
					return false
				}
				if len(podList.Items) == 0 {
					return false
				}
				for _, p := range podList.Items {
					if p.Status.Phase == corev1.PodRunning {
						return true
					}
				}
				return false
			}).WithTimeout(2*time.Minute).WithPolling(5*time.Second).Should(BeTrue(),
				"Expected at least one helloworld-operator pod to be in 'Running' phase")
		})

		AfterAll(func() {
			fixture.EnsureSequentialCleanSlate()

			// 1. DELETE Roles, RoleBindings
			deleteRBACResources(ctx, k8sClient, k8sScheme, "helloworld", helloworldSubscriptionNamespace)
			deleteRBACResources(ctx, k8sClient, k8sScheme, "helloworld", helloworldSubscriptionNamespace) // Deliberate double call

			// 2. DELETE Subscription
			subToDelete := &olmv1alpha1.Subscription{
				ObjectMeta: metav1.ObjectMeta{
					Name:      helloworldSubscriptionName,
					Namespace: helloworldSubscriptionNamespace,
				},
			}
			deleteK8sResource(ctx, k8sClient, subToDelete)

			// 3. DELETE CSVs
			var csvList olmv1alpha1.ClusterServiceVersionList
			err := k8sClient.List(ctx, &csvList, client.InNamespace(helloworldSubscriptionNamespace), client.MatchingLabels{"operators.coreos.com/helloworld-operator.openshift-operators": ""})
			if err == nil {
				for _, csv := range csvList.Items {
					if strings.HasPrefix(csv.Name, helloworldOperatorPackageName+".") {
						deleteK8sResource(ctx, k8sClient, &csv)
					}
				}
			} else if !apierr.IsNotFound(err) {
				GinkgoWriter.Printf("Error listing CSVs for cleanup: %v\n", err)
			}

			// 4. DELETE CRDs
			deleteCRDsWithPrefix(ctx, k8sClient, "helloworld")

			// 5. DELETE Namespaces
			deleteNamespacesWithPrefix(ctx, k8sClient, "helloworld")

			// 6. DELETE OperatorGroup (last, as it's a container for the operator)
			ogToDelete := &olmv1.OperatorGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      helloworldOperatorGroup,
					Namespace: helloworldSubscriptionNamespace,
				},
			}
			deleteK8sResource(ctx, k8sClient, ogToDelete)
		})
	})
})

// deleteK8sResource attempts to delete a Kubernetes resource.
// It includes a force-delete attempt if the initial deletion fails, and a short sleep.
// It does NOT wait for the resource to be gone.
func deleteK8sResource(ctx context.Context, k8sClient client.Client, obj client.Object) {
	// Attempt normal delete first
	err := k8sClient.Delete(ctx, obj, client.PropagationPolicy(metav1.DeletePropagationBackground))
	if err != nil && !apierr.IsNotFound(err) {
		forceDeleteOptions := &client.DeleteOptions{
			PropagationPolicy:  ptr.To(metav1.DeletePropagationBackground),
			GracePeriodSeconds: ptr.To[int64](0),
		}
		err = k8sClient.Delete(ctx, obj, forceDeleteOptions)
		if err != nil && !apierr.IsNotFound(err) {
			Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("Failed to force delete %s %s/%s", obj.GetObjectKind().GroupVersionKind().Kind, obj.GetNamespace(), obj.GetName()))
		}
	} else if apierr.IsNotFound(err) {
		return
	}

	time.Sleep(5 * time.Second)
}

// deleteRBACResources deletes RBAC resources with a given prefix.
func deleteRBACResources(ctx context.Context, k8sClient client.Client, scheme *k8sruntime.Scheme, prefix string, namespace string) {
	// ClusterRoles
	var crList rbacv1.ClusterRoleList
	if err := k8sClient.List(ctx, &crList); err == nil {
		for _, cr := range crList.Items {
			if strings.Contains(cr.Name, prefix) {
				deleteK8sResource(ctx, k8sClient, &cr)
			}
		}
	}

	// ClusterRoleBindings
	var crbList rbacv1.ClusterRoleBindingList
	if err := k8sClient.List(ctx, &crbList); err == nil {
		for _, crb := range crbList.Items {
			if strings.Contains(crb.Name, prefix) {
				deleteK8sResource(ctx, k8sClient, &crb)
			}
		}
	}

	// Roles (in specific namespace)
	var roleList rbacv1.RoleList
	if err := k8sClient.List(ctx, &roleList, client.InNamespace(namespace)); err == nil {
		for _, r := range roleList.Items {
			if strings.Contains(r.Name, prefix) {
				deleteK8sResource(ctx, k8sClient, &r)
			}
		}
	}

	// RoleBindings (in specific namespace)
	var rbList rbacv1.RoleBindingList
	if err := k8sClient.List(ctx, &rbList, client.InNamespace(namespace)); err == nil {
		for _, rb := range rbList.Items {
			if strings.Contains(rb.Name, prefix) {
				deleteK8sResource(ctx, k8sClient, &rb)
			}
		}
	}
}

// deleteCRDsWithPrefix deletes CRDs with a given prefix, handling finalizers.
func deleteCRDsWithPrefix(ctx context.Context, k8sClient client.Client, prefix string) {
	var crdList apiextensionsv1.CustomResourceDefinitionList
	if err := k8sClient.List(ctx, &crdList); err == nil {
		for _, crd := range crdList.Items {
			if strings.Contains(crd.Name, prefix) {
				if len(crd.ObjectMeta.Finalizers) > 0 {
					patch := client.MergeFrom(crd.DeepCopy())
					crd.ObjectMeta.Finalizers = []string{}
					if patchErr := k8sClient.Patch(ctx, &crd, patch); patchErr != nil {
						GinkgoWriter.Printf("Error patching CRD '%s' to remove finalizers: %v\n", crd.Name, patchErr)
					}
				}
				deleteK8sResource(ctx, k8sClient, &crd)
			}
		}
	}
}

// deleteNamespacesWithPrefix deletes Namespaces with a given prefix, handling finalizers.
func deleteNamespacesWithPrefix(ctx context.Context, k8sClient client.Client, prefix string) {
	var nsList corev1.NamespaceList
	if err := k8sClient.List(ctx, &nsList); err == nil {
		for _, ns := range nsList.Items {
			if strings.Contains(ns.Name, prefix) {
				if len(ns.ObjectMeta.Finalizers) > 0 {
					patch := client.MergeFrom(ns.DeepCopy())
					ns.ObjectMeta.Finalizers = []string{}
					if patchErr := k8sClient.Patch(ctx, &ns, patch); patchErr != nil {
						GinkgoWriter.Printf("Error patching Namespace '%s' to remove finalizers: %v\n", ns.Name, patchErr)
					}
				}
				deleteK8sResource(ctx, k8sClient, &ns)
			}
		}
	}
}
