package node

import (
	"context"
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"

	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
)

// ExpectHasAtLeastXNodes expects the cluster eventually have X nodes. This can be useful for tests that require multiple nodes for HA.
func ExpectHasAtLeastXNodes(expectedNodesOnCluster int) {
	k8sClient, _ := utils.GetE2ETestKubeClient()

	// You can set 'SKIP_HA_TESTS=true' env var if you are running on a cluster with < 3 nodes.
	skipHATestsVal := os.Getenv("SKIP_HA_TESTS")
	if strings.TrimSpace(strings.ToLower(skipHATestsVal)) == "true" {
		Skip("Skipping test that requires multiple nodes, because SKIP_HA_TESTS is set")
		return
	}

	Eventually(func() bool {
		var nodeList corev1.NodeList

		err := k8sClient.List(context.Background(), &nodeList)
		if err != nil {
			GinkgoWriter.Println(err)
			return false
		}

		GinkgoWriter.Println("ExpectHasAtLeastXNodes, expected:", expectedNodesOnCluster, "actual:", len(nodeList.Items))
		return len(nodeList.Items) >= expectedNodesOnCluster

	}, "3m", "10s").Should(BeTrue())

}
