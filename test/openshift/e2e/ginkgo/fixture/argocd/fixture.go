package argocd

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	//lint:ignore ST1001 "This is a common practice in Gomega tests for readability."
	. "github.com/onsi/ginkgo/v2" //nolint:all
	//lint:ignore ST1001 "This is a common practice in Gomega tests for readability."
	. "github.com/onsi/gomega" //nolint:all

	matcher "github.com/onsi/gomega/types"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Update will update an ArgoCD CR. Update will keep trying to update object until it succeeds, or times out.
func Update(obj *argov1beta1api.ArgoCD, modify func(*argov1beta1api.ArgoCD)) {
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

	// After we update ArgoCD CR, we should wait a few moments for the operator to reconcile the change.
	// - Ideally, the ArgoCD CR would have a .status field that we could read, that would indicate which resource version/generation had been reconciled.
	// - Sadly, this does not exist, so we instead must use time.Sleep() (for now)
	time.Sleep(7 * time.Second)
}

func GetOpenShiftGitOpsNSArgoCD() (*argov1beta1api.ArgoCD, error) {

	k8sClient, _ := utils.GetE2ETestKubeClient()

	argoCD := argov1beta1api.ArgoCD{
		ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops", Namespace: "openshift-gitops"},
	}

	err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(&argoCD), &argoCD)

	return &argoCD, err

}

// BeAvailable waits for Argo CD instance to have .status.phase of 'Available'
func BeAvailable() matcher.GomegaMatcher {
	return BeAvailableWithCustomSleepTime(10 * time.Second)
}

// In most cases, you should probably just use 'BeAvailable'.
func BeAvailableWithCustomSleepTime(sleepTime time.Duration) matcher.GomegaMatcher {

	// Wait X seconds to allow operator to reconcile the ArgoCD CR, before we start checking if it's ready
	// - We do this so that any previous calls to update the ArgoCD CR have been reconciled by the operator, before we wait to see if ArgoCD has become available.
	// - I'm not aware of a way to do this without a sleep statement, but when we have something better we should do that instead.
	time.Sleep(sleepTime)

	return fetchArgoCD(func(argocd *argov1beta1api.ArgoCD) bool {

		if argocd.Status.Phase != "Available" {
			GinkgoWriter.Println("ArgoCD status is not yet Available")
			return false
		}
		GinkgoWriter.Println("ArgoCD status is now", argocd.Status.Phase)

		return true
	})
}

func HavePhase(phase string) matcher.GomegaMatcher {
	return fetchArgoCD(func(argocd *argov1beta1api.ArgoCD) bool {
		GinkgoWriter.Println("HavePhase:", "expected:", phase, "/ actual:", argocd.Status.Phase)
		return argocd.Status.Phase == phase
	})
}

func HaveRedisStatus(status string) matcher.GomegaMatcher {
	return fetchArgoCD(func(argocd *argov1beta1api.ArgoCD) bool {
		GinkgoWriter.Println("HaveRedisStatus:", "expected:", status, "/ actual:", argocd.Status.Redis)
		return argocd.Status.Redis == status
	})
}

func HaveRepoStatus(status string) matcher.GomegaMatcher {
	return fetchArgoCD(func(argocd *argov1beta1api.ArgoCD) bool {
		GinkgoWriter.Println("HaveRepoStatus:", "expected:", status, "/ actual:", argocd.Status.Repo)
		return argocd.Status.Repo == status
	})
}

func HaveServerStatus(status string) matcher.GomegaMatcher {
	return fetchArgoCD(func(argocd *argov1beta1api.ArgoCD) bool {
		GinkgoWriter.Println("HaveServerStatus:", "expected:", status, "/ actual:", argocd.Status.Server)
		return argocd.Status.Server == status
	})
}

func HaveApplicationControllerStatus(status string) matcher.GomegaMatcher {
	return fetchArgoCD(func(argocd *argov1beta1api.ArgoCD) bool {
		GinkgoWriter.Println("HaveApplicationControllerStatus:", "expected:", status, "/ actual:", argocd.Status.ApplicationController)
		return argocd.Status.ApplicationController == status
	})
}

func HaveApplicationSetControllerStatus(status string) matcher.GomegaMatcher {
	return fetchArgoCD(func(argocd *argov1beta1api.ArgoCD) bool {
		GinkgoWriter.Println("HaveApplicationSetControllerStatus:", "expected:", status, "/ actual:", argocd.Status.ApplicationSetController)
		return argocd.Status.ApplicationSetController == status
	})
}

func HaveNotificationControllerStatus(status string) matcher.GomegaMatcher {
	return fetchArgoCD(func(argocd *argov1beta1api.ArgoCD) bool {
		GinkgoWriter.Println("HaveNotificationControllerStatus:", "expected:", status, "/ actual:", argocd.Status.NotificationsController)
		return argocd.Status.NotificationsController == status
	})
}

func HaveSSOStatus(status string) matcher.GomegaMatcher {
	return fetchArgoCD(func(argocd *argov1beta1api.ArgoCD) bool {
		GinkgoWriter.Println("HaveSSOStatus:", "expected:", status, "/ actual:", argocd.Status.SSO)
		return argocd.Status.SSO == status
	})
}

func HaveHost(host string) matcher.GomegaMatcher {
	return fetchArgoCD(func(argocd *argov1beta1api.ArgoCD) bool {
		GinkgoWriter.Println("HaveHost:", "expected:", host, "/ actual:", argocd.Status.Host)
		return argocd.Status.Host == host
	})
}

func HaveApplicationControllerOperationProcessors(operationProcessors int) matcher.GomegaMatcher {
	return fetchArgoCD(func(argocd *argov1beta1api.ArgoCD) bool {
		GinkgoWriter.Println("HaveApplicationControllerOperationProcessors:", "Expected:", operationProcessors, "/ actual:", argocd.Spec.Controller.Processors.Operation)
		return int(argocd.Spec.Controller.Processors.Operation) == operationProcessors
	})
}

func HaveCondition(condition metav1.Condition) matcher.GomegaMatcher {
	return fetchArgoCD(func(argocd *argov1beta1api.ArgoCD) bool {

		if len(argocd.Status.Conditions) != 1 {
			GinkgoWriter.Println("HaveCondition: length is zero")
			return false
		}

		instanceCondition := argocd.Status.Conditions[0]

		GinkgoWriter.Println("HaveCondition - Message:", instanceCondition.Message, condition.Message)
		if instanceCondition.Message != condition.Message {
			GinkgoWriter.Println("HaveCondition: message does not match")
			return false
		}

		GinkgoWriter.Println("HaveCondition - Reason:", instanceCondition.Reason, condition.Reason)
		if instanceCondition.Reason != condition.Reason {
			GinkgoWriter.Println("HaveCondition: reason does not match")
			return false
		}

		GinkgoWriter.Println("HaveCondition - Status:", instanceCondition.Status, condition.Status)
		if instanceCondition.Status != condition.Status {
			GinkgoWriter.Println("HaveCondition: status does not match")
			return false
		}

		GinkgoWriter.Println("HaveCondition - Type:", instanceCondition.Type, condition.Type)
		if instanceCondition.Type != condition.Type {
			GinkgoWriter.Println("HaveCondition: type does not match")
			return false
		}

		return true

	})
}

// This is intentionally NOT exported, for now. Create another function in this file/package that calls this function, and export that.
func fetchArgoCD(f func(*argov1beta1api.ArgoCD) bool) matcher.GomegaMatcher {

	return WithTransform(func(argocd *argov1beta1api.ArgoCD) bool {

		k8sClient, _, err := utils.GetE2ETestKubeClientWithError()
		if err != nil {
			GinkgoWriter.Println(err)
			return false
		}

		err = k8sClient.Get(context.Background(), client.ObjectKeyFromObject(argocd), argocd)
		if err != nil {
			GinkgoWriter.Println(err)
			return false
		}

		return f(argocd)

	}, BeTrue())

}

func LogInToDefaultArgoCDInstance() error {
	k8sClient, _, err := utils.GetE2ETestKubeClientWithError()
	if err != nil {
		return err
	}

	var routeList routev1.RouteList
	Expect(k8sClient.List(context.Background(), &routeList, client.InNamespace("openshift-gitops"))).To(Succeed())

	var route *routev1.Route
	for idx := range routeList.Items {
		idxRoute := routeList.Items[idx]

		if idxRoute.Name == "openshift-gitops-server" {
			route = &idxRoute
		}
	}
	if route == nil {
		return fmt.Errorf("unable to locate route")
	}

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops-cluster", Namespace: "openshift-gitops"}}
	if err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(secret), secret); err != nil {
		return fmt.Errorf("unable to locate 'openshift-gitops-cluster' Secret")
	}

	// Note: '--skip-test-tls' parameter was added in Feb 2025, to work around OpenShift Routes not supporting HTTP2 by default, along with Argo CD upstream bugs https://github.com/argoproj/argo-cd/issues/21764, and https://github.com/argoproj/argo-cd/issues/20121
	output, err := RunArgoCDCLI("login", route.Spec.Host, "--username", "admin", "--password", string(secret.Data["admin.password"]), "--insecure", "--skip-test-tls")
	if err != nil {
		return err
	}

	if !strings.Contains(string(output), "'admin:login' logged in successfully") {
		return fmt.Errorf("unable to log in to Argo CD")
	}

	return nil

}

func RunArgoCDCLI(args ...string) (string, error) {

	cmdArgs := append([]string{"argocd"}, args...)

	if args[0] == "login" {
		GinkgoWriter.Println("executing command: argocd login (...)")
	} else {
		GinkgoWriter.Println("executing command", cmdArgs)
	}

	// #nosec G204
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)

	output, err := cmd.CombinedOutput()
	GinkgoWriter.Println(string(output))

	return string(output), err
}
