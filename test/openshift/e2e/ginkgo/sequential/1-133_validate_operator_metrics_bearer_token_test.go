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

// E2E coverage for OperatorMetricsTokenReconciler. These tests run sequentially
// because they mutate shared resources in openshift-gitops-operator (the metrics
// bearer token Secret and ServiceMonitor). Mutating cases snapshot and restore
// cluster state so later tests are not affected.
package sequential

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	secretFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/secret"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// Shared operator namespace and resources managed by OperatorMetricsTokenReconciler.
	operatorMetricsNS                    = "openshift-gitops-operator"
	operatorMetricsMonitorName           = "openshift-gitops-operator-metrics-monitor"
	operatorMetricsBearerTokenSecretName = "openshift-gitops-operator-metrics-monitor-bearer-token"
	operatorMetricsBearerTokenKey        = "token"
	operatorMetricsBearerTokenExpiryKey  = "expiry"
	operatorMetricsControllerSAName      = "openshift-gitops-operator-controller-manager"

	// Mirror controller renewal settings: 20% of the requested one-hour TTL (~12 minutes).
	operatorMetricsTokenExpiry         = time.Minute * 10
	operatorMetricsTokenRenewalPercent = 20

	// Bumping this annotation triggers a reconcile via the ServiceMonitor or Secret watch.
	operatorMetricsTriggerAnnotation = "test.gitops.redhat.com/trigger-metrics-token-reconcile"
)

// operatorMetricsBearerTokenSecret returns an empty Secret object reference for matchers and updates.
func operatorMetricsBearerTokenSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      operatorMetricsBearerTokenSecretName,
			Namespace: operatorMetricsNS,
		},
	}
}

// operatorMetricsServiceMonitor returns an empty ServiceMonitor object reference for matchers and updates.
func operatorMetricsServiceMonitor() *monitoringv1.ServiceMonitor {
	return &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      operatorMetricsMonitorName,
			Namespace: operatorMetricsNS,
		},
	}
}

// operatorMetricsBearerTokenRenewalLead matches bearerTokenRenewalLead in the controller.
func operatorMetricsBearerTokenRenewalLead() time.Duration {
	return operatorMetricsTokenExpiry / 10
}

// snapshotOperatorMetricsBearerTokenResources waits for and deep-copies the current
// metrics bearer token Secret and ServiceMonitor so tests can restore them afterward.
func snapshotOperatorMetricsBearerTokenResources(ctx context.Context, k8sClient client.Client) (*corev1.Secret, *monitoringv1.ServiceMonitor) {
	serviceMonitor := operatorMetricsServiceMonitor()
	Eventually(serviceMonitor).Should(k8sFixture.ExistByName())
	Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(serviceMonitor), serviceMonitor)).To(Succeed())

	bearerTokenSecret := operatorMetricsBearerTokenSecret()
	Eventually(bearerTokenSecret).Should(k8sFixture.ExistByName())
	Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(bearerTokenSecret), bearerTokenSecret)).To(Succeed())

	return bearerTokenSecret.DeepCopy(), serviceMonitor.DeepCopy()
}

// restoreOperatorMetricsBearerTokenResources puts the metrics bearer token Secret and
// ServiceMonitor back to the state captured by snapshotOperatorMetricsBearerTokenResources.
func restoreOperatorMetricsBearerTokenResources(
	ctx context.Context,
	k8sClient client.Client,
	originalSecret *corev1.Secret,
	originalServiceMonitor *monitoringv1.ServiceMonitor,
) {
	if originalServiceMonitor != nil {
		desired := originalServiceMonitor.DeepCopy()
		k8sFixture.Update(operatorMetricsServiceMonitor(), func(obj client.Object) {
			current := obj.(*monitoringv1.ServiceMonitor)
			current.Spec = desired.Spec
			current.Annotations = desired.Annotations
			current.Labels = desired.Labels
		})
	}

	if originalSecret == nil {
		return
	}

	desired := originalSecret.DeepCopy()
	existing := operatorMetricsBearerTokenSecret()
	err := k8sClient.Get(ctx, client.ObjectKeyFromObject(existing), existing)
	if apierrors.IsNotFound(err) {
		Expect(k8sClient.Create(ctx, desired)).To(Succeed())
		return
	}
	Expect(err).NotTo(HaveOccurred())
	secretFixture.Update(existing, func(secret *corev1.Secret) {
		secret.Type = desired.Type
		secret.Data = desired.Data
		secret.Annotations = desired.Annotations
		secret.Labels = desired.Labels
	})
}

// triggerOperatorMetricsTokenReconcileViaServiceMonitor enqueues the reconciler through
// the metrics ServiceMonitor watch by updating a test-only annotation.
func triggerOperatorMetricsTokenReconcileViaServiceMonitor() {
	k8sFixture.Update(operatorMetricsServiceMonitor(), func(obj client.Object) {
		current := obj.(*monitoringv1.ServiceMonitor)
		if current.Annotations == nil {
			current.Annotations = map[string]string{}
		}
		current.Annotations[operatorMetricsTriggerAnnotation] = time.Now().Format(time.RFC3339Nano)
	})
}

// triggerOperatorMetricsTokenReconcileViaSecret enqueues the reconciler through the
// bearer token Secret watch by updating a test-only annotation.
func triggerOperatorMetricsTokenReconcileViaSecret() {
	secretFixture.Update(operatorMetricsBearerTokenSecret(), func(secret *corev1.Secret) {
		if secret.Annotations == nil {
			secret.Annotations = map[string]string{}
		}
		secret.Annotations[operatorMetricsTriggerAnnotation] = time.Now().Format(time.RFC3339Nano)
	})
}

var _ = Describe("GitOps Operator Sequential E2E Tests", func() {

	// OperatorMetricsTokenReconciler creates the metrics ServiceMonitor after minting
	// the bearer token Secret. These cases exercise renewal, migration, and drift correction.
	Context("1-133_validate_operator_metrics_bearer_token", func() {

		var (
			ctx       context.Context
			k8sClient client.Client
		)

		BeforeEach(func() {
			fixture.EnsureSequentialCleanSlate()
			k8sClient, _ = utils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("verifies metrics bearer token Secret stores token and expiry with valid timestamps", func() {
			if fixture.EnvLocalRun() || fixture.EnvNonOLM() {
				Skip("this test requires the operator to be installed via OLM in openshift-gitops-operator namespace")
			}

			By("waiting for the operator metrics bearer token Secret to exist")
			bearerTokenSecret := operatorMetricsBearerTokenSecret()
			Eventually(bearerTokenSecret).Should(k8sFixture.ExistByName())
			Eventually(bearerTokenSecret).Should(secretFixture.HaveNonEmptyKeyValue(operatorMetricsBearerTokenKey))
			Eventually(bearerTokenSecret).Should(secretFixture.HaveNonEmptyKeyValue(operatorMetricsBearerTokenExpiryKey))

			By("verifying the bearer token Secret is Opaque with a future expiry")
			Expect(bearerTokenSecret.Type).To(Equal(corev1.SecretTypeOpaque))
			expiry, err := time.Parse(time.RFC3339, string(bearerTokenSecret.Data[operatorMetricsBearerTokenExpiryKey]))
			Expect(err).NotTo(HaveOccurred())
			Expect(expiry.After(time.Now())).To(BeTrue())

			By("restores ServiceMonitor authorization when deprecated bearerTokenSecret is set")

			By("setting deprecated bearerTokenSecret on the operator metrics ServiceMonitor")
			k8sFixture.Update(operatorMetricsServiceMonitor(), func(obj client.Object) {
				current := obj.(*monitoringv1.ServiceMonitor)
				endpoint := &current.Spec.Endpoints[0]
				endpoint.Authorization = nil
				endpoint.BearerTokenSecret = &corev1.SecretKeySelector{ //nolint:staticcheck // SA1019: test migration from deprecated bearerTokenSecret
					LocalObjectReference: corev1.LocalObjectReference{
						Name: operatorMetricsBearerTokenSecretName,
					},
					Key: operatorMetricsBearerTokenKey,
				}
			})

			By("waiting for the operator to restore Bearer authorization on the ServiceMonitor")
			Eventually(func(g Gomega) {
				current := operatorMetricsServiceMonitor()
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(current), current)).To(Succeed())
				endpoint := current.Spec.Endpoints[0]
				g.Expect(endpoint.Authorization).NotTo(BeNil())
				g.Expect(endpoint.Authorization.Type).To(Equal("Bearer"))
				g.Expect(endpoint.Authorization.Credentials).NotTo(BeNil())
				g.Expect(endpoint.Authorization.Credentials.Name).To(Equal(operatorMetricsBearerTokenSecretName))
				g.Expect(endpoint.Authorization.Credentials.Key).To(Equal(operatorMetricsBearerTokenKey))
				g.Expect(endpoint.BearerTokenSecret).To(BeNil()) //nolint:staticcheck // SA1019: deprecated bearerTokenSecret must be cleared
			}, "2m", "5s").Should(Succeed())

		})

		It("refreshes metrics bearer token when expiry is in the past", func() {
			if fixture.EnvLocalRun() || fixture.EnvNonOLM() {
				Skip("this test requires the operator to be installed via OLM in openshift-gitops-operator namespace")
			}

			By("capturing current metrics bearer token Secret and ServiceMonitor state")
			originalSecret, originalServiceMonitor := snapshotOperatorMetricsBearerTokenResources(ctx, k8sClient)
			defer restoreOperatorMetricsBearerTokenResources(ctx, k8sClient, originalSecret, originalServiceMonitor)

			By("setting bearer token expiry to a timestamp in the past")
			bearerTokenSecret := operatorMetricsBearerTokenSecret()
			Eventually(bearerTokenSecret).Should(k8sFixture.ExistByName())
			originalToken := string(bearerTokenSecret.Data[operatorMetricsBearerTokenKey])

			secretFixture.Update(bearerTokenSecret, func(secret *corev1.Secret) {
				secret.Data[operatorMetricsBearerTokenExpiryKey] = []byte(time.Now().Add(-time.Hour).UTC().Format(time.RFC3339))
			})

			By("triggering operator metrics token reconciliation")
			triggerOperatorMetricsTokenReconcileViaServiceMonitor()

			By("waiting for the bearer token Secret to be refreshed")
			Eventually(func(g Gomega) {
				secret := operatorMetricsBearerTokenSecret()
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(secret), secret)).To(Succeed())
				expiry, err := time.Parse(time.RFC3339, string(secret.Data[operatorMetricsBearerTokenExpiryKey]))
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(expiry.After(time.Now())).To(BeTrue())
				g.Expect(string(secret.Data[operatorMetricsBearerTokenKey])).NotTo(Equal(originalToken))
			}, "2m", "5s").Should(Succeed())

			By("refreshes metrics bearer token when token key is missing")

			By("removing the token key from the bearer token Secret")
			secretFixture.Update(operatorMetricsBearerTokenSecret(), func(secret *corev1.Secret) {
				delete(secret.Data, operatorMetricsBearerTokenKey)
				secret.Data[operatorMetricsBearerTokenExpiryKey] = []byte(time.Now().Add(operatorMetricsTokenExpiry).UTC().Format(time.RFC3339))
			})

			By("triggering operator metrics token reconciliation")
			triggerOperatorMetricsTokenReconcileViaServiceMonitor()

			By("waiting for the bearer token Secret to be repopulated")
			Eventually(operatorMetricsBearerTokenSecret()).Should(secretFixture.HaveNonEmptyKeyValue(operatorMetricsBearerTokenKey))
			Eventually(operatorMetricsBearerTokenSecret()).Should(secretFixture.HaveNonEmptyKeyValue(operatorMetricsBearerTokenExpiryKey))

			By("refreshes metrics bearer token when expiry is unparseable")

			By("setting bearer token expiry to an invalid value")
			bearerTokenSecret = operatorMetricsBearerTokenSecret()
			Eventually(bearerTokenSecret).Should(k8sFixture.ExistByName())
			originalToken = string(bearerTokenSecret.Data[operatorMetricsBearerTokenKey])

			secretFixture.Update(bearerTokenSecret, func(secret *corev1.Secret) {
				secret.Data[operatorMetricsBearerTokenExpiryKey] = []byte("not-a-valid-rfc3339-timestamp")
			})

			By("triggering operator metrics token reconciliation")
			triggerOperatorMetricsTokenReconcileViaServiceMonitor()

			By("waiting for the bearer token Secret to be refreshed")
			Eventually(func(g Gomega) {
				secret := operatorMetricsBearerTokenSecret()
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(secret), secret)).To(Succeed())
				expiry, err := time.Parse(time.RFC3339, string(secret.Data[operatorMetricsBearerTokenExpiryKey]))
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(expiry.After(time.Now())).To(BeTrue())
				g.Expect(string(secret.Data[operatorMetricsBearerTokenKey])).NotTo(Equal(originalToken))
			}, "2m", "5s").Should(Succeed())
		})

		It("does not refresh valid metrics bearer token before renewal deadline", func() {
			if fixture.EnvLocalRun() || fixture.EnvNonOLM() {
				Skip("this test requires the operator to be installed via OLM in openshift-gitops-operator namespace")
			}

			By("capturing current metrics bearer token Secret and ServiceMonitor state")
			originalSecret, originalServiceMonitor := snapshotOperatorMetricsBearerTokenResources(ctx, k8sClient)
			defer restoreOperatorMetricsBearerTokenResources(ctx, k8sClient, originalSecret, originalServiceMonitor)

			By("setting a valid bearer token with a far-future expiry")
			markerToken := "e2e-valid-metrics-token-marker"
			secretFixture.Update(operatorMetricsBearerTokenSecret(), func(secret *corev1.Secret) {
				secret.Data[operatorMetricsBearerTokenKey] = []byte(markerToken)
				secret.Data[operatorMetricsBearerTokenExpiryKey] = []byte(time.Now().Add(operatorMetricsTokenExpiry).UTC().Format(time.RFC3339))
			})

			By("triggering operator metrics token reconciliation")
			triggerOperatorMetricsTokenReconcileViaServiceMonitor()
			triggerOperatorMetricsTokenReconcileViaSecret()

			By("verifying the bearer token is not refreshed early")
			Consistently(func(g Gomega) {
				secret := operatorMetricsBearerTokenSecret()
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(secret), secret)).To(Succeed())
				g.Expect(string(secret.Data[operatorMetricsBearerTokenKey])).To(Equal(markerToken))
			}, "30s", "3s").Should(Succeed())

			By("refreshes metrics bearer token at renewal deadline")

			By("setting bearer token expiry to the renewal deadline")
			markerToken = "e2e-due-metrics-token-marker"
			secretFixture.Update(operatorMetricsBearerTokenSecret(), func(secret *corev1.Secret) {
				secret.Data[operatorMetricsBearerTokenKey] = []byte(markerToken)
				secret.Data[operatorMetricsBearerTokenExpiryKey] = []byte(time.Now().Add(operatorMetricsBearerTokenRenewalLead()).UTC().Format(time.RFC3339))
			})

			By("triggering operator metrics token reconciliation")
			triggerOperatorMetricsTokenReconcileViaSecret()

			By("waiting for the bearer token Secret to be refreshed")
			Eventually(func(g Gomega) {
				secret := operatorMetricsBearerTokenSecret()
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(secret), secret)).To(Succeed())
				g.Expect(string(secret.Data[operatorMetricsBearerTokenKey])).NotTo(Equal(markerToken))

				expiry, err := time.Parse(time.RFC3339, string(secret.Data[operatorMetricsBearerTokenExpiryKey]))
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(expiry.After(time.Now().Add(operatorMetricsBearerTokenRenewalLead()))).To(BeTrue())
			}, "2m", "5s").Should(Succeed())
		})
	})
})
