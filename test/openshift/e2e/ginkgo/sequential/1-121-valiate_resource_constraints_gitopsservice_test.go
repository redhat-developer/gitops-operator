package sequential

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	gitopsoperatorv1alpha1 "github.com/redhat-developer/gitops-operator/api/v1alpha1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	gitopsserviceFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/gitopsservice"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-121-validate_resource_constraints_gitopsservice_test", func() {
		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
		})

		It("validates that GitOpsService can take in custom resource constraints", func() {

			By("ensuring the GitOpsService CR is created with Resource constraints set")
			k8sClient, _ := utils.GetE2ETestKubeClient()
			// Clean up the GitOpsService CR so we can test patching it next
			Expect(k8sClient.Delete(context.Background(), &gitopsoperatorv1alpha1.GitopsService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster",
					Namespace: "openshift-gitops",
				},
			})).To(Succeed())
			// Ensure the GitOpsService CR is deleted before proceeding
			Eventually(func() bool {
				gitopsService := &gitopsoperatorv1alpha1.GitopsService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster",
						Namespace: "openshift-gitops",
					},
				}
				err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(gitopsService), gitopsService)
				return err != nil
			}).Should(BeTrue())

			gitops := &gitopsoperatorv1alpha1.GitopsService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster",
					Namespace: "openshift-gitops",
				},
				Spec: gitopsoperatorv1alpha1.GitopsServiceSpec{
					ConsolePlugin: gitopsoperatorv1alpha1.ConsolePluginResourceStruct{
						Backend: gitopsoperatorv1alpha1.BackendResourceStruct{
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
						GitopsPlugin: gitopsoperatorv1alpha1.GitopsPluginResourceStruct{
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

			time.Sleep(90 * time.Second) // Increased time for the operator to react to the new CR
			// Ensure the change is reverted when the test exits
			defer func() {
				gitopsserviceFixture.Update(gitops, func(gs *gitopsoperatorv1alpha1.GitopsService) {
					gs.Spec.ConsolePlugin.Backend.Resources = nil
					gs.Spec.ConsolePlugin.GitopsPlugin.Resources = nil
				})
			}()
			By("verifying the openshift-gitops resources have honoured the resource constraints")
			clusterDepl := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster",
					Namespace: "openshift-gitops",
				},
			}
			gitopsPluginDepl := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gitops-plugin",
					Namespace: "openshift-gitops",
				},
			}
			// Verify the resource constraints are honoured for gitops-plugin deployment
			Eventually(func() corev1.ResourceRequirements {
				_ = k8sClient.Get(context.Background(), client.ObjectKeyFromObject(gitopsPluginDepl), gitopsPluginDepl)
				containers := gitopsPluginDepl.Spec.Template.Spec.Containers
				if len(containers) == 0 {
					return corev1.ResourceRequirements{}
				}
				return containers[0].Resources
			}, "3m", "5s").Should(SatisfyAll(
				WithTransform(func(r corev1.ResourceRequirements) corev1.ResourceList { return r.Requests }, Equal(corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("100m"),
					corev1.ResourceMemory: resource.MustParse("200Mi"),
				})),
				WithTransform(func(r corev1.ResourceRequirements) corev1.ResourceList { return r.Limits }, Equal(corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("300m"),
					corev1.ResourceMemory: resource.MustParse("400Mi"),
				})),
			))

			// Verify the resource constraints are honoured for cluster deployment
			Eventually(func() corev1.ResourceRequirements {
				_ = k8sClient.Get(context.Background(), client.ObjectKeyFromObject(clusterDepl), clusterDepl)
				containers := clusterDepl.Spec.Template.Spec.Containers
				if len(containers) == 0 {
					return corev1.ResourceRequirements{}
				}
				return containers[0].Resources
			}, "3m", "5s").Should(SatisfyAll(
				WithTransform(func(r corev1.ResourceRequirements) corev1.ResourceList { return r.Requests }, Equal(corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("100m"),
					corev1.ResourceMemory: resource.MustParse("200Mi"),
				})),
				WithTransform(func(r corev1.ResourceRequirements) corev1.ResourceList { return r.Limits }, Equal(corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("300m"),
					corev1.ResourceMemory: resource.MustParse("400Mi"),
				})),
			))
		})

		It("validates that GitOpsService can update resource constraints", func() {
			By("enabling resource constraints on GitOpsService CR as a patch")
			gitopsService := &gitopsoperatorv1alpha1.GitopsService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster",
					Namespace: "openshift-gitops",
				},
			}
			Expect(gitopsService).To(k8sFixture.ExistByName())

			// Set resource constraints
			gitopsserviceFixture.Update(gitopsService, func(gs *gitopsoperatorv1alpha1.GitopsService) {
				gs.Spec.ConsolePlugin = gitopsoperatorv1alpha1.ConsolePluginResourceStruct{
					Backend: gitopsoperatorv1alpha1.BackendResourceStruct{
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
					GitopsPlugin: gitopsoperatorv1alpha1.GitopsPluginResourceStruct{
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
			time.Sleep(90 * time.Second) // Increased time for the operator to react to the new CR

			// Ensure the change is reverted when the test exits
			defer func() {
				gitopsserviceFixture.Update(gitopsService, func(gs *gitopsoperatorv1alpha1.GitopsService) {
					gs.Spec.ConsolePlugin.Backend.Resources = nil
					gs.Spec.ConsolePlugin.GitopsPlugin.Resources = nil
				})
			}()

			k8sClient, _ := utils.GetE2ETestKubeClient()
			By("verifying the openshift-gitops resources have honoured the resource constraints")
			clusterDepl := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster",
					Namespace: "openshift-gitops",
				},
			}
			gitopsPluginDepl := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gitops-plugin",
					Namespace: "openshift-gitops",
				},
			}
			// Now you can safely check for available replicas and resource requirements
			// Verify the resource constraints are honoured for gitops-plugin deployment
			Eventually(func() corev1.ResourceRequirements {
				_ = k8sClient.Get(context.Background(), client.ObjectKeyFromObject(gitopsPluginDepl), gitopsPluginDepl)
				containers := gitopsPluginDepl.Spec.Template.Spec.Containers
				if len(containers) == 0 {
					return corev1.ResourceRequirements{}
				}
				return containers[0].Resources
			}, "3m", "5s").Should(SatisfyAll(
				WithTransform(func(r corev1.ResourceRequirements) corev1.ResourceList { return r.Requests }, Equal(corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("123m"),
					corev1.ResourceMemory: resource.MustParse("234Mi"),
				})),
				WithTransform(func(r corev1.ResourceRequirements) corev1.ResourceList { return r.Limits }, Equal(corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("345m"),
					corev1.ResourceMemory: resource.MustParse("456Mi"),
				})),
			))
			// Verify the resource constraints are honoured for cluster deployment
			Eventually(func() corev1.ResourceRequirements {
				_ = k8sClient.Get(context.Background(), client.ObjectKeyFromObject(clusterDepl), clusterDepl)
				containers := clusterDepl.Spec.Template.Spec.Containers
				if len(containers) == 0 {
					return corev1.ResourceRequirements{}
				}
				return containers[0].Resources
			}, "3m", "5s").Should(SatisfyAll(
				WithTransform(func(r corev1.ResourceRequirements) corev1.ResourceList { return r.Requests }, Equal(corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("123m"),
					corev1.ResourceMemory: resource.MustParse("234Mi"),
				})),
				WithTransform(func(r corev1.ResourceRequirements) corev1.ResourceList { return r.Limits }, Equal(corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("345m"),
					corev1.ResourceMemory: resource.MustParse("456Mi"),
				})),
			))

		})
		It("validates gitops plugin and backend can have different resource constraints", func() {
			By("enabling resource constraints on GitOpsService CR as a patch")
			gitopsService := &gitopsoperatorv1alpha1.GitopsService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster",
					Namespace: "openshift-gitops",
				},
			}
			Expect(gitopsService).To(k8sFixture.ExistByName())

			// Set resource constraints
			gitopsserviceFixture.Update(gitopsService, func(gs *gitopsoperatorv1alpha1.GitopsService) {
				gs.Spec.ConsolePlugin = gitopsoperatorv1alpha1.ConsolePluginResourceStruct{
					Backend: gitopsoperatorv1alpha1.BackendResourceStruct{
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
					GitopsPlugin: gitopsoperatorv1alpha1.GitopsPluginResourceStruct{
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

			time.Sleep(90 * time.Second) // Increased time for the operator to react to the new CR
			// Ensure the change is reverted when the test exits
			defer func() {
				gitopsserviceFixture.Update(gitopsService, func(gs *gitopsoperatorv1alpha1.GitopsService) {
					gs.Spec.ConsolePlugin.Backend.Resources = nil
					gs.Spec.ConsolePlugin.GitopsPlugin.Resources = nil
				})
			}()
			k8sClient, _ := utils.GetE2ETestKubeClient()
			By("verifying the openshift-gitops resources have honoured the resource constraints")
			clusterDepl := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster",
					Namespace: "openshift-gitops",
				},
			}
			gitopsPluginDepl := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gitops-plugin",
					Namespace: "openshift-gitops",
				},
			}
			// Now you can safely check for available replicas and resource requirements
			// Verify the resource constraints are honoured for gitops-plugin deployment
			Eventually(func() corev1.ResourceRequirements {
				_ = k8sClient.Get(context.Background(), client.ObjectKeyFromObject(gitopsPluginDepl), gitopsPluginDepl)
				containers := gitopsPluginDepl.Spec.Template.Spec.Containers
				if len(containers) == 0 {
					return corev1.ResourceRequirements{}
				}
				return containers[0].Resources
			}, "3m", "5s").Should(SatisfyAll(
				WithTransform(func(r corev1.ResourceRequirements) corev1.ResourceList { return r.Requests }, Equal(corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("223m"),
					corev1.ResourceMemory: resource.MustParse("334Mi"),
				})),
				WithTransform(func(r corev1.ResourceRequirements) corev1.ResourceList { return r.Limits }, Equal(corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("445m"),
					corev1.ResourceMemory: resource.MustParse("556Mi"),
				})),
			))
			// Verify the resource constraints are honoured for cluster deployment
			Eventually(func() corev1.ResourceRequirements {
				_ = k8sClient.Get(context.Background(), client.ObjectKeyFromObject(clusterDepl), clusterDepl)
				containers := clusterDepl.Spec.Template.Spec.Containers
				if len(containers) == 0 {
					return corev1.ResourceRequirements{}
				}
				return containers[0].Resources
			}, "3m", "5s").Should(SatisfyAll(
				WithTransform(func(r corev1.ResourceRequirements) corev1.ResourceList { return r.Requests }, Equal(corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("123m"),
					corev1.ResourceMemory: resource.MustParse("234Mi"),
				})),
				WithTransform(func(r corev1.ResourceRequirements) corev1.ResourceList { return r.Limits }, Equal(corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("345m"),
					corev1.ResourceMemory: resource.MustParse("456Mi"),
				})),
			))
		})
	})
})
