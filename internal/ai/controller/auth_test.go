package controller

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	authv1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestAuthenticateAIControllerIdentity(t *testing.T) {
	for _, tc := range []struct {
		username string
		allowed  bool
	}{
		{"system:serviceaccount:openshift-gitops:gitops-ai-controller-notifications", true},
		{"system:serviceaccount:other:gitops-ai-controller-notifications", false},
		{"system:serviceaccount:openshift-gitops:ai-controller-notifications", false},
	} {
		t.Run(tc.username, func(t *testing.T) {
			cl := kubefake.NewSimpleClientset()
			cl.PrependReactor("create", "tokenreviews", func(ktesting.Action) (bool, runtime.Object, error) {
				return true, &authv1.TokenReview{Status: authv1.TokenReviewStatus{Authenticated: true, User: authv1.UserInfo{Username: tc.username}}}, nil
			})
			result := authenticateCaller(context.Background(), cl, "test-token", "openshift-gitops", logr.Discard())
			if result == nil || !result.Authenticated || result.Authorized != tc.allowed {
				t.Fatalf("unexpected authentication result: %+v", result)
			}
		})
	}
}
