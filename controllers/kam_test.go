package controllers

import (
	"context"
	"os"
	"testing"

	"github.com/argoproj-labs/argocd-operator/controllers/argocd"
	"github.com/redhat-developer/gitops-operator/controllers/util"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	disableKam = "DISABLE_KAM"
)

func TestReconcile_verify_kam(t *testing.T) {

	defer util.SetConsoleAPIFound(util.IsConsoleAPIFound())
	util.SetConsoleAPIFound(true)

	logf.SetLogger(argocd.ZapLogger(true))
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	var err error
	fakeClient := fake.NewFakeClient(newGitopsService())
	reconciler := newReconcileGitOpsService(fakeClient, s)
	reconciler.DisableDefaultInstall = true
	_, err = reconciler.Reconcile(context.TODO(), newRequest("test", "test"))
	assertNoError(t, err)

	// Kam deployment SHOULD be created (in openshift-gitops namespace)

	deploy := &appsv1.Deployment{}
	if err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: "kam", Namespace: serviceNamespace},
		deploy); err != nil {

		t.Fatalf("Kam deployment should exist in namespace, error: %v", err)
	}

}
func TestReconcile_delete_kam1(t *testing.T) {

	defer util.SetConsoleAPIFound(util.IsConsoleAPIFound())
	util.SetConsoleAPIFound(true)

	logf.SetLogger(argocd.ZapLogger(true))
	s := scheme.Scheme
	addKnownTypesToScheme(s)
	os.Setenv(disableKam, "true")
	var err error
	fakeClient := fake.NewFakeClient(newGitopsService())
	reconciler := newReconcileGitOpsService(fakeClient, s)
	reconciler.DisableDefaultInstall = true
	_, err = reconciler.Reconcile(context.TODO(), newRequest("test", "test"))
	assertNoError(t, err)

	// Kam deployment SHOULD be deleted from namespace (in openshift-gitops namespace)
	deploy := &appsv1.Deployment{}
	if err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: "kam", Namespace: serviceNamespace},
		deploy); err == nil || !errors.IsNotFound(err) {

		t.Fatalf("Kam Deployment should not exist in namespace, error: %v", err)
	}
	os.Unsetenv(disableKam)

}
