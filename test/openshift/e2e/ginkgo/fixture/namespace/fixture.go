package namespace

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	matcher "github.com/onsi/gomega/types"

	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func HavePhase(expectedPhase corev1.NamespacePhase) matcher.GomegaMatcher {
	return fetchNamespace(func(ns *corev1.Namespace) bool {
		GinkgoWriter.Println("Namespace - HavePhase: Expected:", expectedPhase, "Actual:", ns.Status.Phase)
		return ns.Status.Phase == expectedPhase
	})
}

// Update will keep trying to update object until it succeeds, or times out.
func Update(obj *corev1.Namespace, modify func(*corev1.Namespace)) {
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

// This is intentionally NOT exported, for now. Create another function in this file/package that calls this function, and export that.
func fetchNamespace(f func(*corev1.Namespace) bool) matcher.GomegaMatcher {

	return WithTransform(func(depl *corev1.Namespace) bool {

		k8sClient, _, err := utils.GetE2ETestKubeClientWithError()
		if err != nil {
			GinkgoWriter.Println(err)
			return false
		}

		err = k8sClient.Get(context.Background(), client.ObjectKeyFromObject(depl), depl)
		if err != nil {
			GinkgoWriter.Println(err)
			return false
		}

		return f(depl)

	}, BeTrue())

}
