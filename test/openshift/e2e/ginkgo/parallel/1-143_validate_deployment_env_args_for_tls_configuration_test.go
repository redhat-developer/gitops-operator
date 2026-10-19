/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
*/

package parallel

import (
	"context"
	"os"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"sigs.k8s.io/controller-runtime/pkg/client"

	argov1beta1api "github.com/redhat-developer/gitops-operator/argocd-operator/api/v1beta1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/redhat-developer/gitops-operator/argocd-operator/tests/ginkgo/fixture"
	osFixture "github.com/redhat-developer/gitops-operator/argocd-operator/tests/ginkgo/fixture/os"
	"github.com/redhat-developer/gitops-operator/argocd-operator/tests/ginkgo/fixture/utils"
	gitopsFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {
	const (
		argocdInstanceName = "example-argocd"
	)
	var (
		c         client.Client
		ctx       context.Context
		ocVersion string
	)
	BeforeEach(func() {
		if fixture.EnvLocalRun() {
			Skip("This test is known not to work when running gitops operator locally")
		}
	})
	BeforeEach(func() {
		fixture.EnsureParallelCleanSlate()
		c, _ = utils.GetE2ETestKubeClient()
		ctx = context.Background()
		ocVersion = getOCPVersion()
		Expect(ocVersion).ToNot(BeEmpty())
		gitopsFixture.SkipIfMinOCPVersion(ocVersion, gitopsFixture.OCP4_22)
	})
	// --- Helper: Extract TLS values from args ---
	getTLSValues := func(args []string) (min string, hasMin bool, hasCiphers bool, ciphers string) {
		for i := 0; i < len(args); i++ {
			arg := args[i]
			// handle --tlsminversion <value>
			if arg == "--tlsminversion" {
				hasMin = true
				if i+1 < len(args) {
					min = args[i+1]
				}
			}
			if arg == "--tlsciphers" {
				hasCiphers = true
				if i+1 < len(args) {
					ciphers = args[i+1]
				}
			}
			// handle --tlsminversion=value
			if len(arg) > len("--tlsminversion=") && arg[:len("--tlsminversion=")] == "--tlsminversion=" {
				hasMin = true
				min = arg[len("--tlsminversion="):]
			}
			if len(arg) > len("--tlsciphers=") && arg[:len("--tlsciphers=")] == "--tlsciphers=" {
				hasCiphers = true
				ciphers = arg[len("--tlsciphers="):]
			}
		}
		return
	}

	Context("When the ArgoCD instance is created with default TLS settings", func() {
		It("should validate default TLS values and updates on RepoServer, Server and Redis Deployments", Label("openshift"), func() {
			argocdNamespace := "test-tls-argocd-" + string(uuid.NewUUID())[0:8]

			By("creating namespace")
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: argocdNamespace,
				},
			}
			Expect(c.Create(ctx, ns)).To(Succeed())
			DeferCleanup(func() {
				_ = c.Delete(ctx, ns)
			})

			By("generating a test certificate to use with redis, using openssl")
			redis_crt_File, err := os.CreateTemp("", "redis.crt")
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() {
				os.Remove(redis_crt_File.Name())
			})
			Expect(redis_crt_File.Close()).To(Succeed())
			redis_key_File, err := os.CreateTemp("", "redis.key")
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() {
				os.Remove(redis_key_File.Name())
			})
			Expect(redis_key_File.Close()).To(Succeed())
			openssl_test_File, err := os.CreateTemp("", "openssl_test.cnf")
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() {
				os.Remove(openssl_test_File.Name())
			})
			Expect(openssl_test_File.Close()).To(Succeed())
			opensslTestCNFContents := "\n[SAN]\nsubjectAltName=DNS:argocd-redis." + argocdNamespace + ".svc.cluster.local\n[req]\ndistinguished_name=req"
			err = os.WriteFile(openssl_test_File.Name(), ([]byte)(opensslTestCNFContents), 0600)
			Expect(err).ToNot(HaveOccurred())

			_, err = osFixture.ExecCommandWithOutputParam(false, true, "openssl", "req", "-new", "-x509", "-sha256",
				"-subj", "/C=XX/ST=XX/O=Testing/CN=redis",
				"-reqexts", "SAN",
				"-extensions", "SAN",
				"-config", openssl_test_File.Name(),
				"-keyout", redis_key_File.Name(),
				"-out", redis_crt_File.Name(),
				"-newkey", "rsa:4096",
				"-nodes",
				"-days", "10",
			)
			Expect(err).ToNot(HaveOccurred())

			By("creating argocd-operator-redis-tls secret from that cert")
			_, err = osFixture.ExecCommand("kubectl", "create", "secret", "tls", "argocd-operator-redis-tls", "--key="+redis_key_File.Name(), "--cert="+redis_crt_File.Name(), "-n", argocdNamespace)
			Expect(err).ToNot(HaveOccurred())

			By("adding argo cd label to argocd-operator-redis-tls secret")
			_, err = osFixture.ExecCommand("kubectl", "annotate", "secret", "argocd-operator-redis-tls", "argocds.argoproj.io/name=argocd", "-n", argocdNamespace)
			Expect(err).ToNot(HaveOccurred())

			By("creating ArgoCD instance")
			argo := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{
					Name:      argocdInstanceName,
					Namespace: argocdNamespace,
				},
				Spec: argov1beta1api.ArgoCDSpec{},
			}
			argo.Spec.ImageUpdater.Enabled = true
			argo.Spec.ImageUpdater.Env = append(argo.Spec.ImageUpdater.Env, corev1.EnvVar{
				Name:  "ENABLE_WEBHOOK",
				Value: "true",
			})
			Expect(c.Create(ctx, argo)).To(Succeed())
			DeferCleanup(func() {
				_ = c.Delete(ctx, argo)
			})
			By("waiting for ArgoCD to be available")
			Eventually(func() error {
				return c.Get(ctx, types.NamespacedName{Name: argocdInstanceName, Namespace: argocdNamespace}, &argov1beta1api.ArgoCD{})
			}, 2*time.Minute, 5*time.Second).Should(Succeed())
			coreDeployments := []string{
				"example-argocd-server",
				"example-argocd-repo-server",
				"example-argocd-argocd-image-updater-controller",
			}
			time.Sleep(5 * time.Second)
			// --- Validate updated TLS values ---
			By("validating updated TLS args For RepoServer and Server")
			Eventually(func() bool {
				for _, deploymentName := range coreDeployments {
					deployment := &appsv1.Deployment{}
					if err := c.Get(ctx, types.NamespacedName{Name: deploymentName, Namespace: argocdNamespace}, deployment); err != nil {
						return false
					}
					valid := false
					for _, container := range deployment.Spec.Template.Spec.Containers {
						min, hasMin, hasCiphers, ciphers := getTLSValues(container.Args)
						if !hasMin {
							continue
						}
						if min != "1.2" {
							GinkgoWriter.Printf("%s: expected tlsminversion=1.2, got %s\n", deploymentName, min)
							return false
						}
						if !hasCiphers || ciphers == "" {
							GinkgoWriter.Printf("%s: expected --tlsciphers to be present and non-empty, got %q\n", deploymentName, ciphers)
							return false
						}
						GinkgoWriter.Printf("%s updated TLS OK: min=%s\n", deploymentName, min)
						valid = true
					}
					if !valid {
						return false
					}
				}
				return true
			}, 60*time.Second, 2*time.Second).Should(BeTrue(), "all deployments should have updated TLS configuration")
			By("Validating Updated TLS args in Redis deployment")
			Eventually(func() bool {
				deployment := &appsv1.Deployment{}
				if err := c.Get(ctx, types.NamespacedName{Name: "example-argocd-redis", Namespace: argocdNamespace}, deployment); err != nil {
					return false
				}
				if len(deployment.Spec.Template.Spec.Containers) == 0 {
					return false
				}
				args := deployment.Spec.Template.Spec.Containers[0].Args
				var tlsProtocols string
				var tlsCiphersTLS12 string
				var tlsCiphersTLS13 string
				hasProtocols := false
				hasCiphersTLS12 := false
				hasCiphersTLS13 := false
				for i := 0; i < len(args); i++ {
					arg := args[i]
					// --- Handle "--tls-protocols <value>"
					if arg == "--tls-protocols" {
						hasProtocols = true
						if i+1 < len(args) {
							tlsProtocols = args[i+1]
						}
					}
					if arg == "--tls-ciphersuites" {
						hasCiphersTLS13 = true
						if i+1 < len(args) {
							tlsCiphersTLS13 = args[i+1]
						}
					}

					if arg == "--tls-ciphers" {
						hasCiphersTLS12 = true
						if i+1 < len(args) {
							tlsCiphersTLS12 = args[i+1]
						}
					}
				}

				if !hasCiphersTLS13 || tlsCiphersTLS13 == "" {
					GinkgoWriter.Printf("  --tls-ciphersuites should not be empty, got %q\n", tlsCiphersTLS13)
					return false
				}
				if !hasCiphersTLS12 || tlsCiphersTLS12 == "" {
					GinkgoWriter.Printf("  --tls-ciphers should not be empty, got %q\n", tlsCiphersTLS12)
					return false
				}

				if !hasProtocols || tlsProtocols != "TLSv1.2" {
					GinkgoWriter.Printf("%s: expected --tls-protocols=TLSv1.2, got %s\n", deployment.Name, tlsProtocols)
					return false
				}
				GinkgoWriter.Printf("%s TLS args protocol value: %s\n", deployment.Name, tlsProtocols)
				GinkgoWriter.Printf("%s TLS args ciphersuites value: %s\n", deployment.Name, tlsCiphersTLS13)
				GinkgoWriter.Printf("%s TLS args ciphers value: %s\n", deployment.Name, tlsCiphersTLS12)
				return true
			}, 60*time.Second, 2*time.Second).Should(BeTrue())
			By("Validating TLS environment variables in cluster deployment")
			Eventually(func() error {
				depl := &appsv1.Deployment{}
				if err := c.Get(ctx, types.NamespacedName{
					Name:      "cluster",
					Namespace: "openshift-gitops",
				}, depl); err != nil {
					return err
				}
				Expect(depl.Spec.Template.Spec.Containers).NotTo(BeEmpty())
				env := depl.Spec.Template.Spec.Containers[0].Env
				Expect(env).To(ContainElement(corev1.EnvVar{
					Name:  "TLS_MIN_VERSION",
					Value: "1.2",
				}))
				Expect(env).To(ContainElement(corev1.EnvVar{
					Name: "TLS_CIPHER_SUITES",
					// OpenSSL names from the cluster TLS profile are mapped to IANA/Go
					// names; TLS 1.3 suites are omitted when min version is 1.2.
					Value: "TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256:TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256:" +
						"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384:TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384:" +
						"TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256:TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256",
				}))

				return nil
			}, 60*time.Second, 2*time.Second).Should(Succeed())
		})
	})
})
