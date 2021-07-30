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
	"log"
	"net/http"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	b64 "encoding/base64"
	"encoding/json"

	argoapp "github.com/argoproj-labs/argocd-operator/pkg/apis/argoproj/v1alpha1"
	"github.com/argoproj-labs/argocd-operator/pkg/common"
	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	osappsv1 "github.com/openshift/api/apps/v1"
	configv1 "github.com/openshift/api/config/v1"
	console "github.com/openshift/api/console/v1"
	routev1 "github.com/openshift/api/route/v1"
	templatev1 "github.com/openshift/api/template/v1"
	"github.com/redhat-developer/gitops-operator/controllers/argocd"
	"github.com/redhat-developer/gitops-operator/test/helper"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	kubeerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

var _ = Describe("GitOpsServiceController", func() {
	Context("Validate default Argo CD installation", func() {
		argoCDInstance := &argoapp.ArgoCD{}
		It("openshift-gitops namespace is created", func() {
			checkIfPresent(types.NamespacedName{Name: argoCDNamespace}, &corev1.Namespace{})
		})

		It("argocd instance is created", func() {
			checkIfPresent(types.NamespacedName{Name: argoCDInstanceName, Namespace: argoCDNamespace}, argoCDInstance)
		})

		It("manual modification of Argo CD CR is allowed", func() {
			By("modify the Argo CD CR")
			argoCDInstance.Spec.DisableAdmin = true
			err := k8sClient.Update(context.TODO(), argoCDInstance)
			Expect(err).NotTo(HaveOccurred())

			By("check if the modification was not overwritten")
			argoCDInstance = &argoapp.ArgoCD{}
			checkIfPresent(types.NamespacedName{Name: argoCDInstanceName, Namespace: argoCDNamespace}, argoCDInstance)
			Expect(argoCDInstance.Spec.DisableAdmin).Should(BeTrue())
		})
	})

	Context("Check if gitops backend resources are created", func() {
		name := "cluster"
		It("backend deployment is created", func() {
			checkIfPresent(types.NamespacedName{Name: name, Namespace: argoCDNamespace}, &appsv1.Deployment{})
		})

		It("backend service is created", func() {
			checkIfPresent(types.NamespacedName{Name: name, Namespace: argoCDNamespace}, &corev1.Service{})
		})

		It("backend route is created", func() {
			checkIfPresent(types.NamespacedName{Name: name, Namespace: argoCDNamespace}, &routev1.Route{})
		})
	})

	Context("Check if kam resources are created", func() {
		name := "kam"
		It("deployment that hosts kam is created", func() {
			checkIfPresent(types.NamespacedName{Name: name, Namespace: argoCDNamespace}, &appsv1.Deployment{})
		})

		It("service that serves kam is created", func() {
			checkIfPresent(types.NamespacedName{Name: name, Namespace: argoCDNamespace}, &corev1.Service{})
		})

		It("console CLI download resource that adds kam route to OpenShift's CLI download page is created", func() {

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

		It("image is created", func() {
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
			}, time.Second*180, interval).ShouldNot(HaveOccurred())

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

		BeforeEach(func() {
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
		})

		It("create a sample application", func() {
			nonDefaultAppCR := filepath.Join("..", "appcrs", "non_default_appcr.yaml")
			ocPath, err := exec.LookPath("oc")
			Expect(err).NotTo(HaveOccurred())
			cmd := exec.Command(ocPath, "apply", "-f", nonDefaultAppCR)
			output, err := cmd.CombinedOutput()
			if err != nil {
				log.Println(string(output))
				Expect(err).NotTo(HaveOccurred())
			}

			By("Check if the app is healthy and in sync")
			Eventually(func() error {
				err := helper.ApplicationHealthStatus("nginx", argocdNonDefaultNamespace)
				if err != nil {
					return err
				}

				err = helper.ApplicationSyncStatus("nginx", argocdNonDefaultNamespace)
				return err
			}, time.Second*180, interval).ShouldNot(HaveOccurred())
		})

		AfterEach(func() {
			By("delete Argo CD instance")
			err := k8sClient.Delete(context.TODO(), &argoapp.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{
					Name:      argocdNonDefaultInstanceName,
					Namespace: argocdNonDefaultNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() bool {
				err := k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: argocdNonDefaultNamespace, Name: argocdNonDefaultInstanceName}, &argoapp.ArgoCD{})
				if kubeerrors.IsNotFound(err) {
					return true
				}
				return false
			}, timeout, interval).Should(BeTrue())

			By("delete test ns")
			Eventually(func() error {
				err = k8sClient.Delete(context.TODO(), &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: argocdNonDefaultNamespace,
					},
				})
				if err != nil {
					return err
				}
				return nil
			}, timeout, interval).ShouldNot(HaveOccurred())
		})
	})

	Context("Validate namespace scoped install", func() {
		standaloneArgoCDNamespace := "gitops-standalone-test"
		name := "standalone-argocd-instance"
		existingArgoInstance := &argoapp.ArgoCD{}
		BeforeEach(func() {
			By("Create a test namespace")
			newNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: standaloneArgoCDNamespace,
				},
			}
			err := k8sClient.Create(context.TODO(), newNamespace)
			if !kubeerrors.IsAlreadyExists(err) {
				Expect(err).NotTo(HaveOccurred())
			}

			By("Create new ArgoCD instance in the test namespace")
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

		It("verify that a subset of resources are created", func() {
			resourceList := []resourceList{
				{
					resource: &appsv1.Deployment{},
					expectedResources: []string{
						name + "-redis",
						name + "-repo-server",
						name + "-server",
					},
				},
				{
					resource: &corev1.ConfigMap{},
					expectedResources: []string{
						"argocd-cm",
						"argocd-gpg-keys-cm",
						"argocd-rbac-cm",
						"argocd-ssh-known-hosts-cm",
						"argocd-tls-certs-cm",
					},
				},
				{
					resource: &corev1.ServiceAccount{},
					expectedResources: []string{
						name + "-argocd-application-controller",
						name + "-argocd-server",
					},
				},
				{
					resource: &rbacv1.Role{},
					expectedResources: []string{
						name + "-argocd-application-controller",
						name + "-argocd-server",
					},
				},
				{
					resource: &rbacv1.RoleBinding{},
					expectedResources: []string{
						name + "-argocd-application-controller",
						name + "-argocd-server",
					},
				},
				{
					resource: &monitoringv1.ServiceMonitor{},
					expectedResources: []string{
						name,
						name + "-repo-server",
						name + "-server",
					},
				},
			}

			err := waitForResourcesByName(resourceList, existingArgoInstance.Namespace, time.Second*180)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			By("delete Argo CD instance")
			err := k8sClient.Delete(context.TODO(), &argoapp.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: standaloneArgoCDNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() bool {
				err := k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: standaloneArgoCDNamespace, Name: name}, &argoapp.ArgoCD{})
				if kubeerrors.IsNotFound(err) {
					return true
				}
				return false
			}, timeout, interval).Should(BeTrue())

			By("delete test ns")
			Eventually(func() error {
				err = k8sClient.Delete(context.TODO(), &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: standaloneArgoCDNamespace,
					},
				})
				if err != nil {
					return err
				}
				return nil
			}, timeout, interval).ShouldNot(HaveOccurred())
		})

	})

	Context("Verify RHSSO installation", func() {
		namespace := argoCDNamespace
		It("Update TLS", func() {
			argocd := &argoapp.ArgoCD{}
			err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: argoCDInstanceName, Namespace: namespace}, argocd)
			Expect(err).NotTo(HaveOccurred())

			insecure := false
			argocd.Spec.SSO.VerifyTLS = &insecure
			err = k8sClient.Update(context.TODO(), argocd)
			Expect(err).NotTo(HaveOccurred())
		})

		It("template instance is created", func() {
			tInstance := &templatev1.TemplateInstance{}
			checkIfPresent(types.NamespacedName{Name: defaultTemplateIdentifier, Namespace: namespace}, tInstance)
		})

		It("keycloak deployment is created", func() {
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

		It("keycloak service is created", func() {
			svc := &corev1.Service{}
			checkIfPresent(types.NamespacedName{Name: defaultKeycloakIdentifier, Namespace: namespace}, svc)
		})

		It("keycloak service route is created", func() {
			route := &routev1.Route{}
			checkIfPresent(types.NamespacedName{Name: defaultKeycloakIdentifier, Namespace: namespace}, route)
		})
	})

	Context("Verify RHSSO configuration", func() {
		namespace := argoCDNamespace

		It("verify OIDC Configuration is created", func() {
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
	})

	Context("Verify RHSSO uninstallation", func() {
		namespace := argoCDNamespace
		argocd := &argoapp.ArgoCD{}
		It("remove SSO field from Argo CD CR", func() {
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

		It("template instance is deleted", func() {
			Eventually(func() error {
				templateInstance := &templatev1.TemplateInstance{}
				err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: defaultTemplateIdentifier, Namespace: namespace}, templateInstance)
				if kubeerrors.IsNotFound(err) {
					return nil
				}
				return err
			}, timeout, interval).ShouldNot(HaveOccurred())
		})

		It("add SSO field back and verify reconcilation", func() {
			insecure := false
			argocd.Spec.SSO = &argoapp.ArgoCDSSOSpec{
				Provider:  defaultKeycloakIdentifier,
				VerifyTLS: &insecure,
			}
			err := k8sClient.Update(context.TODO(), argocd)
			Expect(err).NotTo(HaveOccurred())

			templateInstance := &templatev1.TemplateInstance{}
			checkIfPresent(types.NamespacedName{Name: defaultTemplateIdentifier, Namespace: namespace}, templateInstance)
		})
	})

	Context("Validate Cluster Config Support", func() {
		BeforeEach(func() {
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
			}, time.Second*180, interval).ShouldNot(HaveOccurred())

			namespacedName := types.NamespacedName{Name: "policy-configmap", Namespace: "openshift-config"}
			existingConfigMap := &corev1.ConfigMap{}

			checkIfPresent(namespacedName, existingConfigMap)
		})
	})

	Context("Validate granting permissions by label", func() {
		sourceNS := "source-ns"
		argocdInstance := "argocd-label"
		targetNS := "target-ns"

		BeforeEach(func() {
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
			Expect(err).NotTo(HaveOccurred())

			// Wait for the default project to exist; this avoids a race condition where the Application
			// can be created before the Project that it targets.
			Eventually(func() error {
				_, err := helper.ProjectExists("default", sourceNS)
				if err != nil {
					return err
				}
				return nil
			}, timeout, interval).ShouldNot(HaveOccurred())

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
			resourceList := []resourceList{
				{
					&rbacv1.Role{},
					[]string{
						argocdInstance + "-argocd-application-controller",
						argocdInstance + "-argocd-redis-ha",
						argocdInstance + "-argocd-server",
					},
				},
				{
					&rbacv1.RoleBinding{},
					[]string{
						argocdInstance + "-argocd-application-controller",
						argocdInstance + "-argocd-redis-ha",
						argocdInstance + "-argocd-server",
					},
				},
			}
			err := waitForResourcesByName(resourceList, targetNS, time.Second*180)
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
			}, time.Second*180, interval).ShouldNot(HaveOccurred())
		})

		It("Clean up resources", func() {
			By("delete Argo CD instance in source namespace")
			err := k8sClient.Delete(context.TODO(), &argoapp.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{
					Name:      argocdInstance,
					Namespace: sourceNS,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() bool {
				err := k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: sourceNS, Name: sourceNS}, &argoapp.ArgoCD{})
				if kubeerrors.IsNotFound(err) {
					return true
				}
				return false
			}, timeout, interval).Should(BeTrue())

			By("delete source namespace")
			Eventually(func() error {
				err = k8sClient.Delete(context.TODO(), &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: sourceNS,
					},
				})
				if err != nil {
					return err
				}
				return nil
			}, timeout, interval).ShouldNot(HaveOccurred())

			By("delete target namespace")
			Eventually(func() error {
				err = k8sClient.Delete(context.TODO(), &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: targetNS,
					},
				})
				if err != nil {
					return err
				}
				return nil
			}, timeout, interval).ShouldNot(HaveOccurred())
		})
	})

})

// resourceList is used by waitForResourcesByName
type resourceList struct {
	// resource is the type of resource to verify that it exists
	resource runtime.Object

	// expectedResources are the names of the resources of the above type
	expectedResources []string
}

// waitForResourcesByName will wait up to 'timeout' minutes for a set of resources to exist; the resources
// should be of the given type (Deployment, Service, etc) and name(s).
// Returns error if the resources could not be found within the given time frame.
func waitForResourcesByName(resourceList []resourceList, namespace string, timeout time.Duration) error {
	// Wait X seconds for all the resources to be created
	err := wait.Poll(time.Second*1, timeout, func() (bool, error) {
		for _, resourceListEntry := range resourceList {
			for _, resourceName := range resourceListEntry.expectedResources {
				resource := resourceListEntry.resource.DeepCopyObject()
				namespacedName := types.NamespacedName{Name: resourceName, Namespace: namespace}
				if err := k8sClient.Get(context.TODO(), namespacedName, resource); err != nil {
					log.Printf("Unable to retrieve expected resource %s: %v", resourceName, err)
					return false, nil
				}
				log.Printf("Able to retrieve %s: %s", resource.GetObjectKind().GroupVersionKind().Kind, resourceName)
			}
		}
		return true, nil
	})
	return err
}

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
