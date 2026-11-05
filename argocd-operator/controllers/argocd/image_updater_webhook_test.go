package argocd

import (
	"context"
	"testing"

	promoter "github.com/argoproj-labs/gitops-promoter/api/v1alpha1"
	configv1 "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	testclient "k8s.io/client-go/kubernetes/fake"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	argoproj "github.com/redhat-developer/gitops-operator/argocd-operator/api/v1beta1"
	"github.com/redhat-developer/gitops-operator/argocd-operator/common"
	"github.com/redhat-developer/gitops-operator/argocd-operator/controllers/argoutil"
)

func makeImageUpdaterWebhookReconciler(t *testing.T, objs ...client.Object) *ReconcileArgoCD {
	t.Helper()
	sch := makeTestReconcilerScheme(argoproj.AddToScheme, promoter.AddToScheme, apiregistrationv1.AddToScheme, configv1.Install, routev1.Install)
	cl := makeTestReconcilerClient(sch, objs, objs, []runtime.Object{})
	return makeTestReconciler(cl, sch, testclient.NewSimpleClientset())
}

// setRouteAPIFoundForTest marks the OpenShift Route API as available for the duration of the test.
func setRouteAPIFoundForTest(t *testing.T) {
	t.Helper()
	previous := argoutil.IsRouteAPIAvailable()
	argoutil.SetRouteAPIFound(true)
	t.Cleanup(func() { argoutil.SetRouteAPIFound(previous) })
}

func makeImageUpdaterWebhookArgoCD(opts ...argoCDOpt) *argoproj.ArgoCD {
	return makeTestArgoCD(append([]argoCDOpt{func(a *argoproj.ArgoCD) {
		a.Spec.ImageUpdater.Enabled = true
		a.Spec.ImageUpdater.WebhookServer.Route.Enabled = true
	}}, opts...)...)
}

func TestImageUpdaterWebhookEnv(t *testing.T) {
	fallback := func(envName, key string) corev1.EnvVar {
		return corev1.EnvVar{
			Name: envName,
			ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: ArgocdImageUpdaterSecret},
				Key:                  key,
				Optional:             new(true),
			}},
		}
	}

	t.Run("webhook not exposed leaves env untouched", func(t *testing.T) {
		cr := makeTestArgoCD(func(a *argoproj.ArgoCD) { a.Spec.ImageUpdater.Enabled = true })
		env := []corev1.EnvVar{{Name: "ENABLE_WEBHOOK", Value: "true"}, {Name: "DISABLE_TLS", Value: "true"}}
		assert.Equal(t, env, imageUpdaterWebhookEnv(cr, env))
	})

	t.Run("secure mode", func(t *testing.T) {
		cr := makeImageUpdaterWebhookArgoCD()
		env := imageUpdaterWebhookEnv(cr, []corev1.EnvVar{{Name: "DISABLE_TLS", Value: "true"}})

		assert.Equal(t, &corev1.EnvVar{Name: "ENABLE_WEBHOOK", Value: "true"}, argoutil.EnvGet(env, "ENABLE_WEBHOOK"))
		assert.Nil(t, argoutil.EnvGet(env, "DISABLE_TLS"), "user DISABLE_TLS must be removed in secure mode")
		for _, reg := range imageUpdaterWebhookRegistries {
			want := fallback(reg.envName, reg.fallbackKey)
			assert.Equal(t, &want, argoutil.EnvGet(env, reg.envName))
		}
	})

	t.Run("insecure mode", func(t *testing.T) {
		cr := makeImageUpdaterWebhookArgoCD(func(a *argoproj.ArgoCD) {
			a.Spec.ImageUpdater.WebhookServer.Insecure = true
		})
		env := imageUpdaterWebhookEnv(cr, nil)
		assert.Equal(t, &corev1.EnvVar{Name: "DISABLE_TLS", Value: "true"}, argoutil.EnvGet(env, "DISABLE_TLS"))
	})

	t.Run("secret precedence: CR reference, then user env, then fallback", func(t *testing.T) {
		cr := makeImageUpdaterWebhookArgoCD(func(a *argoproj.ArgoCD) {
			a.Spec.ImageUpdater.WebhookServer.Secrets = &argoproj.ImageUpdaterWebhookSecretsSpec{
				Quay:   &argoproj.WebhookSecretKeySelector{Name: "my-quay", Key: "token"},
				Harbor: &argoproj.WebhookSecretKeySelector{Name: "my-harbor", Key: "secret"},
			}
		})
		env := imageUpdaterWebhookEnv(cr, []corev1.EnvVar{
			{Name: "DOCKER_WEBHOOK_SECRET", Value: "from-user-env"},
			{Name: "HARBOR_WEBHOOK_SECRET", Value: "overridden-by-cr"},
		})

		assert.Equal(t, &corev1.EnvVar{
			Name: "QUAY_WEBHOOK_SECRET",
			ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "my-quay"},
				Key:                  "token",
				Optional:             new(true),
			}},
		}, argoutil.EnvGet(env, "QUAY_WEBHOOK_SECRET"))
		assert.Equal(t, "my-harbor", argoutil.EnvGet(env, "HARBOR_WEBHOOK_SECRET").ValueFrom.SecretKeyRef.Name)
		assert.Equal(t, &corev1.EnvVar{Name: "DOCKER_WEBHOOK_SECRET", Value: "from-user-env"}, argoutil.EnvGet(env, "DOCKER_WEBHOOK_SECRET"))
		want := fallback("GHCR_WEBHOOK_SECRET", "webhook.ghcr-secret")
		assert.Equal(t, &want, argoutil.EnvGet(env, "GHCR_WEBHOOK_SECRET"))
	})
}

func TestReconcileImageUpdaterWebhookService(t *testing.T) {
	setRouteAPIFoundForTest(t)
	cr := makeImageUpdaterWebhookArgoCD()
	r := makeImageUpdaterWebhookReconciler(t, cr)
	key := types.NamespacedName{Name: testArgoCDName + "-image-updater-webhook", Namespace: testNamespace}

	require.NoError(t, r.reconcileImageUpdaterWebhookService(cr))
	svc := &corev1.Service{}
	require.NoError(t, r.Get(context.TODO(), key, svc))
	assert.Equal(t, []corev1.ServicePort{{
		Name:       "webhook",
		Port:       8082,
		Protocol:   corev1.ProtocolTCP,
		TargetPort: intstr.FromInt32(8082),
	}}, svc.Spec.Ports)
	assert.Equal(t, map[string]string{common.ArgoCDKeyName: testArgoCDName + "-argocd-image-updater-controller"}, svc.Spec.Selector)
	assert.Equal(t, testArgoCDName+"-image-updater-webhook-tls", svc.Annotations[common.AnnotationOpenShiftServiceCA])

	// Switching to insecure mode removes the serving certificate request.
	cr.Spec.ImageUpdater.WebhookServer.Insecure = true
	require.NoError(t, r.reconcileImageUpdaterWebhookService(cr))
	require.NoError(t, r.Get(context.TODO(), key, svc))
	assert.NotContains(t, svc.Annotations, common.AnnotationOpenShiftServiceCA)

	// Drifted ports are corrected.
	svc.Spec.Ports[0].Port = 9999
	require.NoError(t, r.Update(context.TODO(), svc))
	require.NoError(t, r.reconcileImageUpdaterWebhookService(cr))
	require.NoError(t, r.Get(context.TODO(), key, svc))
	assert.Equal(t, int32(8082), svc.Spec.Ports[0].Port)

	// The Service is deleted once neither a Route nor an Ingress is enabled.
	cr.Spec.ImageUpdater.WebhookServer.Route.Enabled = false
	require.NoError(t, r.reconcileImageUpdaterWebhookService(cr))
	assert.True(t, errors.IsNotFound(r.Get(context.TODO(), key, svc)))
}

func TestReconcileImageUpdaterWebhookService_AutoTLSFollowsRouteTermination(t *testing.T) {
	setRouteAPIFoundForTest(t)
	cr := makeImageUpdaterWebhookArgoCD(func(a *argoproj.ArgoCD) {
		a.Spec.ImageUpdater.WebhookServer.Route.TLS = &routev1.TLSConfig{Termination: routev1.TLSTerminationPassthrough}
	})
	r := makeImageUpdaterWebhookReconciler(t, cr)
	key := types.NamespacedName{Name: testArgoCDName + "-image-updater-webhook", Namespace: testNamespace}
	tlsSecretName := testArgoCDName + "-image-updater-webhook-tls"

	// Passthrough presents the backend certificate to external clients, so the service CA is not asked for one.
	require.NoError(t, r.reconcileImageUpdaterWebhookService(cr))
	svc := &corev1.Service{}
	require.NoError(t, r.Get(context.TODO(), key, svc))
	assert.NotContains(t, svc.Annotations, common.AnnotationOpenShiftServiceCA)

	// The webhook server still serves TLS, from a certificate the user places in the same Secret.
	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "sa", Namespace: testNamespace}}
	require.NoError(t, r.reconcileImageUpdaterDeployment(cr, sa, "", nil))
	deployment := &appsv1.Deployment{}
	require.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: testArgoCDName + "-argocd-image-updater-controller", Namespace: testNamespace}, deployment))
	assert.Contains(t, deployment.Spec.Template.Spec.Volumes, imageUpdaterWebhookTLSVolume(cr))
	assert.Nil(t, argoutil.EnvGet(deployment.Spec.Template.Spec.Containers[0].Env, "DISABLE_TLS"))

	// Switching back to reencrypt requests the certificate again.
	cr.Spec.ImageUpdater.WebhookServer.Route.TLS.Termination = routev1.TLSTerminationReencrypt
	require.NoError(t, r.reconcileImageUpdaterWebhookService(cr))
	require.NoError(t, r.Get(context.TODO(), key, svc))
	assert.Equal(t, tlsSecretName, svc.Annotations[common.AnnotationOpenShiftServiceCA])

	// A termination override with no value keeps the reencrypt default.
	cr.Spec.ImageUpdater.WebhookServer.Route.TLS = &routev1.TLSConfig{InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyNone}
	assert.True(t, wantsImageUpdaterWebhookAutoTLS(cr))

	cr.Spec.ImageUpdater.WebhookServer.Route.TLS = &routev1.TLSConfig{Termination: routev1.TLSTerminationEdge}
	require.NoError(t, r.reconcileImageUpdaterWebhookService(cr))
	require.NoError(t, r.Get(context.TODO(), key, svc))
	assert.NotContains(t, svc.Annotations, common.AnnotationOpenShiftServiceCA)

	// The Route termination is irrelevant when only the Ingress is enabled.
	cr.Spec.ImageUpdater.WebhookServer.Route.Enabled = false
	cr.Spec.ImageUpdater.WebhookServer.Ingress.Enabled = true
	assert.True(t, wantsImageUpdaterWebhookAutoTLS(cr))
}

func TestReconcileImageUpdaterWebhookRoute(t *testing.T) {
	setRouteAPIFoundForTest(t)
	cr := makeImageUpdaterWebhookArgoCD()
	ingressConfig := &configv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Spec:       configv1.IngressSpec{Domain: "apps.example.com"},
	}
	r := makeImageUpdaterWebhookReconciler(t, cr, ingressConfig)
	key := types.NamespacedName{Name: testArgoCDName + "-image-updater-webhook", Namespace: testNamespace}

	require.NoError(t, r.reconcileImageUpdaterWebhookRoute(cr))
	route := &routev1.Route{}
	require.NoError(t, r.Get(context.TODO(), key, route))
	assert.Equal(t, testArgoCDName+"-image-updater-webhook-"+testNamespace+".apps.example.com", route.Spec.Host)
	assert.Equal(t, "/webhook", route.Spec.Path)
	assert.Equal(t, intstr.FromString("webhook"), route.Spec.Port.TargetPort)
	assert.Equal(t, testArgoCDName+"-image-updater-webhook", route.Spec.To.Name)
	assert.Equal(t, &routev1.TLSConfig{
		Termination:                   routev1.TLSTerminationReencrypt,
		InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyRedirect,
	}, route.Spec.TLS)

	t.Run("insecure mode defaults to edge termination", func(t *testing.T) {
		cr.Spec.ImageUpdater.WebhookServer.Insecure = true
		require.NoError(t, r.reconcileImageUpdaterWebhookRoute(cr))
		require.NoError(t, r.Get(context.TODO(), key, route))
		assert.Equal(t, routev1.TLSTerminationEdge, route.Spec.TLS.Termination)
		cr.Spec.ImageUpdater.WebhookServer.Insecure = false
	})

	t.Run("custom host and TLS override", func(t *testing.T) {
		cr.Spec.ImageUpdater.WebhookServer.Host = "webhook.example.org"
		cr.Spec.ImageUpdater.WebhookServer.Route.TLS = &routev1.TLSConfig{
			Termination:                   routev1.TLSTerminationPassthrough,
			InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyNone,
		}
		require.NoError(t, r.reconcileImageUpdaterWebhookRoute(cr))
		require.NoError(t, r.Get(context.TODO(), key, route))
		assert.Equal(t, "webhook.example.org", route.Spec.Host)
		assert.Equal(t, routev1.TLSTerminationPassthrough, route.Spec.TLS.Termination)
		assert.Equal(t, routev1.InsecureEdgeTerminationPolicyNone, route.Spec.TLS.InsecureEdgeTerminationPolicy)
		assert.Empty(t, route.Spec.Path, "passthrough routes cannot have a path")
	})

	t.Run("unchanged route is not updated", func(t *testing.T) {
		require.NoError(t, r.Get(context.TODO(), key, route))
		resourceVersion := route.ResourceVersion
		require.NoError(t, r.reconcileImageUpdaterWebhookRoute(cr))
		require.NoError(t, r.Get(context.TODO(), key, route))
		assert.Equal(t, resourceVersion, route.ResourceVersion)
	})

	t.Run("route is deleted when disabled", func(t *testing.T) {
		cr.Spec.ImageUpdater.WebhookServer.Route.Enabled = false
		require.NoError(t, r.reconcileImageUpdaterWebhookRoute(cr))
		assert.True(t, errors.IsNotFound(r.Get(context.TODO(), key, route)))
	})

	t.Run("route is deleted when image updater is disabled", func(t *testing.T) {
		cr.Spec.ImageUpdater.WebhookServer.Route.Enabled = true
		require.NoError(t, r.reconcileImageUpdaterWebhookRoute(cr))
		require.NoError(t, r.Get(context.TODO(), key, route))

		cr.Spec.ImageUpdater.Enabled = false
		require.NoError(t, r.reconcileImageUpdaterWebhookRoute(cr))
		assert.True(t, errors.IsNotFound(r.Get(context.TODO(), key, route)))
	})
}

func TestReconcileImageUpdaterWebhookIngress(t *testing.T) {
	cr := makeTestArgoCD(func(a *argoproj.ArgoCD) {
		a.Spec.ImageUpdater.Enabled = true
		a.Spec.ImageUpdater.WebhookServer.Host = "webhook.example.org"
		a.Spec.ImageUpdater.WebhookServer.Ingress.Enabled = true
	})
	r := makeImageUpdaterWebhookReconciler(t, cr)
	key := types.NamespacedName{Name: testArgoCDName + "-image-updater-webhook", Namespace: testNamespace}

	require.NoError(t, r.reconcileImageUpdaterWebhookIngress(cr))
	ingress := &networkingv1.Ingress{}
	require.NoError(t, r.Get(context.TODO(), key, ingress))
	assert.Equal(t, "HTTPS", ingress.Annotations[common.ArgoCDKeyIngressBackendProtocol])
	require.Len(t, ingress.Spec.Rules, 1)
	rule := ingress.Spec.Rules[0]
	assert.Equal(t, "webhook.example.org", rule.Host)
	assert.Equal(t, "/webhook", rule.HTTP.Paths[0].Path)
	assert.Equal(t, testArgoCDName+"-image-updater-webhook", rule.HTTP.Paths[0].Backend.Service.Name)
	assert.Equal(t, "webhook", rule.HTTP.Paths[0].Backend.Service.Port.Name)

	// User annotations are merged on top of the operator defaults instead of replacing them, so that an
	// unrelated annotation such as a cert-manager issuer does not drop the TLS backend protocol.
	cr.Spec.ImageUpdater.WebhookServer.Ingress.Annotations = map[string]string{
		"cert-manager.io/cluster-issuer": "letsencrypt",
	}
	require.NoError(t, r.reconcileImageUpdaterWebhookIngress(cr))
	require.NoError(t, r.Get(context.TODO(), key, ingress))
	assert.Equal(t, "letsencrypt", ingress.Annotations["cert-manager.io/cluster-issuer"])
	assert.Equal(t, "HTTPS", ingress.Annotations[common.ArgoCDKeyIngressBackendProtocol])
	assert.Equal(t, "true", ingress.Annotations[common.ArgoCDKeyIngressSSLRedirect])

	// An explicit user value still wins over the operator default.
	cr.Spec.ImageUpdater.WebhookServer.Ingress.Annotations[common.ArgoCDKeyIngressBackendProtocol] = "HTTP"
	require.NoError(t, r.reconcileImageUpdaterWebhookIngress(cr))
	require.NoError(t, r.Get(context.TODO(), key, ingress))
	assert.Equal(t, "HTTP", ingress.Annotations[common.ArgoCDKeyIngressBackendProtocol])

	cr.Spec.ImageUpdater.WebhookServer.Ingress.Annotations = nil

	// Switching to insecure mode updates the backend protocol of the existing Ingress.
	cr.Spec.ImageUpdater.WebhookServer.Insecure = true
	require.NoError(t, r.reconcileImageUpdaterWebhookIngress(cr))
	require.NoError(t, r.Get(context.TODO(), key, ingress))
	assert.Equal(t, "HTTP", ingress.Annotations[common.ArgoCDKeyIngressBackendProtocol])

	cr.Spec.ImageUpdater.WebhookServer.Ingress.Enabled = false
	require.NoError(t, r.reconcileImageUpdaterWebhookIngress(cr))
	assert.True(t, errors.IsNotFound(r.Get(context.TODO(), key, ingress)))
}

func TestReconcileImageUpdaterDeployment_Webhook(t *testing.T) {
	setRouteAPIFoundForTest(t)
	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "sa", Namespace: testNamespace}}
	deploymentKey := types.NamespacedName{Name: testArgoCDName + "-argocd-image-updater-controller", Namespace: testNamespace}

	getDeployment := func(t *testing.T, r *ReconcileArgoCD) *appsv1.Deployment {
		t.Helper()
		deployment := &appsv1.Deployment{}
		require.NoError(t, r.Get(context.TODO(), deploymentKey, deployment))
		return deployment
	}
	hasVolume := func(d *appsv1.Deployment) bool {
		for _, v := range d.Spec.Template.Spec.Volumes {
			if v.Name == imageUpdaterWebhookTLSVolumeName {
				return true
			}
		}
		return false
	}
	hasVolumeMount := func(d *appsv1.Deployment) bool {
		for _, m := range d.Spec.Template.Spec.Containers[0].VolumeMounts {
			if m.Name == imageUpdaterWebhookTLSVolumeName && m.MountPath == "/app/config/webhook/tls" {
				return true
			}
		}
		return false
	}

	t.Run("webhook not exposed", func(t *testing.T) {
		cr := makeTestArgoCD(func(a *argoproj.ArgoCD) { a.Spec.ImageUpdater.Enabled = true })
		r := makeImageUpdaterWebhookReconciler(t, cr)
		require.NoError(t, r.reconcileImageUpdaterDeployment(cr, sa, "", nil))
		d := getDeployment(t, r)
		assert.False(t, hasVolume(d))
		assert.NotContains(t, d.Spec.Template.Labels, imageUpdaterWebhookChecksumLabel)
		assert.Nil(t, argoutil.EnvGet(d.Spec.Template.Spec.Containers[0].Env, "ENABLE_WEBHOOK"))
	})

	t.Run("secure and insecure modes", func(t *testing.T) {
		cr := makeImageUpdaterWebhookArgoCD()
		r := makeImageUpdaterWebhookReconciler(t, cr)
		require.NoError(t, r.reconcileImageUpdaterDeployment(cr, sa, "", nil))
		d := getDeployment(t, r)
		assert.True(t, hasVolume(d))
		assert.True(t, hasVolumeMount(d))
		assert.Equal(t, "true", argoutil.EnvGet(d.Spec.Template.Spec.Containers[0].Env, "ENABLE_WEBHOOK").Value)
		assert.Nil(t, argoutil.EnvGet(d.Spec.Template.Spec.Containers[0].Env, "DISABLE_TLS"))
		assert.NotEmpty(t, d.Spec.Template.Labels[imageUpdaterWebhookChecksumLabel])

		cr.Spec.ImageUpdater.WebhookServer.Insecure = true
		require.NoError(t, r.reconcileImageUpdaterDeployment(cr, sa, "", nil))
		d = getDeployment(t, r)
		assert.False(t, hasVolume(d))
		assert.False(t, hasVolumeMount(d))
		assert.Equal(t, "true", argoutil.EnvGet(d.Spec.Template.Spec.Containers[0].Env, "DISABLE_TLS").Value)
	})

	t.Run("checksum changes with the serving certificate and referenced secrets", func(t *testing.T) {
		cr := makeImageUpdaterWebhookArgoCD(func(a *argoproj.ArgoCD) {
			a.Spec.ImageUpdater.WebhookServer.Secrets = &argoproj.ImageUpdaterWebhookSecretsSpec{
				Quay: &argoproj.WebhookSecretKeySelector{Name: "quay-webhook", Key: "secret"},
			}
		})
		r := makeImageUpdaterWebhookReconciler(t, cr)
		checksum := func() string {
			require.NoError(t, r.reconcileImageUpdaterDeployment(cr, sa, "", nil))
			return getDeployment(t, r).Spec.Template.Labels[imageUpdaterWebhookChecksumLabel]
		}

		initial := checksum()
		assert.Equal(t, initial, checksum(), "checksum must be stable")

		// The serving certificate is issued after the pod first starts.
		tlsSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: testArgoCDName + "-image-updater-webhook-tls", Namespace: testNamespace},
			Type:       corev1.SecretTypeTLS,
			Data:       map[string][]byte{corev1.TLSCertKey: []byte("crt"), corev1.TLSPrivateKeyKey: []byte("key")},
		}
		require.NoError(t, r.Create(context.TODO(), tlsSecret))
		withCert := checksum()
		assert.NotEqual(t, initial, withCert)

		quaySecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "quay-webhook", Namespace: testNamespace},
			Data:       map[string][]byte{"secret": []byte("v1")},
		}
		require.NoError(t, r.Create(context.TODO(), quaySecret))
		withQuay := checksum()
		assert.NotEqual(t, withCert, withQuay)

		quaySecret.Data["secret"] = []byte("v2")
		require.NoError(t, r.Update(context.TODO(), quaySecret))
		assert.NotEqual(t, withQuay, checksum())

		// Fallback keys in argocd-image-updater-secret are also tracked.
		before := checksum()
		require.NoError(t, r.Create(context.TODO(), &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: ArgocdImageUpdaterSecret, Namespace: testNamespace},
			Data:       map[string][]byte{"webhook.docker-secret": []byte("docker")},
		}))
		assert.NotEqual(t, before, checksum())
	})
}

func TestImageUpdaterWebhookConfigIssues(t *testing.T) {
	tests := []struct {
		name       string
		opts       argoCDOpt
		objs       []client.Object
		wantIssues int
	}{
		{
			name:       "webhook not exposed",
			opts:       func(a *argoproj.ArgoCD) { a.Spec.ImageUpdater.WebhookServer.Route.Enabled = false },
			wantIssues: 0,
		},
		{
			name:       "no secret configured",
			opts:       func(a *argoproj.ArgoCD) {},
			wantIssues: 1,
		},
		{
			name: "secret declared in CR",
			opts: func(a *argoproj.ArgoCD) {
				a.Spec.ImageUpdater.WebhookServer.Secrets = &argoproj.ImageUpdaterWebhookSecretsSpec{
					GHCR: &argoproj.WebhookSecretKeySelector{Name: "s", Key: "k"},
				}
			},
			objs: []client.Object{&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "s", Namespace: testNamespace},
				Data:       map[string][]byte{"k": []byte("v")},
			}},
			wantIssues: 0,
		},
		{
			name: "secret declared in CR does not exist",
			opts: func(a *argoproj.ArgoCD) {
				a.Spec.ImageUpdater.WebhookServer.Secrets = &argoproj.ImageUpdaterWebhookSecretsSpec{
					GHCR: &argoproj.WebhookSecretKeySelector{Name: "s", Key: "k"},
				}
			},
			wantIssues: 1,
		},
		{
			name: "key declared in CR does not exist",
			opts: func(a *argoproj.ArgoCD) {
				a.Spec.ImageUpdater.WebhookServer.Secrets = &argoproj.ImageUpdaterWebhookSecretsSpec{
					GHCR:        &argoproj.WebhookSecretKeySelector{Name: "s", Key: "typo"},
					CloudEvents: &argoproj.WebhookSecretKeySelector{Name: "s", Key: "k"},
				}
			},
			objs: []client.Object{&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "s", Namespace: testNamespace},
				Data:       map[string][]byte{"k": []byte("v")},
			}},
			wantIssues: 1,
		},
		{
			name: "secret set in user env",
			opts: func(a *argoproj.ArgoCD) {
				a.Spec.ImageUpdater.Env = []corev1.EnvVar{{Name: "HARBOR_WEBHOOK_SECRET", Value: "s"}}
			},
			wantIssues: 0,
		},
		{
			name: "secret set in argocd-image-updater-secret",
			opts: func(a *argoproj.ArgoCD) {},
			objs: []client.Object{&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: ArgocdImageUpdaterSecret, Namespace: testNamespace},
				Data:       map[string][]byte{"webhook.quay-secret": []byte("s")},
			}},
			wantIssues: 0,
		},
		{
			name: "secret enforcement disabled",
			opts: func(a *argoproj.ArgoCD) {
				a.Spec.ImageUpdater.Env = []corev1.EnvVar{{Name: "WEBHOOK_REQUIRE_SECRET", Value: "false"}}
			},
			wantIssues: 0,
		},
		{
			name: "ENABLE_WEBHOOK overridden to true",
			opts: func(a *argoproj.ArgoCD) {
				a.Spec.ImageUpdater.Env = []corev1.EnvVar{{Name: "WEBHOOK_REQUIRE_SECRET", Value: "false"}, {Name: "ENABLE_WEBHOOK", Value: "false"}}
			},
			wantIssues: 1,
		},
		{
			name: "ENABLE_WEBHOOK already true",
			opts: func(a *argoproj.ArgoCD) {
				a.Spec.ImageUpdater.Env = []corev1.EnvVar{{Name: "WEBHOOK_REQUIRE_SECRET", Value: "false"}, {Name: "ENABLE_WEBHOOK", Value: "true"}}
			},
			wantIssues: 0,
		},
		{
			name: "DISABLE_TLS contradicting secure mode",
			opts: func(a *argoproj.ArgoCD) {
				a.Spec.ImageUpdater.Env = []corev1.EnvVar{{Name: "WEBHOOK_REQUIRE_SECRET", Value: "false"}, {Name: "DISABLE_TLS", Value: "true"}}
			},
			wantIssues: 1,
		},
		{
			name: "DISABLE_TLS matching secure mode",
			opts: func(a *argoproj.ArgoCD) {
				a.Spec.ImageUpdater.Env = []corev1.EnvVar{{Name: "WEBHOOK_REQUIRE_SECRET", Value: "false"}, {Name: "DISABLE_TLS", Value: "false"}}
			},
			wantIssues: 0,
		},
		{
			name: "DISABLE_TLS matching insecure mode",
			opts: func(a *argoproj.ArgoCD) {
				a.Spec.ImageUpdater.Env = []corev1.EnvVar{{Name: "WEBHOOK_REQUIRE_SECRET", Value: "false"}, {Name: "DISABLE_TLS", Value: "true"}}
				a.Spec.ImageUpdater.WebhookServer.Insecure = true
			},
			wantIssues: 0,
		},
		{
			name: "DISABLE_TLS from a ConfigMap cannot be compared",
			opts: func(a *argoproj.ArgoCD) {
				a.Spec.ImageUpdater.Env = []corev1.EnvVar{
					{Name: "WEBHOOK_REQUIRE_SECRET", Value: "false"},
					{Name: "DISABLE_TLS", ValueFrom: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: ArgocdImageUpdaterConfigCM},
						Key:                  "disable-tls",
					}}},
				}
			},
			wantIssues: 1,
		},
		{
			name: "webhook secret set in both env and CR",
			opts: func(a *argoproj.ArgoCD) {
				a.Spec.ImageUpdater.Env = []corev1.EnvVar{{Name: "QUAY_WEBHOOK_SECRET", Value: "from-env"}}
				a.Spec.ImageUpdater.WebhookServer.Secrets = &argoproj.ImageUpdaterWebhookSecretsSpec{
					Quay: &argoproj.WebhookSecretKeySelector{Name: "s", Key: "k"},
				}
			},
			objs: []client.Object{&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "s", Namespace: testNamespace},
				Data:       map[string][]byte{"k": []byte("v")},
			}},
			wantIssues: 1,
		},
		{
			name: "leftover WEBHOOK_PORT",
			opts: func(a *argoproj.ArgoCD) {
				a.Spec.ImageUpdater.Env = []corev1.EnvVar{{Name: "WEBHOOK_REQUIRE_SECRET", Value: "false"}, {Name: "WEBHOOK_PORT", Value: "8080"}}
			},
			wantIssues: 1,
		},
		{
			name: "WEBHOOK_PORT matching the fixed port",
			opts: func(a *argoproj.ArgoCD) {
				a.Spec.ImageUpdater.Env = []corev1.EnvVar{{Name: "WEBHOOK_REQUIRE_SECRET", Value: "false"}, {Name: "WEBHOOK_PORT", Value: "8082"}}
			},
			wantIssues: 0,
		},
		{
			name: "leftover TLS cert and key paths in secure mode",
			opts: func(a *argoproj.ArgoCD) {
				a.Spec.ImageUpdater.Env = []corev1.EnvVar{
					{Name: "WEBHOOK_REQUIRE_SECRET", Value: "false"},
					{Name: "TLS_CERT_PATH", Value: "/app/config/tls/tls.crt"},
					{Name: "TLS_KEY_PATH", Value: "/app/config/tls/tls.key"},
				}
			},
			wantIssues: 2,
		},
		{
			name: "leftover TLS cert path in insecure mode",
			opts: func(a *argoproj.ArgoCD) {
				a.Spec.ImageUpdater.Env = []corev1.EnvVar{{Name: "WEBHOOK_REQUIRE_SECRET", Value: "false"}, {Name: "TLS_CERT_PATH", Value: "/app/config/tls/tls.crt"}}
				a.Spec.ImageUpdater.WebhookServer.Insecure = true
			},
			wantIssues: 0,
		},
		{
			name: "image override older than v1.4.0 in secure mode",
			opts: func(a *argoproj.ArgoCD) {
				a.Spec.ImageUpdater.Env = []corev1.EnvVar{{Name: "WEBHOOK_REQUIRE_SECRET", Value: "false"}}
				a.Spec.ImageUpdater.Image = DefaultImageUpdaterImage + ":v1.3.0"
			},
			wantIssues: 1,
		},
		{
			name: "image override older than v1.4.0 in insecure mode",
			opts: func(a *argoproj.ArgoCD) {
				a.Spec.ImageUpdater.Env = []corev1.EnvVar{{Name: "WEBHOOK_REQUIRE_SECRET", Value: "false"}}
				a.Spec.ImageUpdater.Image = DefaultImageUpdaterImage + ":v1.3.0"
				a.Spec.ImageUpdater.WebhookServer.Insecure = true
			},
			wantIssues: 0,
		},
		{
			name: "insecure with reencrypt override",
			opts: func(a *argoproj.ArgoCD) {
				a.Spec.ImageUpdater.Env = []corev1.EnvVar{{Name: "WEBHOOK_REQUIRE_SECRET", Value: "false"}}
				a.Spec.ImageUpdater.WebhookServer.Insecure = true
				a.Spec.ImageUpdater.WebhookServer.Route.TLS = &routev1.TLSConfig{Termination: routev1.TLSTerminationReencrypt}
			},
			wantIssues: 1,
		},
		{
			name: "secure with edge override",
			opts: func(a *argoproj.ArgoCD) {
				a.Spec.ImageUpdater.WebhookServer.Route.TLS = &routev1.TLSConfig{Termination: routev1.TLSTerminationEdge}
			},
			wantIssues: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cr := makeImageUpdaterWebhookArgoCD(tt.opts)
			r := makeImageUpdaterWebhookReconciler(t, append([]client.Object{cr}, tt.objs...)...)
			issues, err := r.imageUpdaterWebhookConfigIssues(cr)
			require.NoError(t, err)
			assert.Len(t, issues, tt.wantIssues, "issues: %v", issues)
		})
	}
}

func TestReconcileImageUpdaterWebhookCondition(t *testing.T) {
	cr := makeImageUpdaterWebhookArgoCD()
	r := makeImageUpdaterWebhookReconciler(t, cr)

	require.NoError(t, r.reconcileImageUpdaterWebhookCondition(cr))
	require.Len(t, cr.Status.Conditions, 1)
	assert.Equal(t, argoproj.ArgoCDConditionImageUpdaterWebhookConfigurationError, cr.Status.Conditions[0].Type)
	assert.Equal(t, metav1.ConditionTrue, cr.Status.Conditions[0].Status)

	cr.Spec.ImageUpdater.Env = []corev1.EnvVar{{Name: "WEBHOOK_REQUIRE_SECRET", Value: "false"}}
	require.NoError(t, r.reconcileImageUpdaterWebhookCondition(cr))
	assert.Empty(t, cr.Status.Conditions)
}

func TestImageUpdaterWebhookSecretMapper(t *testing.T) {
	referencing := makeImageUpdaterWebhookArgoCD(func(a *argoproj.ArgoCD) {
		a.Spec.ImageUpdater.WebhookServer.Secrets = &argoproj.ImageUpdaterWebhookSecretsSpec{
			CloudEvents: &argoproj.WebhookSecretKeySelector{Name: "registry-secret", Key: "k"},
		}
	})
	r := makeImageUpdaterWebhookReconciler(t, referencing)

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "registry-secret", Namespace: testNamespace}}
	assert.Equal(t, []reconcile.Request{{NamespacedName: types.NamespacedName{Name: testArgoCDName, Namespace: testNamespace}}},
		r.imageUpdaterWebhookSecretMapper(context.TODO(), secret))

	other := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: testNamespace}}
	assert.Empty(t, r.imageUpdaterWebhookSecretMapper(context.TODO(), other))

	otherNamespace := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "registry-secret", Namespace: "elsewhere"}}
	assert.Empty(t, r.imageUpdaterWebhookSecretMapper(context.TODO(), otherNamespace))
}

func TestImageUpdaterWebhookTLSSecretIsOfInterest(t *testing.T) {
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "argocd-image-updater-webhook-tls"}}
	assert.True(t, isSecretOfInterest(secret))
	assert.True(t, isOwnerOfInterest(metav1.OwnerReference{Kind: "Service", Name: "argocd-image-updater-webhook"}))
}

func TestConditionsEquivalent(t *testing.T) {
	a := []metav1.Condition{{Type: "A", Status: metav1.ConditionTrue, Reason: "R", Message: "m", LastTransitionTime: metav1.Now()}}
	b := []metav1.Condition{{Type: "A", Status: metav1.ConditionTrue, Reason: "R", Message: "m"}}
	assert.True(t, conditionsEquivalent(a, b))
	assert.False(t, conditionsEquivalent(a, nil))
	b[0].Message = "changed"
	assert.False(t, conditionsEquivalent(a, b))
}

func TestUpstreamImageUpdaterVersion(t *testing.T) {
	tests := []struct {
		image  string
		want   string
		wantOK bool
	}{
		{image: DefaultImageUpdaterImage + ":v1.3.0", want: "v1.3.0", wantOK: true},
		{image: DefaultImageUpdaterImage + ":1.2.1", want: "v1.2.1", wantOK: true},
		{image: DefaultImageUpdaterImage + ":latest"},
		{image: DefaultImageUpdaterImage + "@sha256:0123456789abcdef"},
		{image: DefaultImageUpdaterImage + ":v1.3.0@sha256:0123456789abcdef"},
		{image: DefaultImageUpdaterImage},
		{image: "registry.redhat.io/openshift-gitops-1/argocd-image-updater-rhel9:v1.3.0"},
		{image: "registry.example.com:5000/argoprojlabs/argocd-image-updater"},
	}
	for _, tt := range tests {
		t.Run(tt.image, func(t *testing.T) {
			got, ok := upstreamImageUpdaterVersion(tt.image)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestImageUpdaterWebhook_UpgradeWithoutOptIn covers an existing Image Updater installation, configured before the
// operator managed the webhook, being reconciled by this operator version without webhookServer set.
func TestImageUpdaterWebhook_UpgradeWithoutOptIn(t *testing.T) {
	setRouteAPIFoundForTest(t)
	legacyEnv := []corev1.EnvVar{
		{Name: "ENABLE_WEBHOOK", Value: "true"},
		{Name: "DISABLE_TLS", Value: "true"},
		{Name: "WEBHOOK_PORT", Value: "8082"},
		{Name: "TLS_CERT_PATH", Value: "/app/config/tls/tls.crt"},
		{Name: "QUAY_WEBHOOK_SECRET", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: ArgocdImageUpdaterSecret},
			Key:                  "webhook.quay-secret",
		}}},
	}
	cr := makeTestArgoCD(func(a *argoproj.ArgoCD) {
		a.Spec.ImageUpdater.Enabled = true
		a.Spec.ImageUpdater.Env = legacyEnv
	})

	// Objects created by hand under the names the operator now uses, to expose the webhook server.
	name := testArgoCDName + "-image-updater-webhook"
	manualService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Spec: corev1.ServiceSpec{
			Ports:    []corev1.ServicePort{{Name: "http", Port: 8080, TargetPort: intstr.FromInt32(8082)}},
			Selector: map[string]string{"app": "custom"},
		},
	}
	manualRoute := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Spec:       routev1.RouteSpec{To: routev1.RouteTargetReference{Kind: "Service", Name: name}},
	}
	manualIngress := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace}}
	r := makeImageUpdaterWebhookReconciler(t, cr, manualService, manualRoute, manualIngress)
	key := types.NamespacedName{Name: name, Namespace: testNamespace}

	require.NoError(t, r.reconcileImageUpdaterWebhookCondition(cr))
	assert.Empty(t, cr.Status.Conditions, "no warning without opting in")

	t.Run("hand-made Service, Route and Ingress are left untouched", func(t *testing.T) {
		require.NoError(t, r.reconcileImageUpdaterWebhookNetworking(cr))
		svc := &corev1.Service{}
		require.NoError(t, r.Get(context.TODO(), key, svc))
		assert.Equal(t, manualService.Spec.Ports[0].Port, svc.Spec.Ports[0].Port)
		assert.Empty(t, svc.OwnerReferences)
		require.NoError(t, r.Get(context.TODO(), key, &routev1.Route{}))
		require.NoError(t, r.Get(context.TODO(), key, &networkingv1.Ingress{}))
	})

	t.Run("deployment keeps the user's webhook env and is not rolled", func(t *testing.T) {
		sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "sa", Namespace: testNamespace}}
		require.NoError(t, r.reconcileImageUpdaterDeployment(cr, sa, "", nil))
		deploymentKey := types.NamespacedName{Name: testArgoCDName + "-argocd-image-updater-controller", Namespace: testNamespace}
		deployment := &appsv1.Deployment{}
		require.NoError(t, r.Get(context.TODO(), deploymentKey, deployment))

		container := deployment.Spec.Template.Spec.Containers[0]
		assert.ElementsMatch(t, legacyEnv, container.Env, "webhook env must pass through unchanged")
		assert.NotContains(t, deployment.Spec.Template.Labels, imageUpdaterWebhookChecksumLabel)
		for _, v := range deployment.Spec.Template.Spec.Volumes {
			assert.NotEqual(t, imageUpdaterWebhookTLSVolumeName, v.Name)
		}

		// A deployment created by an earlier operator version has no named webhook container port. Declaring the
		// port must not cause an update, which would restart the pod during the operator upgrade.
		deployment.Spec.Template.Spec.Containers[0].Ports = nil
		require.NoError(t, r.Update(context.TODO(), deployment))
		require.NoError(t, r.Get(context.TODO(), deploymentKey, deployment))
		resourceVersion := deployment.ResourceVersion

		require.NoError(t, r.reconcileImageUpdaterDeployment(cr, sa, "", nil))
		require.NoError(t, r.Get(context.TODO(), deploymentKey, deployment))
		assert.Equal(t, resourceVersion, deployment.ResourceVersion)
	})

	t.Run("opting in adopts the hand-made objects", func(t *testing.T) {
		cr.Spec.ImageUpdater.WebhookServer.Insecure = true
		cr.Spec.ImageUpdater.WebhookServer.Route.Enabled = true
		require.NoError(t, r.Create(context.TODO(), &configv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       configv1.IngressSpec{Domain: "apps.example.com"},
		}))
		require.NoError(t, r.reconcileImageUpdaterWebhookNetworking(cr))

		svc := &corev1.Service{}
		require.NoError(t, r.Get(context.TODO(), key, svc))
		assert.True(t, metav1.IsControlledBy(svc, cr))
		assert.Equal(t, int32(8082), svc.Spec.Ports[0].Port)

		route := &routev1.Route{}
		require.NoError(t, r.Get(context.TODO(), key, route))
		assert.True(t, metav1.IsControlledBy(route, cr))
		assert.Equal(t, routev1.TLSTerminationEdge, route.Spec.TLS.Termination)

		// The hand-made Ingress is not part of this configuration, so it is still left alone.
		ingress := &networkingv1.Ingress{}
		require.NoError(t, r.Get(context.TODO(), key, ingress))
		assert.False(t, metav1.IsControlledBy(ingress, cr))

		// Once adopted, disabling the webhook removes the objects.
		cr.Spec.ImageUpdater.WebhookServer.Route.Enabled = false
		require.NoError(t, r.reconcileImageUpdaterWebhookNetworking(cr))
		assert.True(t, errors.IsNotFound(r.Get(context.TODO(), key, &corev1.Service{})))
		assert.True(t, errors.IsNotFound(r.Get(context.TODO(), key, &routev1.Route{})))
	})
}

func TestImageUpdaterWebhook_ObjectControlledByAnotherOwner(t *testing.T) {
	setRouteAPIFoundForTest(t)
	cr := makeImageUpdaterWebhookArgoCD(func(a *argoproj.ArgoCD) {
		a.UID = "argocd-uid"
		a.Spec.ImageUpdater.Env = []corev1.EnvVar{{Name: "WEBHOOK_REQUIRE_SECRET", Value: "false"}}
		a.Spec.ImageUpdater.WebhookServer.Host = "webhook.example.org"
	})
	name := testArgoCDName + "-image-updater-webhook"
	foreign := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "someone-else",
				UID:        "other-uid",
				Controller: new(true),
			}},
		},
		Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Name: "http", Port: 9000}}},
	}
	r := makeImageUpdaterWebhookReconciler(t, cr, foreign)
	key := types.NamespacedName{Name: name, Namespace: testNamespace}

	// The conflict is reported, not returned as an error.
	issues, err := r.imageUpdaterWebhookConfigIssues(cr)
	require.NoError(t, err)
	require.Len(t, issues, 1)
	assert.Contains(t, issues[0], `Service "`+name+`" is controlled by Deployment "someone-else"`)

	// The Service is left untouched, and the Route is still reconciled.
	require.NoError(t, r.reconcileImageUpdaterWebhookNetworking(cr))
	svc := &corev1.Service{}
	require.NoError(t, r.Get(context.TODO(), key, svc))
	assert.Equal(t, int32(9000), svc.Spec.Ports[0].Port)
	assert.False(t, metav1.IsControlledBy(svc, cr))
	require.NoError(t, r.Get(context.TODO(), key, &routev1.Route{}))

	// Disabling the webhook does not delete it either.
	cr.Spec.ImageUpdater.WebhookServer.Route.Enabled = false
	require.NoError(t, r.reconcileImageUpdaterWebhookNetworking(cr))
	require.NoError(t, r.Get(context.TODO(), key, svc))
}

func TestReconcileImageUpdaterController_RouteErrorDoesNotBlockDeployment(t *testing.T) {
	setRouteAPIFoundForTest(t)
	cr := makeImageUpdaterWebhookArgoCD(func(a *argoproj.ArgoCD) {
		a.Spec.ImageUpdater.Env = []corev1.EnvVar{{Name: "WEBHOOK_REQUIRE_SECRET", Value: "false"}}
		a.Spec.ImageUpdater.WebhookServer.Host = "webhook.example.org"
		// The referenced certificate Secret does not exist, so the Route cannot be reconciled.
		a.Spec.ImageUpdater.WebhookServer.Route.TLS = &routev1.TLSConfig{
			Termination:         routev1.TLSTerminationReencrypt,
			ExternalCertificate: &routev1.LocalObjectReference{Name: "missing-cert"},
		}
	})
	r := makeImageUpdaterWebhookReconciler(t, cr)

	err := r.reconcileImageUpdaterController(cr)
	require.Error(t, err)

	// The Service and the deployment, which come before the Route, are still reconciled.
	require.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: testArgoCDName + "-image-updater-webhook", Namespace: testNamespace}, &corev1.Service{}))
	deployment := &appsv1.Deployment{}
	require.NoError(t, r.Get(context.TODO(), types.NamespacedName{Name: testArgoCDName + "-argocd-image-updater-controller", Namespace: testNamespace}, deployment))
	assert.Equal(t, "true", argoutil.EnvGet(deployment.Spec.Template.Spec.Containers[0].Env, "ENABLE_WEBHOOK").Value)
}

func TestImageUpdaterWebhook_ArgoCDTrackedObjectsAreNotAdopted(t *testing.T) {
	name := testArgoCDName + "-image-updater-webhook"
	tests := []struct {
		name        string
		meta        metav1.ObjectMeta
		labelKey    string
		wantMessage string
	}{
		{
			name:        "tracking annotation",
			meta:        metav1.ObjectMeta{Annotations: map[string]string{"argocd.argoproj.io/tracking-id": "app:/Service:ns/" + name}},
			wantMessage: "annotation argocd.argoproj.io/tracking-id",
		},
		{
			name:        "default tracking label",
			meta:        metav1.ObjectMeta{Labels: map[string]string{"app.kubernetes.io/instance": "my-app"}},
			wantMessage: "label app.kubernetes.io/instance",
		},
		{
			name:        "custom tracking label of this instance",
			meta:        metav1.ObjectMeta{Labels: map[string]string{"example.com/app": "my-app"}},
			labelKey:    "example.com/app",
			wantMessage: "label example.com/app",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setRouteAPIFoundForTest(t)
			cr := makeImageUpdaterWebhookArgoCD(func(a *argoproj.ArgoCD) {
				a.Spec.ApplicationInstanceLabelKey = tt.labelKey
				a.Spec.ImageUpdater.Env = []corev1.EnvVar{{Name: "WEBHOOK_REQUIRE_SECRET", Value: "false"}}
				a.Spec.ImageUpdater.WebhookServer.Host = "webhook.example.org"
			})
			tracked := &corev1.Service{
				ObjectMeta: tt.meta,
				Spec:       corev1.ServiceSpec{Ports: []corev1.ServicePort{{Name: "http", Port: 9000}}},
			}
			tracked.Name = name
			tracked.Namespace = testNamespace
			r := makeImageUpdaterWebhookReconciler(t, cr, tracked)
			key := types.NamespacedName{Name: name, Namespace: testNamespace}

			issues, err := r.imageUpdaterWebhookConfigIssues(cr)
			require.NoError(t, err)
			require.Len(t, issues, 1)
			assert.Contains(t, issues[0], `Service "`+name+`" has the Argo CD tracking `+tt.wantMessage)

			require.NoError(t, r.reconcileImageUpdaterWebhookNetworking(cr))
			svc := &corev1.Service{}
			require.NoError(t, r.Get(context.TODO(), key, svc))
			assert.Equal(t, int32(9000), svc.Spec.Ports[0].Port)
			assert.Empty(t, svc.OwnerReferences)

			cr.Spec.ImageUpdater.WebhookServer.Route.Enabled = false
			require.NoError(t, r.reconcileImageUpdaterWebhookNetworking(cr))
			require.NoError(t, r.Get(context.TODO(), key, svc))
		})
	}
}
