package application

import (
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	"k8s.io/client-go/util/retry"

	appv1alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	"github.com/argoproj/gitops-engine/pkg/health"
	"github.com/argoproj/gitops-engine/pkg/sync/common"
	matcher "github.com/onsi/gomega/types"

	. "github.com/onsi/ginkgo/v2"

	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// This is intentionally NOT exported, for now. Create another function in this file/package that calls this function, and export that.
func expectedCondition(f func(app *appv1alpha1.Application) bool) matcher.GomegaMatcher {

	return WithTransform(func(app *appv1alpha1.Application) bool {

		k8sClient, _, err := utils.GetE2ETestKubeClientWithError()
		if err != nil {
			GinkgoWriter.Println(err)
			return false
		}

		err = k8sClient.Get(context.Background(), client.ObjectKeyFromObject(app), app)
		if err != nil {
			GinkgoWriter.Println(err)
			return false
		}

		return f(app)

	}, BeTrue())

}

func HaveOperationStatePhase(expectedPhase common.OperationPhase) matcher.GomegaMatcher {

	return expectedCondition(func(app *appv1alpha1.Application) bool {

		var currStatePhase string

		if app.Status.OperationState != nil {
			currStatePhase = string(app.Status.OperationState.Phase)
		}

		GinkgoWriter.Println("HaveOperationStatePhase - current phase:", currStatePhase, " / expected phase:", expectedPhase)

		return currStatePhase == string(expectedPhase)

	})

}

func HaveHealthStatusCode(expectedHealth health.HealthStatusCode) matcher.GomegaMatcher {

	return expectedCondition(func(app *appv1alpha1.Application) bool {

		GinkgoWriter.Println("HaveHealthStatusCode - current health:", app.Status.Health.Status, " / expected health:", expectedHealth)

		return app.Status.Health.Status == expectedHealth

	})

}

// HaveSyncStatusCode waits for Argo CD to have the given sync status
func HaveSyncStatusCode(expected appv1alpha1.SyncStatusCode) matcher.GomegaMatcher {

	return expectedCondition(func(app *appv1alpha1.Application) bool {

		GinkgoWriter.Println("HaveSyncStatusCode - current syncStatusCode:", app.Status.Sync.Status, " / expected syncStatusCode:", expected)

		return app.Status.Sync.Status == expected

	})

}

// Update will keep trying to update object until it succeeds, or times out.
func Update(obj *appv1alpha1.Application, modify func(*appv1alpha1.Application)) {
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

// Update will keep trying to update object until it succeeds, or times out.
func UpdateWithError(obj *appv1alpha1.Application, modify func(*appv1alpha1.Application)) error {
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

	return err
}
