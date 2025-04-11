package k8s

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/client-go/util/retry"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	matcher "github.com/onsi/gomega/types"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

func HaveLabelWithValue(key string, value string) matcher.GomegaMatcher {

	return WithTransform(func(k8sObject client.Object) bool {
		k8sClient, _, err := utils.GetE2ETestKubeClientWithError()
		if err != nil {
			GinkgoWriter.Println(err)
		}

		err = k8sClient.Get(context.Background(), client.ObjectKeyFromObject(k8sObject), k8sObject)
		if err != nil {
			GinkgoWriter.Println("HasLabelWithValue:", err)
			return false
		}

		labels := k8sObject.GetLabels()
		if labels == nil {
			return false
		}

		return labels[key] == value

	}, BeTrue())
}

func NotHaveLabelWithValue(key string, value string) matcher.GomegaMatcher {

	return WithTransform(func(k8sObject client.Object) bool {
		k8sClient, _, err := utils.GetE2ETestKubeClientWithError()
		if err != nil {
			GinkgoWriter.Println(err)
			return false
		}

		err = k8sClient.Get(context.Background(), client.ObjectKeyFromObject(k8sObject), k8sObject)
		if err != nil {
			GinkgoWriter.Println("DoesNotHaveLabelWithValue:", err)
			return false
		}

		labels := k8sObject.GetLabels()
		if labels == nil {
			return true
		}

		return labels[key] != value

	}, BeTrue())
}

// ExistByName checks if the given k8s resource exists, when retrieving it by name/namespace.
// - It does NOT check if the resource content matches. It only checks that a resource of that type and name exists.
func ExistByName() matcher.GomegaMatcher {

	return WithTransform(func(k8sObject client.Object) bool {
		k8sClient, _, err := utils.GetE2ETestKubeClientWithError()
		if err != nil {
			GinkgoWriter.Println(err)
			return false
		}

		err = k8sClient.Get(context.Background(), client.ObjectKeyFromObject(k8sObject), k8sObject)
		if err != nil {
			GinkgoWriter.Println("Object does not exists in ExistByName:", k8sObject.GetName(), err)
		} else {
			GinkgoWriter.Println("Object exists in ExistByName:", k8sObject.GetName())
		}
		return err == nil
	}, BeTrue())
}

// NotExistByName checks if the given resource does not exist, when retrieving it by name/namespace.
// Does NOT check if the resource content matches.
func NotExistByName() matcher.GomegaMatcher {

	return WithTransform(func(k8sObject client.Object) bool {
		k8sClient, _, err := utils.GetE2ETestKubeClientWithError()
		if err != nil {
			GinkgoWriter.Println(err)
			return false
		}

		err = k8sClient.Get(context.Background(), client.ObjectKeyFromObject(k8sObject), k8sObject)
		if apierrors.IsNotFound(err) {
			return true
		} else {
			if err != nil {
				GinkgoWriter.Println(err)
			}
			return false
		}
	}, BeTrue())
}

// Update will keep trying to update object until it succeeds, or times out.
func Update(obj client.Object, modify func(client.Object)) {
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
