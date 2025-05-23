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

package parallel

import (
	"context"
	"strings"

	argov1alpha1api "github.com/argoproj-labs/argocd-operator/api/v1alpha1"
	argov1beta1api "github.com/argoproj-labs/argocd-operator/api/v1beta1"
	argocdv1alpha1 "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	argocdFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/argocd"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/configmap"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/deployment"
	k8sFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/k8s"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/notificationsconfiguration"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/os"
	fixtureUtils "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GitOps Operator Parallel E2E Tests", func() {

	Context("1-034_validate_webhook_notifications", func() {

		var (
			k8sClient client.Client
			ctx       context.Context
		)

		BeforeEach(func() {
			fixture.EnsureParallelCleanSlate()
			k8sClient, _ = fixtureUtils.GetE2ETestKubeClient()
			ctx = context.Background()
		})

		It("ensures that NotificationsConfiguration can be used to enable notifications webhook", func() {

			By("creating namespace-scoped Argo CD instance")

			ns, cleanupFunc := fixture.CreateRandomE2ETestNamespaceWithCleanupFunc()
			defer cleanupFunc()

			By("creating webhook workload and configuration that will output to console when webhooks are received")

			service := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "webhook",
					Namespace: ns.Name,
				},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{
						"app": "webhook",
					},
					Ports: []corev1.ServicePort{
						{
							Name:       "https",
							Port:       443,
							TargetPort: intstr.FromInt(9000),
						}},
				},
			}
			Expect(k8sClient.Create(ctx, service)).To(Succeed())

			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "webhook-config",
					Namespace: ns.Name,
				},
				Data: map[string]string{
					"hooks.yaml": `- id: example
  execute-command: "/usr/bin/date"
  command-working-directory: "/var/webhook"
  incoming-payload-content-type: "application/json"
  pass-environment-to-command:
    - source: entire-payload
      envname: PAYLOAD`,
				},
			}

			Expect(k8sClient.Create(ctx, cm)).To(Succeed())

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "webhook-tls",
					Namespace: ns.Name,
				},
				StringData: map[string]string{
					// certificate is valid till 2123
					"tls.crt": `-----BEGIN CERTIFICATE-----
MIIFrjCCA5agAwIBAgIUbM9O0W6IdumLQodDCDqyckYDr2IwDQYJKoZIhvcNAQEL
BQAwTTELMAkGA1UEBhMCVVMxDTALBgNVBAgMBFRlc3QxDTALBgNVBAoMBFRlc3Qx
DTALBgNVBAsMBFRlc3QxETAPBgNVBAMMCHRlc3QuY29tMCAXDTIzMTEyNjIyMTg0
N1oYDzIxMjMxMTI3MjIxODQ3WjBNMQswCQYDVQQGEwJVUzENMAsGA1UECAwEVGVz
dDENMAsGA1UECgwEVGVzdDENMAsGA1UECwwEVGVzdDERMA8GA1UEAwwIdGVzdC5j
b20wggIiMA0GCSqGSIb3DQEBAQUAA4ICDwAwggIKAoICAQDbgAmnUjFux9u2Xzhi
mno5zjA/YsoXr3eFtK9XtByQMLLyT0hbXoa9gpTeafOs3IkCotPdN+omxm2tN9UA
ebAq+EamWyIF28EA3UbCWWULghveezrmAKSMcqQqby3knbcbGng+ZZjRdC3xc0uz
/sd4FqaLt0UHBDMlpxRskj/S3CDetfyIrKYQcZ5NQjx75aRN8At5OPC1NiWTmlsv
ppa4LLV0HR6AJzq+C6RAmJTcHQOFAq33wZEHHIpoQoGWHHPpT0ut54KIiVTRJ2o4
MEV4KlBBgL3ux4+v7R0RfVmzgaMEDG1fC9tX8pIofv7wP7WX/5XHTjyAiv8gbpUW
nLiU8FoTDZWxZN+MiCkUvZl8KqotbcUPjhnRdnq4anFwywY1lKILnCIayqzI7mPW
12h39fNwprFz9YFYbLLoQHekir2nLw8ZH83nNyD82YQ3EFm7UnOld6zw/8aURRuQ
C0oOEHyAXsvIyaWAb6lWvplDdCUGQWWr7MVp5YPPhWdtAv7B4QLDUNHGQMU/1Qrq
VBH22lcU7XrCh6GXrRVm+gF7kAuJzkuae0txvk9mHc+8Y0C4/i9C3xU2qHjWcElw
etcHbqOZjDtC8+n8mDD4hDYEMGV54VhXCKwoFLneT2no27S3SVPvNbMfyyNuUa2i
5azKnIf439Cmfww7ImxIpOR5nQIDAQABo4GDMIGAMB0GA1UdDgQWBBQfe95iWKlT
K6BGFov9JFXQTQN0ZjAfBgNVHSMEGDAWgBQfe95iWKlTK6BGFov9JFXQTQN0ZjAP
BgNVHRMBAf8EBTADAQH/MC0GA1UdEQQmMCSCB3dlYmhvb2uCDndlYmhvb2stc2Vy
dmVygglsb2NhbGhvc3QwDQYJKoZIhvcNAQELBQADggIBAH7Vv+Iar1UbF41c9I88
oIu8iWfLVnAfe/64tULy77x4nfEQiukBJDoZ9m19KEVdPqsFzT6lFB7Fu1oc9A28
5b1+PEynHcopNK41zF4n4FnIy9h8zJfaPYYCPPMT0v9LzuT5zyF5sXCz0o4KwQJ6
zrggZme8udl9sWyDxZyFoFPLWtnQFY7vJ9LSM2Gt+XUIuYNwDkvGFs6RfBYJGarX
qq7YHYj0H2x/us3KQCXGX5GzSmM9ewHvaScRpFcCdVwszKwWF0vMvdnh+3P72/Yy
dQvZXyfNiwqaIdznJn/AjzR9K4dHfbY7wMm83WHwWyjzV6CybHbtWpoUIlZtW3TT
gz6MP2z+BhOdMiQA33aO38J2TX/CMkEvkagEiZdS9t3xtpF2LOb5bRIdlENtZU0i
LnhgWEpJmswxBtuJ0d/zcyUlvK7FYoJZB7pT3YX/321HXZVCKyw+xrinwQoI3RnX
7u0TZ3MqtSKEwCyDWYRJDbs6XUX1G0q7jXBf1+3cd+lBdOZ4Kl5B4YSU9hcFxAuO
4a1eFXBdmT8PnwoTizFvag3IgBXkf8PqcKNvSMU6UKcD5LYTwRGK3JVl1L79gkrb
LmWEfOXFHgSlMIZkEs41TiopXy8p/LSera8NR86Q3mTZ7rRdEveOb6ZLJksRqaqr
UVwpFuaKz5vTCD36Gmmy/u8y
-----END CERTIFICATE-----`,
					"tls.key": `-----BEGIN PRIVATE KEY-----
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
Xq+NinfrqOLJkIZ/u/PJu4KqN3M=
-----END PRIVATE KEY-----`, // notsecret
				},
			}

			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			depl := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "webhook",
					Namespace: ns.Name,
					Labels: map[string]string{
						"app": "webhook",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr.To((int32)(1)),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "webhook",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "webhook",
							},
						},
						Spec: corev1.PodSpec{

							Containers: []corev1.Container{{
								Name:    "server",
								Image:   "quay.io/svghadi/webhook-server:latest",
								Command: []string{"/var/webhook/server"},
								Args: []string{
									"-hooks",
									"/var/webhook/hooks.yaml",
									"-cert",
									"/var/webhook/tls.crt",
									"-key",
									"/var/webhook/tls.key",
									"-secure",
									"-verbose",
								},
								Ports: []corev1.ContainerPort{
									{ContainerPort: int32(9000)},
								},
								VolumeMounts: []corev1.VolumeMount{
									{
										Name:      "webhook-tls",
										SubPath:   "tls.crt",
										MountPath: "/var/webhook/tls.crt",
									},
									{
										Name:      "webhook-tls",
										SubPath:   "tls.key",
										MountPath: "/var/webhook/tls.key",
									},
									{
										Name:      "webhook-config",
										SubPath:   "hooks.yaml",
										MountPath: "/var/webhook/hooks.yaml",
									},
								},
							}},
							Volumes: []corev1.Volume{
								{
									Name: "webhook-tls", VolumeSource: corev1.VolumeSource{
										Secret: &corev1.SecretVolumeSource{
											SecretName: "webhook-tls",
										},
									},
								},
								{
									Name: "webhook-config", VolumeSource: corev1.VolumeSource{
										ConfigMap: &corev1.ConfigMapVolumeSource{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "webhook-config",
											},
										},
									},
								},
							},
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, depl)).To(Succeed())
			Eventually(depl, "4m", "5s").Should(deployment.HaveAvailableReplicas(1))

			argocd := &argov1beta1api.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argocd",
					Namespace: ns.Name,
				},
				Spec: argov1beta1api.ArgoCDSpec{
					Notifications: argov1beta1api.ArgoCDNotifications{
						Enabled: true,
					},
					TLS: argov1beta1api.ArgoCDTLSSpec{
						InitialCerts: map[string]string{
							"webhook": `-----BEGIN CERTIFICATE-----
MIIFrjCCA5agAwIBAgIUbM9O0W6IdumLQodDCDqyckYDr2IwDQYJKoZIhvcNAQEL
BQAwTTELMAkGA1UEBhMCVVMxDTALBgNVBAgMBFRlc3QxDTALBgNVBAoMBFRlc3Qx
DTALBgNVBAsMBFRlc3QxETAPBgNVBAMMCHRlc3QuY29tMCAXDTIzMTEyNjIyMTg0
N1oYDzIxMjMxMTI3MjIxODQ3WjBNMQswCQYDVQQGEwJVUzENMAsGA1UECAwEVGVz
dDENMAsGA1UECgwEVGVzdDENMAsGA1UECwwEVGVzdDERMA8GA1UEAwwIdGVzdC5j
b20wggIiMA0GCSqGSIb3DQEBAQUAA4ICDwAwggIKAoICAQDbgAmnUjFux9u2Xzhi
mno5zjA/YsoXr3eFtK9XtByQMLLyT0hbXoa9gpTeafOs3IkCotPdN+omxm2tN9UA
ebAq+EamWyIF28EA3UbCWWULghveezrmAKSMcqQqby3knbcbGng+ZZjRdC3xc0uz
/sd4FqaLt0UHBDMlpxRskj/S3CDetfyIrKYQcZ5NQjx75aRN8At5OPC1NiWTmlsv
ppa4LLV0HR6AJzq+C6RAmJTcHQOFAq33wZEHHIpoQoGWHHPpT0ut54KIiVTRJ2o4
MEV4KlBBgL3ux4+v7R0RfVmzgaMEDG1fC9tX8pIofv7wP7WX/5XHTjyAiv8gbpUW
nLiU8FoTDZWxZN+MiCkUvZl8KqotbcUPjhnRdnq4anFwywY1lKILnCIayqzI7mPW
12h39fNwprFz9YFYbLLoQHekir2nLw8ZH83nNyD82YQ3EFm7UnOld6zw/8aURRuQ
C0oOEHyAXsvIyaWAb6lWvplDdCUGQWWr7MVp5YPPhWdtAv7B4QLDUNHGQMU/1Qrq
VBH22lcU7XrCh6GXrRVm+gF7kAuJzkuae0txvk9mHc+8Y0C4/i9C3xU2qHjWcElw
etcHbqOZjDtC8+n8mDD4hDYEMGV54VhXCKwoFLneT2no27S3SVPvNbMfyyNuUa2i
5azKnIf439Cmfww7ImxIpOR5nQIDAQABo4GDMIGAMB0GA1UdDgQWBBQfe95iWKlT
K6BGFov9JFXQTQN0ZjAfBgNVHSMEGDAWgBQfe95iWKlTK6BGFov9JFXQTQN0ZjAP
BgNVHRMBAf8EBTADAQH/MC0GA1UdEQQmMCSCB3dlYmhvb2uCDndlYmhvb2stc2Vy
dmVygglsb2NhbGhvc3QwDQYJKoZIhvcNAQELBQADggIBAH7Vv+Iar1UbF41c9I88
oIu8iWfLVnAfe/64tULy77x4nfEQiukBJDoZ9m19KEVdPqsFzT6lFB7Fu1oc9A28
5b1+PEynHcopNK41zF4n4FnIy9h8zJfaPYYCPPMT0v9LzuT5zyF5sXCz0o4KwQJ6
zrggZme8udl9sWyDxZyFoFPLWtnQFY7vJ9LSM2Gt+XUIuYNwDkvGFs6RfBYJGarX
qq7YHYj0H2x/us3KQCXGX5GzSmM9ewHvaScRpFcCdVwszKwWF0vMvdnh+3P72/Yy
dQvZXyfNiwqaIdznJn/AjzR9K4dHfbY7wMm83WHwWyjzV6CybHbtWpoUIlZtW3TT
gz6MP2z+BhOdMiQA33aO38J2TX/CMkEvkagEiZdS9t3xtpF2LOb5bRIdlENtZU0i
LnhgWEpJmswxBtuJ0d/zcyUlvK7FYoJZB7pT3YX/321HXZVCKyw+xrinwQoI3RnX
7u0TZ3MqtSKEwCyDWYRJDbs6XUX1G0q7jXBf1+3cd+lBdOZ4Kl5B4YSU9hcFxAuO
4a1eFXBdmT8PnwoTizFvag3IgBXkf8PqcKNvSMU6UKcD5LYTwRGK3JVl1L79gkrb
LmWEfOXFHgSlMIZkEs41TiopXy8p/LSera8NR86Q3mTZ7rRdEveOb6ZLJksRqaqr
UVwpFuaKz5vTCD36Gmmy/u8y
-----END CERTIFICATE-----`,
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, argocd)).To(Succeed())

			By("verifying Argo CD and related components becomes available")

			Eventually(argocd, "5m", "5s").Should(argocdFixture.BeAvailable())
			Eventually(argocd).Should(argocdFixture.HaveNotificationControllerStatus("Running"))
			Eventually(argocd).Should(argocdFixture.HaveApplicationControllerStatus("Running"))
			Eventually(argocd).Should(argocdFixture.HaveServerStatus("Running"))

			By("creating a NotificationsConfiguration that will post a message to webhook container when an app is created")
			nc := &argov1alpha1api.NotificationsConfiguration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default-notifications-configuration",
					Namespace: ns.Name,
				},
			}
			Eventually(nc).Should(k8sFixture.ExistByName())

			notificationsconfiguration.Update(nc, func(nc *argov1alpha1api.NotificationsConfiguration) {

				nc.Spec.Services = map[string]string{
					"service.webhook.test-webhook": "url: https://webhook/hooks/example",
				}
				nc.Spec.Triggers = map[string]string{
					"trigger.test-on-created": `- description: Application is created.
  send: ["test-app-created"]
  when: "true"`}
				nc.Spec.Templates = map[string]string{
					"template.test-app-created": `webhook:
  test-webhook:
    method: POST
    body: |
      {"created":"{{.app.metadata.name}}","type":"{{(call .repo.GetAppDetails).Type}}"}`,
				}
			})

			By("waiting for NotificationsConfiguration reconciler to modify argocd-notifications-cm, which will be picked up by ArgoCD")

			notifConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argocd-notifications-cm",
					Namespace: ns.Name,
				},
			}
			Eventually(notifConfigMap).Should(k8sFixture.ExistByName())
			Eventually(notifConfigMap).Should(configmap.HaveStringDataKeyValueContainsSubstring("template.test-app-created", `{"created":"{{.app.metadata.name}}","type":"{{(call .repo.GetAppDetails).Type}}"}`))

			By("creating an Argo CD Application that contains a notificatio annotation, which will trigger the notifications controller")
			app := &argocdv1alpha1.Application{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-app-3",
					Namespace: ns.Name,
					Annotations: map[string]string{
						"notifications.argoproj.io/subscribe.test-on-created.test-webhook": "",
					},
				},
				Spec: argocdv1alpha1.ApplicationSpec{
					Source: &argocdv1alpha1.ApplicationSource{
						Path:           "test/examples/nginx",
						RepoURL:        "https://github.com/redhat-developer/gitops-operator",
						TargetRevision: "HEAD",
					},
					Destination: argocdv1alpha1.ApplicationDestination{
						Namespace: ns.Name,
						Server:    "https://kubernetes.default.svc",
					},
					Project: "default",
				},
			}
			Expect(k8sClient.Create(ctx, app)).To(Succeed())

			out, err := os.ExecCommand("kubectl", "-n", ns.Name, "logs", "deployment.apps/argocd-notifications-controller")
			Expect(err).ToNot(HaveOccurred())
			Expect(out).ToNot(ContainSubstring("x509"))

			By("waiting for notifications controller to POST to the webhook workload, indicating that the workload event was successfully processed")
			Eventually(func() bool {

				out, err := os.ExecCommand("kubectl", "-n", ns.Name, "logs", "deployment.apps/webhook")
				if err != nil {
					GinkgoWriter.Println(err)
					return false
				}

				return strings.Contains(out, `{"created":"my-app-3","type":"Directory"}`)

			}).Should(BeTrue())

		})

	})
})
