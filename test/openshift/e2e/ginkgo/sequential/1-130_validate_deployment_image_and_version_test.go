/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package sequential

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	argov1beta1api "github.com/redhat-developer/gitops-operator/argocd-operator/api/v1beta1"
	"github.com/redhat-developer/gitops-operator/argocd-operator/common"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
)

const (
	controllerManagerName      = "openshift-gitops-operator-controller-manager"
	controllerManagerNamespace = "openshift-gitops-operator"
	argocdInstanceName         = "argocd"
	argocdNamespace            = "argocd"
	defaultImage               = "quay.io/argoproj/argocd:latest"
	expectedDefaultImage       = "quay.io/argoproj/argocd:latest"
)

// --- Local Helpers (kept inline for reuse in this test) ---

func waitForDeployments(ctx context.Context, c client.Client, ns string, min int) *appsv1.DeploymentList {
	deploys := &appsv1.DeploymentList{}
	Eventually(func() error {
		if err := c.List(ctx, deploys, client.InNamespace(ns)); err != nil {
			return err
		}
		if len(deploys.Items) < min {
			return fmt.Errorf("expected at least %d deployments, got %d", min, len(deploys.Items))
		}
		return nil
	}, "2m", "5s").Should(Succeed())
	return deploys
}

func assertDeploymentImages(deploys *appsv1.DeploymentList, expected map[string]string) {
	for _, d := range deploys.Items {
		if exp, ok := expected[d.Name]; ok {
			Expect(d.Spec.Template.Spec.Containers).To(HaveLen(1))
			got := d.Spec.Template.Spec.Containers[0].Image
			Expect(got).To(Equal(exp), fmt.Sprintf("deployment %s has %s, expected %s", d.Name, got, exp))
		}
	}
}

func assertStatefulSetImage(ctx context.Context, c client.Client, ns, name, expected string) {
	sts := &appsv1.StatefulSet{}
	Expect(c.Get(ctx, types.NamespacedName{Name: name, Namespace: ns}, sts)).To(Succeed())
	Expect(sts.Spec.Template.Spec.Containers).To(HaveLen(1))
	got := sts.Spec.Template.Spec.Containers[0].Image
	Expect(got).To(Equal(expected), fmt.Sprintf("statefulset %s has %s, expected %s", name, got, expected))
}

// --- Test ---

var _ = Describe("Validate deployment image and version", func() {
	Context("1-130_validate_deployment_image_and_version", func() {
		var (
			c   client.Client
			ctx context.Context
		)

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
			c, _ = utils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		BeforeEach(func() {
			if fixture.EnvLocalRun() {
				Skip("Requires setting operator environment variable")
			}
		})

		It("validates image resolution order", func() {

			// The image used must be valid one for things to start, but it must be different from the default so the test is valid
			Expect(defaultImage).To(Not(Equal(fmt.Sprintf("%s:%s", common.ArgoCDDefaultArgoImage, common.ArgoCDDefaultArgoVersion))))

			By("ensuring operator env variable ARGOCD_IMAGE is set")
			fixture.SetEnvInOperatorSubscriptionOrDeployment("ARGOCD_IMAGE", defaultImage)

			By("creating namespace-scoped ArgoCD without image/version")
			ns, cleanupFunc := fixture.CreateNamespaceWithCleanupFunc(argocdNamespace)

			argo := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{Name: argocdInstanceName, Namespace: argocdNamespace},
				Spec:       argov1beta1api.ArgoCDSpec{ApplicationSet: &argov1beta1api.ArgoCDApplicationSet{Enabled: new(true)}},
			}
			Expect(c.Create(ctx, argo)).To(Succeed())
			Eventually(argo, "2m", "5s").Should(argocdFixture.BeAvailable())

			defer func() {
				cleanupFunc()
				fixture.RestoreSubscriptionToDefault()
			}()

			checkImages := func(expected map[string]string, stsExpected string) {
				deploys := waitForDeployments(ctx, c, ns.Name, 4)
				assertDeploymentImages(deploys, expected)
				assertStatefulSetImage(ctx, c, ns.Name, "argocd-application-controller", stsExpected)
			}

			By("verifying deployments/statefulset use env image")
			checkImages(map[string]string{
				"argocd-repo-server":               expectedDefaultImage,
				"argocd-server":                    expectedDefaultImage,
				"argocd-applicationset-controller": expectedDefaultImage,
			}, expectedDefaultImage)

			By("updating spec.version")
			argocdFixture.Update(argo, func(argo *argov1beta1api.ArgoCD) {
				argo.Spec.Version = "v0.0.1"
			})
			time.Sleep(30 * time.Second)

			checkImages(map[string]string{
				"argocd-repo-server":               "quay.io/argoproj/argocd:v0.0.1",
				"argocd-server":                    "quay.io/argoproj/argocd:v0.0.1",
				"argocd-applicationset-controller": "quay.io/argoproj/argocd:v0.0.1",
			}, "quay.io/argoproj/argocd:v0.0.1")

			By("updating spec.version and spec.image")
			argocdFixture.Update(argo, func(argo *argov1beta1api.ArgoCD) {
				argo.Spec.Image = "argoprpj/sampletestimage"
				argo.Spec.Version = "v0.0.1"
			})
			time.Sleep(30 * time.Second)

			checkImages(map[string]string{
				"argocd-repo-server":               "argoprpj/sampletestimage:v0.0.1",
				"argocd-server":                    "argoprpj/sampletestimage:v0.0.1",
				"argocd-applicationset-controller": "argoprpj/sampletestimage:v0.0.1",
			}, "argoprpj/sampletestimage:v0.0.1")
		})
	})
})
