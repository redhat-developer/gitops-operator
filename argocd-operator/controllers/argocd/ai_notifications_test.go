package argocd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/redhat-developer/gitops-operator/argocd-operator/api/v1alpha1"
	argoproj "github.com/redhat-developer/gitops-operator/argocd-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func aiNotificationsFixture(t *testing.T, tls bool) (*ReconcileArgoCD, *argoproj.ArgoCD) {
	t.Helper()
	t.Setenv(enableAIFeaturesEnv, "true")
	t.Setenv(aiTriggerTLSEnv, fmt.Sprint(tls))
	t.Setenv("ARGOCD_OPERATOR_NAMESPACE", "openshift-gitops")
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, v1alpha1.AddToScheme, argoproj.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	cr := &argoproj.ArgoCD{
		ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops", Namespace: "openshift-gitops"},
		Spec:       argoproj.ArgoCDSpec{AI: &argoproj.ArgoCDAISpec{Troubleshooting: true}},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cr).Build()
	return &ReconcileArgoCD{Client: cl, Scheme: scheme}, cr
}

func TestAINotificationsWaitsPropagatesRotatesAndCleansUp(t *testing.T) {
	for _, tls := range []bool{false, true} {
		t.Run(fmt.Sprintf("TLS=%v", tls), func(t *testing.T) {
			r, cr := aiNotificationsFixture(t, tls)
			ctx := context.Background()
			aiNCKey := client.ObjectKey{Name: aiNotificationsConfigurationName, Namespace: cr.Namespace}

			pending := func() {
				t.Helper()
				if err := r.reconcileAINotifications(cr); !errors.Is(err, errAINotificationsPending) {
					t.Fatalf("expected pending, got %v", err)
				}
			}
			create := func(obj client.Object) {
				t.Helper()
				if err := r.Create(ctx, obj); err != nil {
					t.Fatal(err)
				}
			}
			get := func(obj client.Object) {
				t.Helper()
				if err := r.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
					t.Fatal(err)
				}
			}
			meta := func(name string) metav1.ObjectMeta { return metav1.ObjectMeta{Name: name, Namespace: cr.Namespace} }

			// First pending: creates NC CR, SA, token secret (and CA ConfigMap if TLS).
			// Returns pending because token data is not yet populated.
			pending()

			// The AI NC CR should have been created with AI-specific entries.
			aiNC := &v1alpha1.NotificationsConfiguration{}
			if err := r.Get(ctx, aiNCKey, aiNC); err != nil {
				t.Fatalf("AI NC CR not created: %v", err)
			}
			if aiNC.Spec.Triggers[aiNotifTriggerKey] == "" || aiNC.Spec.Templates[aiNotifTemplateKey] == "" || aiNC.Spec.Services[aiNotifServiceKey] == "" {
				t.Fatalf("AI NC CR missing entries: %+v", aiNC.Spec)
			}

			// Token secret and SA were created by the reconciler; simulate Kubernetes SA token controller.
			token := &corev1.Secret{ObjectMeta: meta(aiTriggerTokenSecretName)}
			get(token)
			token.Data = map[string][]byte{"token": []byte("first-token")}
			if err := r.Update(ctx, token); err != nil {
				t.Fatal(err)
			}
			pending() // destination secret absent

			target := &corev1.Secret{ObjectMeta: meta(aiNotificationsSecretName), Data: map[string][]byte{"user-token": []byte("keep")}}
			create(target)
			ca := &corev1.ConfigMap{ObjectMeta: meta(aiCAConfigMapName)}
			certs := &corev1.ConfigMap{ObjectMeta: meta(aiTLSCertsConfigMapName), Data: map[string]string{"user-host": "keep"}}
			if tls {
				// CA ConfigMap was created by the reconciler; simulate OpenShift CA injection.
				get(ca)
				pending() // source CA not populated
				ca.Data = map[string]string{aiCABundleKey: "first-ca"}
				if err := r.Update(ctx, ca); err != nil {
					t.Fatal(err)
				}
				pending() // destination certs CM absent
				create(certs)
			}

			for _, generation := range []string{"first", "rotated"} {
				get(token)
				token.Data["token"] = []byte(generation + "-token")
				if err := r.Update(ctx, token); err != nil {
					t.Fatal(err)
				}
				if tls {
					get(ca)
					ca.Data[aiCABundleKey] = generation + "-ca"
					if err := r.Update(ctx, ca); err != nil {
						t.Fatal(err)
					}
				}
				if err := r.reconcileAINotifications(cr); err != nil {
					t.Fatal(err)
				}
				get(target)
				if string(target.Data[aiTriggerTokenKey]) != generation+"-token" || string(target.Data["user-token"]) != "keep" {
					t.Fatalf("incorrect token propagation: %v", target.Data)
				}
				if tls {
					get(certs)
					if certs.Data["gitops-ai-controller."+cr.Namespace+".svc"] != generation+"-ca" || certs.Data["user-host"] != "keep" {
						t.Fatalf("incorrect CA propagation: %v", certs.Data)
					}
				}
			}

			// Verify AI NC CR has the correct service endpoint.
			get(aiNC)
			endpoint := "http://gitops-ai-controller." + cr.Namespace + ".svc:8080"
			if tls {
				endpoint = "https://gitops-ai-controller." + cr.Namespace + ".svc:8443"
			}
			if !strings.Contains(aiNC.Spec.Services[aiNotifServiceKey], endpoint) {
				t.Fatalf("incorrect AI NC CR service endpoint: %+v", aiNC.Spec.Services)
			}
			if !strings.Contains(aiNC.Spec.Subscriptions["subscriptions"], "gitops-ai-controller") {
				t.Fatalf("AI NC CR missing subscription: %+v", aiNC.Spec.Subscriptions)
			}

			// Idempotency: no patch when nothing changed.
			aiNCVersion := aiNC.ResourceVersion
			targetVersion := target.ResourceVersion
			if err := r.reconcileAINotifications(cr); err != nil {
				t.Fatal(err)
			}
			get(aiNC)
			get(target)
			if aiNC.ResourceVersion != aiNCVersion || target.ResourceVersion != targetVersion {
				t.Fatal("unchanged configuration was patched")
			}

			// Disable AI.
			cr.Spec.AI.Troubleshooting = false
			if err := r.reconcileAINotifications(cr); err != nil {
				t.Fatal(err)
			}

			// AI NC CR must be deleted.
			if err := r.Get(ctx, aiNCKey, &v1alpha1.NotificationsConfiguration{}); !apierrors.IsNotFound(err) {
				t.Fatalf("AI NC CR retained after disable: %v", err)
			}

			// Token secret must be deleted.
			if err := r.Get(ctx, client.ObjectKey{Name: aiTriggerTokenSecretName, Namespace: cr.Namespace}, &corev1.Secret{}); !apierrors.IsNotFound(err) {
				t.Fatalf("token secret retained after disable: %v", err)
			}

			// Notifications SA must be deleted.
			if err := r.Get(ctx, client.ObjectKey{Name: aiNotificationsSAName, Namespace: cr.Namespace}, &corev1.ServiceAccount{}); !apierrors.IsNotFound(err) {
				t.Fatalf("notifications SA retained after disable: %v", err)
			}

			// Token removed from notifications secret; user token preserved.
			get(target)
			if _, exists := target.Data[aiTriggerTokenKey]; exists {
				t.Fatal("integration token retained")
			}
			if string(target.Data["user-token"]) != "keep" {
				t.Fatal("user token removed")
			}

			if tls {
				get(certs)
				if len(certs.Data) != 1 || certs.Data["user-host"] != "keep" {
					t.Fatal("incorrect CA cleanup")
				}
				// CA ConfigMap must be deleted.
				if err := r.Get(ctx, client.ObjectKey{Name: aiCAConfigMapName, Namespace: cr.Namespace}, &corev1.ConfigMap{}); !apierrors.IsNotFound(err) {
					t.Fatalf("CA ConfigMap retained after disable: %v", err)
				}
			}
		})
	}
}

func TestAINotificationDependencyRequests(t *testing.T) {
	r, cr := aiNotificationsFixture(t, true)
	for _, tc := range []struct {
		name string
		obj  client.Object
		want int
	}{
		{"token rotation", &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: aiTriggerTokenSecretName, Namespace: cr.Namespace}}, 1},
		{"CA rotation", &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: aiCAConfigMapName, Namespace: cr.Namespace}}, 1},
		{"unrelated secret", &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: cr.Namespace}}, 0},
		{"other namespace", &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: aiTriggerTokenSecretName, Namespace: "other"}}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := r.aiNotificationDependencyRequests(context.Background(), tc.obj)
			if len(requests) != tc.want {
				t.Fatalf("got %v, want %d requests", requests, tc.want)
			}
			if len(requests) > 0 && requests[0].NamespacedName != client.ObjectKeyFromObject(cr) {
				t.Fatalf("wrong ArgoCD request: %v", requests)
			}
		})
	}
}

func TestAINotificationsCleanupWithoutConfiguration(t *testing.T) {
	r, cr := aiNotificationsFixture(t, true)
	t.Setenv(enableAIFeaturesEnv, "false")
	ctx := context.Background()
	target := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: aiNotificationsSecretName, Namespace: cr.Namespace}, Data: map[string][]byte{aiTriggerTokenKey: []byte("remove"), "user-token": []byte("keep")}}
	certs := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: aiTLSCertsConfigMapName, Namespace: cr.Namespace}, Data: map[string]string{"gitops-ai-controller." + cr.Namespace + ".svc": "remove", "user-host": "keep"}}
	for _, obj := range []client.Object{target, certs} {
		if err := r.Create(ctx, obj); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.reconcileAINotifications(cr); err != nil {
		t.Fatal(err)
	}
	for _, obj := range []client.Object{target, certs} {
		if err := r.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
			t.Fatal(err)
		}
	}
	if len(target.Data) != 1 || string(target.Data["user-token"]) != "keep" || len(certs.Data) != 1 || certs.Data["user-host"] != "keep" {
		t.Fatal("cleanup skipped credentials or removed user data")
	}
}
