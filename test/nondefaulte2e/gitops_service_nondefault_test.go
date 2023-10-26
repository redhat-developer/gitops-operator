/*
Copyright 2021.

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

package nondefaulte2e

import (
	"context"
	"fmt"
	"time"

	argoapp "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	routev1 "github.com/openshift/api/route/v1"

	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/redhat-developer/gitops-operator/common"
	"github.com/redhat-developer/gitops-operator/test/helper"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	kubeerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("GitOpsServiceNoDefaultInstall", func() {
	Context("Validate no default install", func() {
		existingArgoInstance := &argoapp.ArgoCD{
			ObjectMeta: metav1.ObjectMeta{
				Name:      common.ArgoCDInstanceName,
				Namespace: "openshift-gitops",
			},
		}

		It("Backend and kam resources are created in 'openshift-gitops' namespace", func() {
			resourceList := []helper.ResourceList{
				{
					Resource: &appsv1.Deployment{},
					ExpectedResources: []string{
						"cluster",
						"kam",
					},
				},
				{
					Resource: &routev1.Route{},
					ExpectedResources: []string{
						"kam",
					},
				},
			}
			err := helper.WaitForResourcesByName(k8sClient, resourceList, existingArgoInstance.Namespace, time.Second*180)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Default Argo CD instance should not be found", func() {
			Eventually(func() error {
				err := k8sClient.Get(context.Background(),
					types.NamespacedName{Name: existingArgoInstance.Name, Namespace: existingArgoInstance.Namespace},
					existingArgoInstance)

				if err == nil {
					return fmt.Errorf("argoCD instance in 'openshift-gitops' should not be found")
				}
				return nil
			}, timeout, interval).ShouldNot(HaveOccurred())
		})
	})

	Context("Validate namespace scoped install", func() {
		name := "standalone-argocd-instance"
		existingArgoInstance := &argoapp.ArgoCD{}
		It("Create a non-default Argo CD instance in test namespace", func() {
			By("create a test namespace")
			newNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: helper.StandaloneArgoCDNamespace,
				},
			}
			err := k8sClient.Create(context.TODO(), newNamespace)
			if !kubeerrors.IsAlreadyExists(err) {
				Expect(err).NotTo(HaveOccurred())
			}

			By("create new ArgoCD instance in the test namespace")
			existingArgoInstance =
				&argoapp.ArgoCD{
					ObjectMeta: metav1.ObjectMeta{
						Name:      name,
						Namespace: newNamespace.Name,
					},
				}
			err = k8sClient.Create(context.TODO(), existingArgoInstance)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Verify that a subset of resources are created", func() {
			resourceList := []helper.ResourceList{
				{
					Resource: &appsv1.Deployment{},
					ExpectedResources: []string{
						name + "-redis",
						name + "-repo-server",
						name + "-server",
					},
				},
				{
					Resource: &corev1.ConfigMap{},
					ExpectedResources: []string{
						"argocd-cm",
						"argocd-gpg-keys-cm",
						"argocd-rbac-cm",
						"argocd-ssh-known-hosts-cm",
						"argocd-tls-certs-cm",
					},
				},
				{
					Resource: &corev1.ServiceAccount{},
					ExpectedResources: []string{
						name + "-argocd-application-controller",
						name + "-argocd-server",
					},
				},
				{
					Resource: &rbacv1.Role{},
					ExpectedResources: []string{
						name + "-argocd-application-controller",
						name + "-argocd-server",
					},
				},
				{
					Resource: &rbacv1.RoleBinding{},
					ExpectedResources: []string{
						name + "-argocd-application-controller",
						name + "-argocd-server",
					},
				},
				{
					Resource: &monitoringv1.ServiceMonitor{},
					ExpectedResources: []string{
						name,
						name + "-repo-server",
						name + "-server",
					},
				},
			}

			err := helper.WaitForResourcesByName(k8sClient, resourceList, existingArgoInstance.Namespace, time.Second*180)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Clean up test resources", func() {
			Expect(helper.DeleteNamespace(k8sClient, helper.StandaloneArgoCDNamespace))
		})
	})
})
