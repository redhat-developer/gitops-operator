package controller

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	aiconfig "github.com/redhat-developer/gitops-operator/argocd-operator/pkg/ai"
	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestAuthorizeCallerRequiresOperatorEnrollment(t *testing.T) {
	for _, tc := range []struct {
		name            string
		mutate          func(*corev1.ConfigMap)
		selector        string
		tokenUID        string
		missingTokenUID bool
		missingRegistry bool
		allowed         bool
	}{
		{name: "enrolled identity", tokenUID: testNotificationsUID("team-a"), allowed: true},
		{name: "no registry", missingRegistry: true},
		{name: "unprovisioned instance", mutate: func(cm *corev1.ConfigMap) { delete(cm.Data, "team-a") }},
		{name: "unowned registry", mutate: func(cm *corev1.ConfigMap) { cm.Labels = nil }},
		{name: "malformed enrollment", mutate: func(cm *corev1.ConfigMap) { cm.Data["team-a"] = "not-json" }},
		{name: "replaced instance", mutate: func(cm *corev1.ConfigMap) {
			cm.Data["team-a"] = `{"name":"argocd","uid":"previous-instance","serviceAccountUID":"notifications-team-a"}`
		}},
		{name: "wrong instance name", mutate: func(cm *corev1.ConfigMap) {
			cm.Data["team-a"] = `{"name":"other","uid":"argocd-team-a-argocd","serviceAccountUID":"notifications-team-a"}`
		}},
		{name: "replaced notification SA", tokenUID: "replacement-sa"},
		{name: "token review missing UID", missingTokenUID: true},
		{name: "operator excludes instance", selector: "team=trusted", tokenUID: testNotificationsUID("team-a")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			cr := newArgoCDInstanceObject("argocd", "team-a", true)
			s := authFixture(saUsername("team-a", notificationsSAName), cr)
			s.config.ArgoCDLabelSelector = tc.selector
			kube := s.kubeClient.(*kubefake.Clientset)
			tokenUID := tc.tokenUID
			if tokenUID == "" && !tc.missingTokenUID {
				tokenUID = testNotificationsUID("team-a")
			}
			kube.PrependReactor("create", "tokenreviews", func(ktesting.Action) (bool, runtime.Object, error) {
				return true, &authv1.TokenReview{Status: authv1.TokenReviewStatus{Authenticated: true,
					User: authv1.UserInfo{Username: saUsername("team-a", notificationsSAName), UID: tokenUID}}}, nil
			})
			if tc.missingRegistry {
				if err := kube.CoreV1().ConfigMaps(testOperatorNamespace).Delete(ctx, aiconfig.EnrollmentConfigMapName, metav1.DeleteOptions{}); err != nil {
					t.Fatal(err)
				}
				// A tenant-controlled copy in the caller namespace is not enrollment.
				forged := testEnrollmentRegistry(cr)
				forged.Namespace = "team-a"
				if _, err := kube.CoreV1().ConfigMaps("team-a").Create(ctx, forged, metav1.CreateOptions{}); err != nil {
					t.Fatal(err)
				}
			} else if tc.mutate != nil {
				cm, err := kube.CoreV1().ConfigMaps(testOperatorNamespace).Get(ctx, aiconfig.EnrollmentConfigMapName, metav1.GetOptions{})
				if err != nil {
					t.Fatal(err)
				}
				tc.mutate(cm)
				if _, err := kube.CoreV1().ConfigMaps(testOperatorNamespace).Update(ctx, cm, metav1.UpdateOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			result, err := s.authorizeCaller(ctx, "token", logr.Discard())
			if err != nil {
				t.Fatal(err)
			}
			if result.Authorized != tc.allowed {
				t.Fatalf("authorized=%v want %v: %+v", result.Authorized, tc.allowed, result)
			}
		})
	}
}

func TestAuthorizeCallerIgnoresInstancesOutsideOperatorScope(t *testing.T) {
	selected := newArgoCDInstanceObject("selected", "team-a", true)
	selected.SetLabels(map[string]string{"team": "trusted"})
	ignored := newArgoCDInstanceObject("ignored", "team-a", true)
	s := authFixture(saUsername("team-a", notificationsSAName), ignored, selected)
	s.config.ArgoCDLabelSelector = "team=trusted"
	result, err := s.authorizeCaller(context.Background(), "token", logr.Discard())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Authorized || result.Instance.Name != selected.GetName() {
		t.Fatalf("selected, enrolled instance not authorized: %+v", result)
	}
}

func TestAuthorizeCallerRetriesRegistryAPIFailure(t *testing.T) {
	s := authFixture(saUsername("team-a", notificationsSAName), newArgoCDInstanceObject("argocd", "team-a", true))
	s.kubeClient.(*kubefake.Clientset).PrependReactor("get", "configmaps", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewServiceUnavailable("temporary API failure")
	})
	result, err := s.authorizeCaller(context.Background(), "token", logr.Discard())
	if err == nil || result != nil {
		t.Fatalf("got result=%+v err=%v, expected API error", result, err)
	}
}

func TestLoadServiceConfigUsesOperatorScope(t *testing.T) {
	t.Setenv("GITOPS_AI_OPERATOR_NAMESPACE", "custom-operator")
	t.Setenv("ARGOCD_LABEL_SELECTOR", "team=trusted")
	t.Setenv("ARGOCD_NAMESPACE", "obsolete-instance-value")
	cfg, err := LoadServiceConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OperatorNamespace != "custom-operator" || cfg.ArgoCDLabelSelector != "team=trusted" {
		t.Fatalf("wrong operator scope: %+v", cfg)
	}
	t.Setenv("ARGOCD_LABEL_SELECTOR", "invalid in (")
	if _, err := LoadServiceConfig(); err == nil {
		t.Fatal("invalid selector accepted")
	}
}
