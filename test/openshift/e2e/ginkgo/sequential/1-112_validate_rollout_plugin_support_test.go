package sequential

import (
	"context"

	rolloutmanagerv1alpha1 "github.com/argoproj-labs/argo-rollouts-manager/api/v1alpha1"
	rolloutsOperator "github.com/argoproj-labs/argo-rollouts-manager/controllers"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/configmap"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-112_validate_rollout_plugin_support", func() {

		var (
			ctx       context.Context
			k8sClient client.Client
		)

		BeforeEach(func() {

			fixture.EnsureSequentialCleanSlate()
			k8sClient, _ = utils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("verifies that custom traffic management and metrics plugins can be added to Argo Rollouts instance via RolloutManager CR", func() {

			By("creating a new Argo Rollouts instance in openshift-gitops namespace, containing a custom traffic management plugin and a custom metrics plugin")
			rm := &rolloutmanagerv1alpha1.RolloutManager{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "example-rollout-manager",
					Namespace: "openshift-gitops",
				},
				Spec: rolloutmanagerv1alpha1.RolloutManagerSpec{
					Plugins: rolloutmanagerv1alpha1.Plugins{
						TrafficManagement: []rolloutmanagerv1alpha1.Plugin{
							{
								Name:     "argoproj-labs/gatewayAPI",
								Location: "https://github.com/argoproj-labs/rollouts-plugin-trafficrouter-gatewayapi/releases/download/v0.4.0/gatewayapi-plugin-linux-amd64",
							},
						},
						Metric: []rolloutmanagerv1alpha1.Plugin{
							{
								Name:     "argoproj-labs/sample-prometheus",
								Location: "https://github.com/argoproj-labs/sample-rollouts-metric-plugin/releases/download/v0.0.4/metric-plugin-linux-amd64",
								SHA256:   "af83581a496cebad569c6ddca4e1b7beef1c6f51573d6cd235cebe4390d3a767",
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, rm)).To(Succeed())

			defer func() {
				Expect(k8sClient.Delete(ctx, rm)).To(Succeed()) // Cleanup on exit
			}()

			By("verifying Argo Rollouts resources are successfully created in the namespace")
			rolloutsServiceAcct := &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argo-rollouts",
					Namespace: "openshift-gitops",
				},
			}
			Eventually(rolloutsServiceAcct).Should(k8sFixture.ExistByName())

			clusterRolesToCheck := []string{
				"argo-rollouts",
				"argo-rollouts-aggregate-to-admin",
				"argo-rollouts-aggregate-to-edit",
				"argo-rollouts-aggregate-to-view",
			}

			for _, clusterRoleToCheck := range clusterRolesToCheck {
				clusterRole := &rbacv1.ClusterRole{
					ObjectMeta: metav1.ObjectMeta{
						Name: clusterRoleToCheck,
					},
				}
				Eventually(clusterRole).Should(k8sFixture.ExistByName())
			}

			clusterRoleBinding := &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name: "argo-rollouts",
				},
			}
			Eventually(clusterRoleBinding).Should(k8sFixture.ExistByName())

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argo-rollouts-notification-secret",
					Namespace: "openshift-gitops",
				},
			}
			Eventually(secret).Should(k8sFixture.ExistByName())

			depl := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argo-rollouts",
					Namespace: "openshift-gitops",
				},
			}
			Eventually(depl).Should(k8sFixture.ExistByName())
			Eventually(depl, "3m", "5s").Should(deployment.HaveReadyReplicas(1))

			metricsService := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argo-rollouts-metrics",
					Namespace: "openshift-gitops",
				},
			}
			Eventually(metricsService).Should(k8sFixture.ExistByName())

			By("verifying argo-rollouts-config ConfigMap contains the plugin values we specified above")
			rolloutsConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argo-rollouts-config",
					Namespace: "openshift-gitops",
				},
			}
			Eventually(rolloutsConfigMap).Should(k8sFixture.ExistByName())
			Eventually(rolloutsConfigMap).Should(configmap.HaveStringDataKeyValue("metricProviderPlugins", `
- name: argoproj-labs/sample-prometheus
  location: https://github.com/argoproj-labs/sample-rollouts-metric-plugin/releases/download/v0.0.4/metric-plugin-linux-amd64
  sha256: af83581a496cebad569c6ddca4e1b7beef1c6f51573d6cd235cebe4390d3a767`))

			By("verifying the trafficRouterPlugin contains both gatewayAPI, AND our openshift route plugin")

			expectedTrafficRouterPluginsVal := `
- name: argoproj-labs/gatewayAPI
  location: https://github.com/argoproj-labs/rollouts-plugin-trafficrouter-gatewayapi/releases/download/v0.4.0/gatewayapi-plugin-linux-amd64
  sha256: ""`

			if fixture.EnvLocalRun() || fixture.EnvCI() {
				// When running the operator locally, the value comes from 'DefaultOpenShiftRoutePluginURL' constant
				expectedTrafficRouterPluginsVal += `
- name: argoproj-labs/openshift
  location: ` + rolloutsOperator.DefaultOpenShiftRoutePluginURL + `
  sha256: ""`
			} else {
				// Otherwise, the openshift-route-plugin binary will likely be mounted on the filesystem
				expectedTrafficRouterPluginsVal += `
- name: argoproj-labs/openshift
  location: file:/plugins/rollouts-trafficrouter-openshift/openshift-route-plugin
  sha256: ""`
			}
			Eventually(rolloutsConfigMap).Should(configmap.HaveStringDataKeyValue("trafficRouterPlugins", expectedTrafficRouterPluginsVal))

		})

	})

})
