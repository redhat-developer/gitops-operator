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

package e2e

import (
	"context"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	b64 "encoding/base64"
	"encoding/json"

	argoapp "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	"github.com/argoproj-labs/argocd-operator/common"
	"github.com/argoproj-labs/argocd-operator/controllers/argoutil"
	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	osappsv1 "github.com/openshift/api/apps/v1"
	configv1 "github.com/openshift/api/config/v1"
	console "github.com/openshift/api/console/v1"
	routev1 "github.com/openshift/api/route/v1"
	templatev1 "github.com/openshift/api/template/v1"
	pipelinesv1alpha1 "github.com/redhat-developer/gitops-operator/api/v1alpha1"
	gitopscommon "github.com/redhat-developer/gitops-operator/common"
	"github.com/redhat-developer/gitops-operator/controllers/argocd"
	"github.com/redhat-developer/gitops-operator/test/helper"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	kubeerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOpsServiceController", func() {
	Context("Validate default Argo CD installation", func() {
		argoCDInstance := &argoapp.ArgoCD{}
		It("openshift-gitops namespace is created", func() {
			checkIfPresent(types.NamespacedName{Name: argoCDNamespace}, &corev1.Namespace{})
		})

		It("Argo CD instance is created", func() {
			checkIfPresent(types.NamespacedName{Name: argoCDInstanceName, Namespace: argoCDNamespace}, argoCDInstance)
			checkIfPresent(types.NamespacedName{Name: defaultApplicationControllerName, Namespace: argoCDNamespace}, &appsv1.StatefulSet{})
			checkIfPresent(types.NamespacedName{Name: defaultApplicationSetControllerName, Namespace: argoCDNamespace}, &appsv1.Deployment{})
			checkIfPresent(types.NamespacedName{Name: defaultDexInstanceName, Namespace: argoCDNamespace}, &appsv1.Deployment{})
			checkIfPresent(types.NamespacedName{Name: defaultRedisName, Namespace: argoCDNamespace}, &appsv1.Deployment{})
			checkIfPresent(types.NamespacedName{Name: defaultRepoServerName, Namespace: argoCDNamespace}, &appsv1.Deployment{})
			checkIfPresent(types.NamespacedName{Name: defaultServerName, Namespace: argoCDNamespace}, &appsv1.Deployment{})
		})

		It("Manual modification of Argo CD CR is allowed", func() {
			By("modify the Argo CD CR")
			// update .sso.provider = keycloak to enable RHSSO for default Argo CD instance.
			// update verifyTLS = false to ensure operator(when run locally) can create RHSSO resources.
			argoCDInstance.Spec.DisableAdmin = true
			insecure := false
			// remove dex configuration, only one SSO is supported.
			argoCDInstance.Spec.SSO = &argoapp.ArgoCDSSOSpec{
				Provider: "keycloak",
				Keycloak: &argoapp.ArgoCDKeycloakSpec{
					VerifyTLS: &insecure,
				},
			}

			err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				updatedInstance := &argoapp.ArgoCD{}
				err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: argoCDInstanceName, Namespace: argoCDNamespace}, updatedInstance)
				if err != nil {
					return err
				}
				updatedInstance.Spec.DisableAdmin = argoCDInstance.Spec.DisableAdmin
				updatedInstance.Spec.SSO = argoCDInstance.Spec.SSO
				return k8sClient.Update(context.TODO(), updatedInstance)
			})
			Expect(err).NotTo(HaveOccurred())

			By("check if the modification was not overwritten")
			argoCDInstance = &argoapp.ArgoCD{}
			checkIfPresent(types.NamespacedName{Name: argoCDInstanceName, Namespace: argoCDNamespace}, argoCDInstance)
			Expect(argoCDInstance.Spec.DisableAdmin).Should(BeTrue())
		})
	})

	Context("Check if gitops backend resources are created", func() {
		name := "cluster"
		It("Backend deployment is created", func() {
			checkIfPresent(types.NamespacedName{Name: name, Namespace: argoCDNamespace}, &appsv1.Deployment{})
		})

		It("Backend service is created", func() {
			checkIfPresent(types.NamespacedName{Name: name, Namespace: argoCDNamespace}, &corev1.Service{})
		})

		It("RBAC for backend service is created", func() {
			prefixedName := fmt.Sprintf("%s-%s", "gitops-service", name)
			checkIfPresent(types.NamespacedName{Name: prefixedName}, &rbacv1.ClusterRole{})
			checkIfPresent(types.NamespacedName{Name: prefixedName}, &rbacv1.ClusterRoleBinding{})
			checkIfPresent(types.NamespacedName{Name: prefixedName, Namespace: argoCDNamespace}, &corev1.ServiceAccount{})
		})
	})

	Context("Check if kam resources are created", func() {
		name := "kam"
		It("Deployment that hosts kam is created", func() {
			checkIfPresent(types.NamespacedName{Name: name, Namespace: argoCDNamespace}, &appsv1.Deployment{})
		})

		It("Service that serves kam is created", func() {
			checkIfPresent(types.NamespacedName{Name: name, Namespace: argoCDNamespace}, &corev1.Service{})
		})

		It("Console CLI download resource that adds kam route to OpenShift's CLI download page is created", func() {

			By("route that serves kam is created")
			route := &routev1.Route{}
			checkIfPresent(types.NamespacedName{Name: name, Namespace: argoCDNamespace}, route)

			By("CLI download link is created")
			consoleCLIDownload := &console.ConsoleCLIDownload{}
			checkIfPresent(types.NamespacedName{Name: name}, consoleCLIDownload)

			By("CLI download link should match the kam route")
			consoleCLILink := strings.TrimLeft(consoleCLIDownload.Spec.Links[0].Href, "https://")
			Expect(route.Spec.Host + "/kam/").Should(Equal(consoleCLILink))
		})
	})

	Context("Validate machine config updates", func() {
		BeforeEach(func() {
			imageYAML := filepath.Join("..", "appcrs", "image_appcr.yaml")
			ocPath, err := exec.LookPath("oc")
			Expect(err).NotTo(HaveOccurred())

			// 'When GitOps operator is run locally (not installed via OLM), it does not correctly setup
			// the 'argoproj.io' Role rules for the 'argocd-application-controller'
			// Thus, applying missing rules for 'argocd-application-controller'
			// TODO: Remove once https://github.com/redhat-developer/gitops-operator/issues/148 is fixed
			err = applyMissingPermissions("openshift-gitops", "openshift-gitops")
			Expect(err).NotTo(HaveOccurred())

			cmd := exec.Command(ocPath, "apply", "-f", imageYAML)
			err = cmd.Run()
			Expect(err).NotTo(HaveOccurred())
		})

		It("Image is created", func() {
			By("check health and sync status")
			Eventually(func() error {
				err := helper.ApplicationHealthStatus("image", "openshift-gitops")
				if err != nil {
					return err
				}
				err = helper.ApplicationSyncStatus("image", "openshift-gitops")
				if err != nil {
					return err
				}
				return nil
			}, time.Minute*10, interval).ShouldNot(HaveOccurred())

			existingImage := &configv1.Image{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
			}
			checkIfPresent(types.NamespacedName{Name: existingImage.Name}, existingImage)
		})
	})

	Context("Validate non-default Argo CD instance", func() {
		argocdNonDefaultInstanceName := "argocd-instance"
		argocdNonDefaultNamespace := "argocd-ns"

		It("Create a non-default Argo CD instance", func() {
			By("create a test ns")
			argocdNonDefaultNamespaceObj := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: argocdNonDefaultNamespace,
				},
			}
			err := k8sClient.Create(context.TODO(), argocdNonDefaultNamespaceObj)
			if !kubeerrors.IsAlreadyExists(err) {
				Expect(err).NotTo(HaveOccurred())
			}

			By("create a new Argo CD instance in test ns")
			argocdNonDefaultNamespaceInstance, err := argocd.NewCR(argocdNonDefaultInstanceName, argocdNonDefaultNamespace)
			err = k8sClient.Create(context.TODO(), argocdNonDefaultNamespaceInstance)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() error {
				_, err := helper.ProjectExists("default", argocdNonDefaultNamespace)
				if err != nil {
					return err
				}
				return nil
			}, timeout, interval).ShouldNot(HaveOccurred())
		})

		It("Create a sample application", func() {
			nonDefaultAppCR := filepath.Join("..", "appcrs", "non_default_appcr.yaml")
			ocPath, err := exec.LookPath("oc")
			Expect(err).NotTo(HaveOccurred())
			cmd := exec.Command(ocPath, "apply", "-f", nonDefaultAppCR)
			_, err = cmd.CombinedOutput()
			if err != nil {
				Expect(err).NotTo(HaveOccurred())
			}

			By("check if the app is healthy and in sync")
			Eventually(func() error {
				err := helper.ApplicationHealthStatus("nginx", argocdNonDefaultNamespace)
				if err != nil {
					return err
				}

				err = helper.ApplicationSyncStatus("nginx", argocdNonDefaultNamespace)
				return err
			}, time.Minute*10, interval).ShouldNot(HaveOccurred())
		})

		It("Clean up test resources", func() {
			Expect(helper.DeleteNamespace(k8sClient, argocdNonDefaultNamespace))
		})
	})

	Context("Validate namespace scoped install", func() {
		name := "standalone-argocd-instance"
		existingArgoInstance := &argoapp.ArgoCD{}
		It("Create a non-default Argo CD instance", func() {
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

	Context("Validate Cluster Config Support", func() {
		It("Apply missing permissions", func() {
			// 'When GitOps operator is run locally (not installed via OLM), it does not correctly setup
			// the 'argoproj.io' Role rules for the 'argocd-application-controller'
			// Thus, applying missing rules for 'argocd-application-controller'
			// TODO: Remove once https://github.com/redhat-developer/gitops-operator/issues/148 is fixed
			err := applyMissingPermissions("openshift-gitops", "openshift-gitops")
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() error {
				_, err := helper.ProjectExists("default", "openshift-gitops")
				if err != nil {
					return err
				}
				return nil
			}, timeout, interval).ShouldNot(HaveOccurred())
		})

		It("Update cluster config resource", func() {
			ocPath, err := exec.LookPath("oc")
			Expect(err).NotTo(HaveOccurred())
			schedulerYAML := filepath.Join("..", "appcrs", "scheduler_appcr.yaml")
			cmd := exec.Command(ocPath, "apply", "-f", schedulerYAML)
			_, err = cmd.CombinedOutput()
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() error {
				err := helper.ApplicationHealthStatus("policy-configmap", "openshift-gitops")
				if err != nil {
					return err
				}
				err = helper.ApplicationSyncStatus("policy-configmap", "openshift-gitops")
				if err != nil {
					return err
				}
				return nil
			}, time.Minute*5, interval).ShouldNot(HaveOccurred())

			namespacedName := types.NamespacedName{Name: "policy-configmap", Namespace: "openshift-config"}
			existingConfigMap := &corev1.ConfigMap{}

			checkIfPresent(namespacedName, existingConfigMap)
		})
	})

	Context("Validate granting permissions by label", func() {
		sourceNS := "source-ns"
		argocdInstance := "argocd-label"
		targetNS := "target-ns"

		It("Create source and target namespaces", func() {
			// create a new source namespace
			sourceNamespaceObj := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: sourceNS,
				},
			}
			err := k8sClient.Create(context.TODO(), sourceNamespaceObj)
			if !kubeerrors.IsAlreadyExists(err) {
				Expect(err).NotTo(HaveOccurred())
			}

			// create an ArgoCD instance in the source namespace
			argoCDInstanceObj, err := argocd.NewCR(argocdInstance, sourceNS)
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), argoCDInstanceObj)
			if !kubeerrors.IsAlreadyExists(err) {
				Expect(err).NotTo(HaveOccurred())
			}

			// Wait for the default project to exist; this avoids a race condition where the Application
			// can be created before the Project that it targets.
			Eventually(func() error {
				_, err := helper.ProjectExists("default", sourceNS)
				if err != nil {
					return err
				}
				return nil
			}, time.Minute*10, interval).ShouldNot(HaveOccurred())

			// 'When GitOps operator is run locally (not installed via OLM), it does not correctly setup
			// the 'argoproj.io' Role rules for the 'argocd-application-controller'
			// Thus, applying missing rules for 'argocd-application-controller'
			// TODO: Remove once https://github.com/redhat-developer/gitops-operator/issues/148 is fixed
			if err := applyMissingPermissions(argocdInstance, sourceNS); err != nil {
				Expect(err).NotTo(HaveOccurred())
			}

			// create a target namespace to deploy resources
			// allow argocd to create resources in the target namespace by adding managed-by label
			targetNamespaceObj := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: targetNS,
					Labels: map[string]string{
						"argocd.argoproj.io/managed-by": sourceNS,
					},
				},
			}
			err = k8sClient.Create(context.TODO(), targetNamespaceObj)
			if !kubeerrors.IsAlreadyExists(err) {
				Expect(err).NotTo(HaveOccurred())
			}
		})

		It("Required RBAC resources are created in the target namespace", func() {
			resourceList := []helper.ResourceList{
				{
					Resource: &rbacv1.Role{},
					ExpectedResources: []string{
						argocdInstance + "-argocd-application-controller",
						argocdInstance + "-argocd-server",
					},
				},
				{
					Resource: &rbacv1.RoleBinding{},
					ExpectedResources: []string{
						argocdInstance + "-argocd-application-controller",
						argocdInstance + "-argocd-server",
					},
				},
			}
			err := helper.WaitForResourcesByName(k8sClient, resourceList, targetNS, time.Second*180)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Check if an application could be deployed in target namespace", func() {
			nginxAppCr := filepath.Join("..", "appcrs", "nginx_appcr.yaml")
			ocPath, err := exec.LookPath("oc")
			Expect(err).NotTo(HaveOccurred())
			cmd := exec.Command(ocPath, "apply", "-f", nginxAppCr)
			err = cmd.Run()
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() error {
				err := helper.ApplicationHealthStatus("nginx", sourceNS)
				if err != nil {
					return err
				}
				err = helper.ApplicationSyncStatus("nginx", sourceNS)
				if err != nil {
					return err
				}
				return nil
			}, time.Second*600, interval).ShouldNot(HaveOccurred())
		})

		It("Clean up resources", func() {
			Expect(helper.DeleteNamespace(k8sClient, sourceNS)).NotTo(HaveOccurred())
			Expect(helper.DeleteNamespace(k8sClient, targetNS)).NotTo(HaveOccurred())
		})
	})

	Context("Validate permission label feature for OOTB Argo CD instance", func() {
		argocdTargetNamespace := "argocd-target"
		It("Create target namespace", func() {
			// 'When GitOps operator is run locally (not installed via OLM), it does not correctly setup
			// the 'argoproj.io' Role rules for the 'argocd-application-controller'
			// Thus, applying missing rules for 'argocd-application-controller'
			// TODO: Remove once https://github.com/redhat-developer/gitops-operator/issues/148 is fixed
			err := applyMissingPermissions(argoCDInstanceName, argoCDNamespace)
			Expect(err).NotTo(HaveOccurred())

			// create a target namespace to deploy resources
			// allow argocd to create resources in the target namespace by adding managed-by label
			targetNamespaceObj := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: argocdTargetNamespace,
					Labels: map[string]string{
						argocdManagedByLabel: argoCDNamespace,
					},
				},
			}
			err = k8sClient.Create(context.TODO(), targetNamespaceObj)
			if !kubeerrors.IsAlreadyExists(err) {
				Expect(err).NotTo(HaveOccurred())
			}
		})

		It("Required RBAC resources should be created in target namespace", func() {
			resourceList := []helper.ResourceList{
				{
					Resource: &rbacv1.Role{},
					ExpectedResources: []string{
						argoCDInstanceName + "-argocd-application-controller",
						argoCDInstanceName + "-argocd-server",
					},
				},
				{
					Resource: &rbacv1.RoleBinding{},
					ExpectedResources: []string{
						argoCDInstanceName + "-argocd-application-controller",
						argoCDInstanceName + "-argocd-server",
					},
				},
			}
			err := helper.WaitForResourcesByName(k8sClient, resourceList, argocdTargetNamespace, time.Second*180)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Deploy an app in the target namespace", func() {
			nginxAppCr := filepath.Join("..", "appcrs", "nginx_default_ns_appcr.yaml")
			ocPath, err := exec.LookPath("oc")
			Expect(err).NotTo(HaveOccurred())
			cmd := exec.Command(ocPath, "apply", "-f", nginxAppCr)
			err = cmd.Run()
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() error {
				if err := helper.ApplicationHealthStatus("nginx", argoCDNamespace); err != nil {
					return err
				}
				if err := helper.ApplicationSyncStatus("nginx", argoCDNamespace); err != nil {
					return err
				}
				return nil
			}, time.Minute*5, interval).ShouldNot(HaveOccurred())
		})

		It("Clean up resources", func() {
			Expect(helper.DeleteNamespace(k8sClient, argocdTargetNamespace)).NotTo(HaveOccurred())
		})
	})

	Context("Validate revoking permissions by label", func() {
		argocdNonDefaultNamespace := "argocd-non-default-source"
		argocdTargetNamespace := "argocd-target"
		argocdNonDefaultNamespaceInstanceName := "argocd-non-default-namespace-instance"
		resourceList := []helper.ResourceList{
			{
				Resource: &rbacv1.Role{},
				ExpectedResources: []string{
					argocdNonDefaultNamespaceInstanceName + "-argocd-application-controller",
					argocdNonDefaultNamespaceInstanceName + "-argocd-server",
				},
			},
			{
				Resource: &rbacv1.RoleBinding{},
				ExpectedResources: []string{
					argocdNonDefaultNamespaceInstanceName + "-argocd-application-controller",
					argocdNonDefaultNamespaceInstanceName + "-argocd-server",
				},
			},
		}

		It("Create source and target namespaces", func() {
			By("create source namespace")
			sourceNamespaceObj := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: argocdNonDefaultNamespace,
				},
			}
			err := k8sClient.Create(context.TODO(), sourceNamespaceObj)
			if !kubeerrors.IsAlreadyExists(err) {
				Expect(err).NotTo(HaveOccurred())
			}

			By("create an Argo CD instance in source namespace")
			argoCDInstanceObj, err := argocd.NewCR(argocdNonDefaultNamespaceInstanceName, argocdNonDefaultNamespace)
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), argoCDInstanceObj)
			Expect(err).NotTo(HaveOccurred())

			By("create target namespace with label")
			targetNamespaceObj := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: argocdTargetNamespace,
					Labels: map[string]string{
						argocdManagedByLabel: argocdNonDefaultNamespace,
					},
				},
			}
			err = k8sClient.Create(context.TODO(), targetNamespaceObj)
			if !kubeerrors.IsAlreadyExists(err) {
				Expect(err).NotTo(HaveOccurred())
			}
		})

		It("Required RBAC resources should be created in target namespace", func() {
			err := helper.WaitForResourcesByName(k8sClient, resourceList, argocdTargetNamespace, time.Second*180)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Remove label from target namespace", func() {
			targetNsObj := &corev1.Namespace{}
			err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: argocdTargetNamespace}, targetNsObj)
			Expect(err).NotTo(HaveOccurred())

			delete(targetNsObj.Labels, argocdManagedByLabel)
			err = k8sClient.Update(context.TODO(), targetNsObj)
			Expect(err).NotTo(HaveOccurred())
		})

		It("RBAC resources must be absent", func() {
			Eventually(func() error {
				for _, resourceListEntry := range resourceList {
					for _, resourceName := range resourceListEntry.ExpectedResources {
						resource := resourceListEntry.Resource
						namespacedName := types.NamespacedName{Name: resourceName, Namespace: argocdTargetNamespace}
						if err := k8sClient.Get(context.TODO(), namespacedName, resource); err == nil {
							GinkgoT().Logf("Resource %s was not deleted", resourceName)
							return nil
						}
						GinkgoT().Logf("Resource %s was successfully deleted", resourceName)
					}
				}
				return nil
			}, time.Minute*2, interval).ShouldNot(HaveOccurred())
		})

		It("Target namespace must be removed from cluster secret", func() {
			Eventually(func() error {
				argocdSecretTypeLabel := "argocd.argoproj.io/secret-type"
				argoCDDefaultServer := "https://kubernetes.default.svc"
				listOptions := &client.ListOptions{}
				client.MatchingLabels{argocdSecretTypeLabel: "cluster"}.ApplyToList(listOptions)
				clusterSecretList := &corev1.SecretList{}
				err := k8sClient.List(context.TODO(), clusterSecretList, listOptions)

				if err != nil {
					GinkgoT().Logf("Unable to retrieve cluster secrets: %v", err)
					return err
				}
				for _, secret := range clusterSecretList.Items {
					if string(secret.Data["server"]) != argoCDDefaultServer {
						continue
					}
					if namespaces, ok := secret.Data["namespaces"]; ok {
						namespaceList := strings.Split(string(namespaces), ",")
						for _, ns := range namespaceList {
							if strings.TrimSpace(ns) == argocdTargetNamespace {
								err := fmt.Errorf("namespace %v still present in cluster secret namespace list", argocdTargetNamespace)
								GinkgoT().Log(err.Error())
								return err
							}
						}
						GinkgoT().Logf("namespace %v succesfully removed from cluster secret namespace list", argocdTargetNamespace)
					}
				}
				return nil
			}, timeout, interval).ShouldNot(HaveOccurred())
		})

		It("Clean up resources", func() {
			Expect(helper.DeleteNamespace(k8sClient, argocdNonDefaultNamespace)).NotTo(HaveOccurred())
			Expect(helper.DeleteNamespace(k8sClient, argocdTargetNamespace)).NotTo(HaveOccurred())
		})
	})

	Context("Verify RHSSO installation", func() {
		namespace := argoCDNamespace
		It("Template instance is created", func() {
			tInstance := &templatev1.TemplateInstance{}
			checkIfPresent(types.NamespacedName{Name: defaultTemplateIdentifier, Namespace: namespace}, tInstance)
		})

		It("Keycloak deployment is created", func() {
			Eventually(func() error {
				dc := &osappsv1.DeploymentConfig{}
				err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: defaultKeycloakIdentifier, Namespace: namespace}, dc)
				if err != nil {
					return err
				}
				if dc != nil {
					got := dc.Status.AvailableReplicas
					want := int32(1)
					if got != want {
						return fmt.Errorf("expected %d, got %d", want, got)
					}
				}
				return nil
			}, timeout, interval).ShouldNot(HaveOccurred())
		})

		It("Keycloak service is created", func() {
			svc := &corev1.Service{}
			checkIfPresent(types.NamespacedName{Name: defaultKeycloakIdentifier, Namespace: namespace}, svc)
		})

		It("Keycloak service route is created", func() {
			route := &routev1.Route{}
			checkIfPresent(types.NamespacedName{Name: defaultKeycloakIdentifier, Namespace: namespace}, route)
		})
	})

	Context("Verify RHSSO configuration", func() {
		namespace := argoCDNamespace

		It("Verify RHSSO Realm creation", func() {
			By("get keycloak URL and credentials")
			route := &routev1.Route{}
			checkIfPresent(types.NamespacedName{Name: defaultKeycloakIdentifier, Namespace: namespace}, route)

			secret := &corev1.Secret{}
			checkIfPresent(types.NamespacedName{Name: rhssosecret, Namespace: namespace}, secret)

			userEnc := b64.URLEncoding.EncodeToString(secret.Data["SSO_USERNAME"])
			user, _ := b64.URLEncoding.DecodeString(userEnc)

			passEnc := b64.URLEncoding.EncodeToString(secret.Data["SSO_PASSWORD"])
			pass, _ := b64.URLEncoding.DecodeString(passEnc)

			By("get auth token from kaycloak")
			accessURL := fmt.Sprintf("https://%s%s", route.Spec.Host, authURL)
			argoRealmURL := fmt.Sprintf("https://%s%s", route.Spec.Host, realmURL)

			accessToken, err := getAccessToken(string(user), string(pass), accessURL)
			Expect(err).NotTo(HaveOccurred())

			By("create a new https request to verify Realm creation")
			client := http.Client{}
			http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
			request, err := http.NewRequest("GET", argoRealmURL, nil)
			Expect(err).NotTo(HaveOccurred())
			request.Header.Set("Content-Type", "application/json")
			request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", accessToken))

			By("verify RHSSO realm creation and check if HTTP GET returns 200 ")
			response, err := client.Do(request)
			Expect(err).NotTo(HaveOccurred())
			defer response.Body.Close()

			By("verify reponse")
			b, err := ioutil.ReadAll(response.Body)
			Expect(err).NotTo(HaveOccurred())

			m := make(map[string]interface{})
			err = json.Unmarshal(b, &m)
			Expect(err).NotTo(HaveOccurred())

			Expect(m["realm"]).To(Equal("argocd"))
			Expect(m["registrationFlow"]).To(Equal("registration"))
			Expect(m["browserFlow"]).To(Equal("browser"))
			Expect(m["clientAuthenticationFlow"]).To(Equal("clients"))
			Expect(m["directGrantFlow"]).To(Equal("direct grant"))
			Expect(m["loginWithEmailAllowed"]).To(BeTrue())

			idps := m["identityProviders"].([]interface{})
			idp := idps[0].(map[string]interface{})

			Expect(idp["alias"]).To(Equal("openshift-v4"))
			Expect(idp["displayName"] == "Login with OpenShift")
			Expect(idp["providerId"]).To(Equal("openshift-v4"))
			Expect(idp["firstBrokerLoginFlowAlias"]).To(Equal("first broker login"))
		})

		It("Verify OIDC Configuration is created", func() {
			Eventually(func() error {
				cm := &corev1.ConfigMap{}
				err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: argoCDConfigMapName, Namespace: namespace}, cm)
				if err != nil {
					return err
				}
				if cm.Data[common.ArgoCDKeyOIDCConfig] == "" {
					return fmt.Errorf("expected OIDC configuration to be created")
				}
				return nil
			}, timeout, interval).ShouldNot(HaveOccurred())
		})

	})

	Context("Verify RHSSO uninstallation", func() {
		namespace := argoCDNamespace
		argocd := &argoapp.ArgoCD{}
		It("Remove SSO field from Argo CD CR", func() {
			err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: argoCDInstanceName, Namespace: namespace}, argocd)

			argocd.Spec.SSO = nil
			err = k8sClient.Update(context.TODO(), argocd)
			Expect(err).NotTo(HaveOccurred())
		})

		It("OIDC configuration is removed", func() {
			Eventually(func() bool {
				cm := &corev1.ConfigMap{}
				err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: argoCDConfigMapName, Namespace: namespace}, cm)
				Expect(err).NotTo(HaveOccurred())
				return cm.Data[common.ArgoCDKeyOIDCConfig] == ""
			}, timeout, interval).Should(BeTrue())
		})

		It("Template instance is deleted", func() {
			Eventually(func() error {
				templateInstance := &templatev1.TemplateInstance{}
				err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: defaultTemplateIdentifier, Namespace: namespace}, templateInstance)
				if kubeerrors.IsNotFound(err) {
					return nil
				}
				return err
			}, timeout, interval).ShouldNot(HaveOccurred())
		})

		It("Add SSO field back and verify reconcilation", func() {
			insecure := false
			argocd.Spec.SSO = &argoapp.ArgoCDSSOSpec{
				Provider: defaultKeycloakIdentifier,
				Keycloak: &argoapp.ArgoCDKeycloakSpec{
					VerifyTLS: &insecure,
				},
			}
			err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				updatedInstance := &argoapp.ArgoCD{}
				err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: argoCDInstanceName, Namespace: argoCDNamespace}, updatedInstance)
				if err != nil {
					return err
				}
				updatedInstance.Spec.SSO = argocd.Spec.SSO
				return k8sClient.Update(context.TODO(), updatedInstance)
			})
			Expect(err).NotTo(HaveOccurred())

			templateInstance := &templatev1.TemplateInstance{}
			checkIfPresent(types.NamespacedName{Name: defaultTemplateIdentifier, Namespace: namespace}, templateInstance)
		})
	})

	Context("Verify Configuring Infrastructure NodeSelector ", func() {
		name := "cluster"
		gitopsService := &pipelinesv1alpha1.GitopsService{}

		It("Add runOnInfra spec to gitopsService CR", func() {
			err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: argoCDNamespace}, gitopsService)
			gitopsService.Spec.RunOnInfra = true
			nodeSelector := argoutil.AppendStringMap(gitopscommon.InfraNodeSelector(), common.DefaultNodeSelector())
			err = k8sClient.Update(context.TODO(), gitopsService)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() error {
				deployment := &appsv1.Deployment{}
				err = k8sClient.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: argoCDNamespace}, deployment)
				Expect(err).NotTo(HaveOccurred())
				Expect(deployment.Spec.Template.Spec.NodeSelector).To(Equal(nodeSelector))

				argocd := &argoapp.ArgoCD{}
				err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: argoCDInstanceName, Namespace: argoCDNamespace}, argocd)
				Expect(err).NotTo(HaveOccurred())
				Expect(argocd.Spec.NodePlacement.NodeSelector).To(Equal(gitopscommon.InfraNodeSelector()))
				return nil
			}, time.Second*180, interval).ShouldNot(HaveOccurred())

		})
		It("Remove runOnInfra spec from gitopsService CR", func() {
			err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: argoCDNamespace}, gitopsService)
			gitopsService.Spec.RunOnInfra = false
			err = k8sClient.Update(context.TODO(), gitopsService)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() error {
				deployment := &appsv1.Deployment{}
				err = k8sClient.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: argoCDNamespace}, deployment)
				Expect(err).NotTo(HaveOccurred())
				if len(deployment.Spec.Template.Spec.NodeSelector) != 1 {
					return fmt.Errorf("expected one nodeSelector in deployment")
				}

				argocd := &argoapp.ArgoCD{}
				err = k8sClient.Get(context.TODO(), types.NamespacedName{Name: argoCDInstanceName, Namespace: argoCDNamespace}, argocd)
				Expect(err).NotTo(HaveOccurred())
				if argocd.Spec.NodePlacement != nil {
					return fmt.Errorf("expected no NodePlacement in argocd ")
				}

				return nil
			}, time.Second*180, interval).ShouldNot(HaveOccurred())

		})
	})

})

type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	ExpiresIn        int    `json:"expires_in"`
	RefreshExpiresIn int    `json:"refresh_expires_in"`
	RefreshToken     string `json:"refresh_token"`
	TokenType        string `json:"token_type"`
	NotBeforePolicy  int    `json:"not-before-policy"`
	SessionState     string `json:"session_state"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func getAccessToken(user, pass, accessURL string) (string, error) {
	form := url.Values{}
	form.Add("username", user)
	form.Add("password", pass)
	form.Add("client_id", "admin-cli")
	form.Add("grant_type", "password")

	client := http.Client{}
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	req, err := http.NewRequest(
		"POST",
		accessURL,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return "", err
	}

	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}

	defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return "", err
	}

	tokenRes := &tokenResponse{}
	err = json.Unmarshal(body, tokenRes)
	if err != nil {
		return "", err
	}

	if tokenRes.Error != "" {
		return "", err
	}

	return tokenRes.AccessToken, nil
}

func applyMissingPermissions(name, namespace string) error {
	// Check the role was created. If not, create a new role
	roleName := fmt.Sprintf("%s-openshift-gitops-argocd-application-controller", namespace)
	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleName,
			Namespace: namespace,
		},
		Rules: []rbacv1.PolicyRule{
			{
				Verbs:     []string{"*"},
				APIGroups: []string{"*"},
				Resources: []string{"*"},
			},
		},
	}
	err := k8sClient.Get(context.TODO(),
		types.NamespacedName{Name: roleName, Namespace: namespace}, role)
	if err != nil {
		if kubeerrors.IsNotFound(err) {
			err := k8sClient.Create(context.TODO(), role)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}

	// Check the role binding was created. If not, create a new role binding
	roleBindingName := fmt.Sprintf("%s-openshift-gitops-argocd-application-controller", namespace)
	roleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleBindingName,
			Namespace: namespace,
		},
		RoleRef: rbacv1.RoleRef{
			Name:     roleName,
			Kind:     "Role",
			APIGroup: "rbac.authorization.k8s.io",
		},
		Subjects: []rbacv1.Subject{
			{
				Name: fmt.Sprintf("%s-argocd-application-controller", name),
				Kind: "ServiceAccount",
			},
		},
	}
	err = k8sClient.Get(context.TODO(),
		types.NamespacedName{Name: roleBindingName, Namespace: namespace},
		roleBinding)
	if err != nil {
		if kubeerrors.IsNotFound(err) {
			err := k8sClient.Create(context.TODO(), roleBinding)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}

	return nil
}
