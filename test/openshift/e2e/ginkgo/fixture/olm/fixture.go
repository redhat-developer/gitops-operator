package olm

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	olmv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	apierr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
)

// WaitForClusterServiceVersion waits for a specific ClusterServiceVersion to reach the 'Succeeded' phase.
func WaitForClusterServiceVersion(ctx context.Context, namespace, csvName string, timeout, pollingInterval time.Duration) {
	k8sClient, _, err := utils.GetE2ETestKubeClientWithError() // Get client here
	Expect(err).ToNot(HaveOccurred())

	GinkgoWriter.Printf("Waiting for ClusterServiceVersion '%s' in namespace '%s' to be Succeeded...\n", csvName, namespace)

	csvKey := types.NamespacedName{
		Name:      csvName,
		Namespace: namespace,
	}

	var foundCSV olmv1alpha1.ClusterServiceVersion

	Eventually(func() bool {
		getErr := k8sClient.Get(ctx, csvKey, &foundCSV)
		if getErr != nil {
			if apierr.IsNotFound(getErr) {
				GinkgoWriter.Printf("CSV '%s' not found yet. Listing all CSVs in '%s' for debug...\n", csvName, namespace)
				var csvList olmv1alpha1.ClusterServiceVersionList
				listErr := k8sClient.List(ctx, &csvList, client.InNamespace(namespace))
				if listErr != nil {
					GinkgoWriter.Printf("Error listing CSVs for debug: %v\n", listErr)
				} else {
					for _, csv := range csvList.Items {
						GinkgoWriter.Printf("- Found CSV: %s (Phase: %s, Reason: %s)\n", csv.Name, csv.Status.Phase, csv.Status.Reason)
					}
				}
				return false
			}
			GinkgoWriter.Printf("Error getting CSV '%s': %v. Retrying...\n", csvName, getErr)
			return false // retrying on errors
		}

		// Check if the CSV phase is Succeeded
		if foundCSV.Status.Phase == olmv1alpha1.CSVPhaseSucceeded {
			GinkgoWriter.Printf("ClusterServiceVersion '%s' is Succeeded.\n", csvName)
			return true
		}

		GinkgoWriter.Printf("CSV '%s' status is '%s' (Reason: %s). Waiting...\n", csvName, foundCSV.Status.Phase, foundCSV.Status.Reason)
		return false // Not succeeded yet
	}).WithTimeout(timeout).WithPolling(pollingInterval).Should(BeTrue(),
		fmt.Sprintf("Expected ClusterServiceVersion '%s' in namespace '%s' to be Succeeded within %s", csvName, namespace, timeout))

	GinkgoWriter.Println("ClusterServiceVersion successfully installed and Succeeded.")
}
