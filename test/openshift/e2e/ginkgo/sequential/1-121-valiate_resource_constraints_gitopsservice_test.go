package sequential

import (
	"context"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	olmv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	gitopsoperatorv1alpha1 "github.com/redhat-developer/gitops-operator/api/v1alpha1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	clusterserviceversionFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/clusterserviceversion"
	deploymentFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	gitopsserviceFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/gitopsservice"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	osFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/os"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// --- Helper Functions ---

func getCSV(ctx context.Context, k8sClient client.Client) *olmv1alpha1.ClusterServiceVersion {
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

func getOCPVersion() string {
	output, err := osFixture.ExecCommand("oc", "version")
	Expect(err).ToNot(HaveOccurred())
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "Server Version:") {
			return strings.TrimSpace(line[strings.Index(line, ":")+1:])
		}
	}
	return ""
}

func addDynamicPluginEnv(csv *olmv1alpha1.ClusterServiceVersion, ocVersion string) {
	clusterserviceversionFixture.Update(csv, func(csv *olmv1alpha1.ClusterServiceVersion) {
		envList := csv.Spec.InstallStrategy.StrategySpec.DeploymentSpecs[0].Spec.Template.Spec.Containers[0].Env
		envList = append(envList, corev1.EnvVar{Name: "DYNAMIC_PLUGIN_START_OCP_VERSION", Value: ocVersion})
		csv.Spec.InstallStrategy.StrategySpec.DeploymentSpecs[0].Spec.Template.Spec.Containers[0].Env = envList
	})
}

func verifyResourceConstraints(k8sClient client.Client, deplName string, expectedReq, expectedLim corev1.ResourceList) {
	depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: deplName, Namespace: "openshift-gitops"}}
	Eventually(func() corev1.ResourceRequirements {
		_ = k8sClient.Get(context.Background(), client.ObjectKeyFromObject(depl), depl)
		containers := depl.Spec.Template.Spec.Containers
		if len(containers) == 0 {
			return corev1.ResourceRequirements{}
		}
		return containers[0].Resources
	}, "2m", "5s").Should(SatisfyAll(
		WithTransform(func(r corev1.ResourceRequirements) corev1.ResourceList { return r.Requests }, Equal(expectedReq)),
		WithTransform(func(r corev1.ResourceRequirements) corev1.ResourceList { return r.Limits }, Equal(expectedLim)),
	))
}

// --- Test Suite ---

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-121-validate_resource_constraints_gitopsservice_test", func() {
		var (
			ctx       context.Context
			k8sClient client.Client
		)
		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
			k8sClient, _ = utils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("validates that GitOpsService can take in custom resource constraints", func() {
			csv := getCSV(ctx, k8sClient)
			Expect(csv).ToNot(BeNil())
			defer func() { Expect(fixture.RemoveDynamicPluginFromCSV(ctx, k8sClient)).To(Succeed()) }()

			ocVersion := getOCPVersion()
			Expect(ocVersion).ToNot(BeEmpty())
			if strings.Contains(ocVersion, "4.15.") {
				Skip("skipping this test as OCP version is 4.15")
				return
			}
			addDynamicPluginEnv(csv, ocVersion)

			depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "gitops-plugin", Namespace: "openshift-gitops"}}
			Eventually(depl, "3m", "5s").Should(k8sFixture.ExistByName())
			Eventually(depl, "60s", "5s").Should(deploymentFixture.HaveReadyReplicas(1))

			Expect(k8sClient.Delete(context.Background(), &gitopsoperatorv1alpha1.GitopsService{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: "openshift-gitops"},
			})).To(Succeed())
			Eventually(func() bool {
				gitopsService := &gitopsoperatorv1alpha1.GitopsService{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: "openshift-gitops"},
				}
				err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(gitopsService), gitopsService)
				return err != nil
			}).Should(BeTrue())

			gitops := &gitopsoperatorv1alpha1.GitopsService{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: "openshift-gitops"},
				Spec: gitopsoperatorv1alpha1.GitopsServiceSpec{
					ConsolePlugin: &gitopsoperatorv1alpha1.ConsolePluginStruct{
						Backend: &gitopsoperatorv1alpha1.BackendStruct{
							Resources: &corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("200Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("300m"),
									corev1.ResourceMemory: resource.MustParse("400Mi"),
								},
							},
						},
						GitopsPlugin: &gitopsoperatorv1alpha1.GitopsPluginStruct{
							Resources: &corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("200Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("300m"),
									corev1.ResourceMemory: resource.MustParse("400Mi"),
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(context.Background(), gitops)).To(Succeed())
			Expect(gitops).To(k8sFixture.ExistByName())

			defer func() {
				gitopsserviceFixture.Update(gitops, func(gs *gitopsoperatorv1alpha1.GitopsService) {
					gs.Spec.ConsolePlugin.Backend.Resources = nil
					gs.Spec.ConsolePlugin.GitopsPlugin.Resources = nil
				})
			}()

			expectedReq := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("200Mi"),
			}
			expectedLim := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("300m"),
				corev1.ResourceMemory: resource.MustParse("400Mi"),
			}
			verifyResourceConstraints(k8sClient, "gitops-plugin", expectedReq, expectedLim)
			verifyResourceConstraints(k8sClient, "cluster", expectedReq, expectedLim)
		})

		It("validates that GitOpsService can update resource constraints", func() {
			csv := getCSV(ctx, k8sClient)
			Expect(csv).ToNot(BeNil())
			defer func() { Expect(fixture.RemoveDynamicPluginFromCSV(ctx, k8sClient)).To(Succeed()) }()

			ocVersion := getOCPVersion()
			Expect(ocVersion).ToNot(BeEmpty())
			if strings.Contains(ocVersion, "4.15.") {
				Skip("skipping this test as OCP version is 4.15")
				return
			}
			addDynamicPluginEnv(csv, ocVersion)

			depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "gitops-plugin", Namespace: "openshift-gitops"}}
			Eventually(depl, "3m", "5s").Should(k8sFixture.ExistByName())
			Eventually(depl, "60s", "5s").Should(deploymentFixture.HaveReadyReplicas(1))

			gitopsService := &gitopsoperatorv1alpha1.GitopsService{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: "openshift-gitops"},
			}
			Expect(gitopsService).To(k8sFixture.ExistByName())

			gitopsserviceFixture.Update(gitopsService, func(gs *gitopsoperatorv1alpha1.GitopsService) {
				gs.Spec.ConsolePlugin = &gitopsoperatorv1alpha1.ConsolePluginStruct{
					Backend: &gitopsoperatorv1alpha1.BackendStruct{
						Resources: &corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("123m"),
								corev1.ResourceMemory: resource.MustParse("234Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("345m"),
								corev1.ResourceMemory: resource.MustParse("456Mi"),
							},
						},
					},
					GitopsPlugin: &gitopsoperatorv1alpha1.GitopsPluginStruct{
						Resources: &corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("123m"),
								corev1.ResourceMemory: resource.MustParse("234Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("345m"),
								corev1.ResourceMemory: resource.MustParse("456Mi"),
							},
						},
					},
				}
			})

			defer func() {
				gitopsserviceFixture.Update(gitopsService, func(gs *gitopsoperatorv1alpha1.GitopsService) {
					gs.Spec.ConsolePlugin.Backend.Resources = nil
					gs.Spec.ConsolePlugin.GitopsPlugin.Resources = nil
				})
			}()

			k8sClient, _ := utils.GetE2ETestKubeClient()
			expectedReq := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("123m"),
				corev1.ResourceMemory: resource.MustParse("234Mi"),
			}
			expectedLim := corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("345m"),
				corev1.ResourceMemory: resource.MustParse("456Mi"),
			}
			verifyResourceConstraints(k8sClient, "gitops-plugin", expectedReq, expectedLim)
			verifyResourceConstraints(k8sClient, "cluster", expectedReq, expectedLim)
		})

		It("validates gitops plugin and backend can have different resource constraints", func() {
			csv := getCSV(ctx, k8sClient)
			Expect(csv).ToNot(BeNil())
			defer func() { Expect(fixture.RemoveDynamicPluginFromCSV(ctx, k8sClient)).To(Succeed()) }()

			ocVersion := getOCPVersion()
			Expect(ocVersion).ToNot(BeEmpty())
			if strings.Contains(ocVersion, "4.15.") {
				Skip("skipping this test as OCP version is 4.15")
				return
			}
			addDynamicPluginEnv(csv, ocVersion)

			depl := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "gitops-plugin", Namespace: "openshift-gitops"}}
			Eventually(depl, "3m", "5s").Should(k8sFixture.ExistByName())
			Eventually(depl, "60s", "5s").Should(deploymentFixture.HaveReadyReplicas(1))

			gitopsService := &gitopsoperatorv1alpha1.GitopsService{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: "openshift-gitops"},
			}
			Expect(gitopsService).To(k8sFixture.ExistByName())

			gitopsserviceFixture.Update(gitopsService, func(gs *gitopsoperatorv1alpha1.GitopsService) {
				gs.Spec.ConsolePlugin = &gitopsoperatorv1alpha1.ConsolePluginStruct{
					Backend: &gitopsoperatorv1alpha1.BackendStruct{
						Resources: &corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("123m"),
								corev1.ResourceMemory: resource.MustParse("234Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("345m"),
								corev1.ResourceMemory: resource.MustParse("456Mi"),
							},
						},
					},
					GitopsPlugin: &gitopsoperatorv1alpha1.GitopsPluginStruct{
						Resources: &corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("223m"),
								corev1.ResourceMemory: resource.MustParse("334Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("445m"),
								corev1.ResourceMemory: resource.MustParse("556Mi"),
							},
						},
					},
				}
			})

			defer func() {
				gitopsserviceFixture.Update(gitopsService, func(gs *gitopsoperatorv1alpha1.GitopsService) {
					gs.Spec.ConsolePlugin.Backend.Resources = nil
					gs.Spec.ConsolePlugin.GitopsPlugin.Resources = nil
				})
			}()
			k8sClient, _ := utils.GetE2ETestKubeClient()
			verifyResourceConstraints(k8sClient, "gitops-plugin",
				corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("223m"),
					corev1.ResourceMemory: resource.MustParse("334Mi"),
				},
				corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("445m"),
					corev1.ResourceMemory: resource.MustParse("556Mi"),
				},
			)
			verifyResourceConstraints(k8sClient, "cluster",
				corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("123m"),
					corev1.ResourceMemory: resource.MustParse("234Mi"),
				},
				corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("345m"),
					corev1.ResourceMemory: resource.MustParse("456Mi"),
				},
			)
		})
	})
})
