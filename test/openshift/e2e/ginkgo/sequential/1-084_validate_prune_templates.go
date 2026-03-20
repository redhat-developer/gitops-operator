package sequential

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	argov1alpha1 "github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	Context("1-084_validate_prune_templates", func() {
		var (
			k8sClient   client.Client
			ctx         context.Context
			ns          *corev1.Namespace
			cleanupFunc func()
		)

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()

			ns, cleanupFunc = fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()

			// permissions
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(ns), ns)
			Expect(err).ToNot(HaveOccurred())

			if ns.Labels == nil {
				ns.Labels = make(map[string]string)
			}
			ns.Labels["argocd.argoproj.io/managed-by"] = "openshift-gitops"
			Expect(k8sClient.Update(ctx, ns)).To(Succeed())
		})

		AfterEach(func() {
			defer cleanupFunc()
			fixture.OutputDebugOnFail(ns)
		})

		It("validates that resources with duplicate GVKs can be pruned successfully with local sync", func() {
			By("creating a temp dir for git repo")
			workDir, err := os.MkdirTemp("", "gitops-prune-test")
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() {
				_ = os.RemoveAll(workDir)
			})

			By("writing two OpenShift Templates (duplicate GVKs) to the working dir")
			template1 := `---
apiVersion: template.openshift.io/v1
kind: Template
metadata:
  name: redis-template-gitops
  annotations:
    description: "Description"
    iconClass: "icon-redis"
    tags: "database,nosql"
objects:
- apiVersion: v1
  kind: Pod
  metadata:
    name: redis-master
  spec:
    containers:
    - env:
      - name: REDIS_PASSWORD
        value: xyz1234s
      image: dockerfile/redis
      name: master
      ports:
      - containerPort: 6379
        protocol: TCP
parameters:
- description: Password used for Redis authentication
  from: '[A-Z0-9]{8}'
  generate: expression
  name: REDIS_PASSWORD
labels:
  redis: master
`
			template2 := `---
apiVersion: template.openshift.io/v1
kind: Template
metadata:
  name: redis-template-gitops2
  annotations:
    description: "Description"
    iconClass: "icon-redis"
    tags: "database,nosql"
objects:
- apiVersion: v1
  kind: Pod
  metadata:
    name: redis-master
  spec:
    containers:
    - env:
      - name: REDIS_PASSWORD
        value: xyz1234s
      image: dockerfile/redis
      name: master
      ports:
      - containerPort: 6379
        protocol: TCP
parameters:
- description: Password used for Redis authentication
  from: '[A-Z0-9]{8}'
  generate: expression
  name: REDIS_PASSWORD
labels:
  redis: master
`
			err = os.WriteFile(filepath.Join(workDir, "app-template.yaml"), []byte(template1), 0600)
			Expect(err).ToNot(HaveOccurred())

			err = os.WriteFile(filepath.Join(workDir, "app-template2.yaml"), []byte(template2), 0600)
			Expect(err).ToNot(HaveOccurred())

			By("logging into the Argo CD CLI")
			err = argocd.LogInToDefaultArgoCDInstance()
			Expect(err).ToNot(HaveOccurred(), "Failed to login to Argo CD")

			By("Creating ArgoCD Application CR using the typed schema")
			appName := "app-kustomize-" + ns.Name

			app := &argov1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{
					Name:      appName,
					Namespace: "openshift-gitops",
				},
				Spec: argov1alpha1.ApplicationSpec{
					Project: "default",
					Source: &argov1alpha1.ApplicationSource{
						RepoURL:        "file://" + workDir + ".git",
						Path:           ".",
						TargetRevision: "HEAD",
					},
					Destination: argov1alpha1.ApplicationDestination{
						Server:    "https://kubernetes.default.svc",
						Namespace: ns.Name,
					},
					SyncPolicy: &argov1alpha1.SyncPolicy{
						SyncOptions: argov1alpha1.SyncOptions{"PruneLast=true"},
					},
				},
			}

			Expect(k8sClient.Create(ctx, app)).To(Succeed())

			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, app)
			})

			By("syncing the application using the local dir")
			out, err := argocd.RunArgoCDCLI("app", "sync", appName, "--local", workDir, "--timeout", "100")
			Expect(err).ToNot(HaveOccurred(), "Failed to sync app with local flag: %s", out)

			By("verifying both templates were created")
			tmplObj := &unstructured.Unstructured{}
			tmplObj.SetGroupVersionKind(schema.GroupVersionKind{Group: "template.openshift.io", Version: "v1", Kind: "Template"})

			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKey{Name: "redis-template-gitops", Namespace: ns.Name}, tmplObj)
			}, "2m", "5s").Should(Succeed())

			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKey{Name: "redis-template-gitops2", Namespace: ns.Name}, tmplObj)
			}, "2m", "5s").Should(Succeed())

			By("deleting one template from the local source directory")
			err = os.Remove(filepath.Join(workDir, "app-template.yaml"))
			Expect(err).ToNot(HaveOccurred())

			By("syncing the application again this time with the prune flag enabled")
			out, err = argocd.RunArgoCDCLI("app", "sync", appName, "--local", workDir, "--prune", "--timeout", "100")
			Expect(err).ToNot(HaveOccurred(), "Failed to sync and prune app: %s", out)

			By("verifying the deleted template was pruned from the cluster")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: "redis-template-gitops", Namespace: ns.Name}, tmplObj)
				return k8serrors.IsNotFound(err)
			}, "2m", "5s").Should(BeTrue(), "Expected redis-template-gitops to be pruned, but it still exists")

			By("verifying the remaining template still exists")
			err = k8sClient.Get(ctx, client.ObjectKey{Name: "redis-template-gitops2", Namespace: ns.Name}, tmplObj)
			Expect(err).ToNot(HaveOccurred(), "Expected redis-template-gitops2 to still exist")
		})
	})
})
