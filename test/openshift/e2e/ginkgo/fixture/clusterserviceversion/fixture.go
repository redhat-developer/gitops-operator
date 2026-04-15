package clusterserviceversion

import (
	"context"
	"strings"

	. "github.com/onsi/gomega"
	olmv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Update will update a ClusterServiceVersion CR. Update will keep trying to update object until it succeeds, or times out.
func Update(obj *olmv1alpha1.ClusterServiceVersion, modify func(*olmv1alpha1.ClusterServiceVersion)) {
	k8sClient, _ := utils.GetE2ETestKubeClient()

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Retrieve the latest version of the object
		err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(obj), obj)
		if err != nil {
			return err
		}

		modify(obj)

		// Attempt to update the object
		return k8sClient.Update(context.Background(), obj)
	})
	Expect(err).ToNot(HaveOccurred())
}

func Get(ctx context.Context, k8sClient client.Client) *olmv1alpha1.ClusterServiceVersion {
	var csvList olmv1alpha1.ClusterServiceVersionList
	Expect(k8sClient.List(ctx, &csvList, client.InNamespace("openshift-gitops-operator"))).To(Succeed())
	for idx := range csvList.Items {
		idxCSV := csvList.Items[idx]
		if strings.Contains(idxCSV.Name, "gitops-operator") {
			return &idxCSV
		}
	}
	return nil
}
