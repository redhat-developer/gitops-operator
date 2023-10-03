/*
Copyright 2021.

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

package controllers

import (
	"context"
	"net/url"
	"testing"

	"github.com/argoproj-labs/argocd-operator/controllers/argocd"
	"github.com/google/go-cmp/cmp"
	configv1 "github.com/openshift/api/config/v1"
	console "github.com/openshift/api/console/v1"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/redhat-developer/gitops-operator/controllers/util"
	"gotest.tools/assert"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	argocdInstanceName       = "openshift-gitops"
	disableArgoCDConsoleLink = "DISABLE_DEFAULT_ARGOCD_CONSOLELINK"
)

var (
	argoCDRoute = &routev1.Route{
		ObjectMeta: v1.ObjectMeta{
			Name:      argocdRouteName,
			Namespace: argocdNS,
		},
		Spec: routev1.RouteSpec{
			Host: "test.com",
		},
	}

	consoleLink = &console.ConsoleLink{
		ObjectMeta: v1.ObjectMeta{
			Name:      consoleLinkName,
			Namespace: argocdNS,
		},
	}
)

func TestReconcile_create_consolelink(t *testing.T) {
	defer util.SetConsoleAPIFound(util.IsConsoleAPIFound())
	util.SetConsoleAPIFound(true)

	reconcileArgoCD, fakeClient := newFakeReconcileArgoCD(argoCDRoute)
	want := newConsoleLink("https://test.com", "Cluster Argo CD")

	result, err := reconcileArgoCD.Reconcile(context.TODO(), newRequest(argocdNS, argocdInstanceName))
	assertConsoleLinkExists(t, fakeClient, reconcileResult{result, err}, want)
}

func TestReconcile_delete_consolelink(t *testing.T) {
	logf.SetLogger(argocd.ZapLogger(true))

	defer util.SetConsoleAPIFound(util.IsConsoleAPIFound())
	util.SetConsoleAPIFound(true)

	tests := []struct {
		name                   string
		setEnvVarFunc          func(*testing.T, string)
		envVar                 string
		consoleLinkShouldExist bool
		wantErr                bool
		Err                    error
	}{
		{
			name: "DISABLE_DEFAULT_ARGOCD_CONSOLELINK is set to true and consoleLink gets deleted",
			setEnvVarFunc: func(t *testing.T, envVar string) {
				t.Setenv(disableArgoCDConsoleLink, envVar)
			},
			consoleLinkShouldExist: false,
			envVar:                 "true",
			wantErr:                false,
		},
		{
			name: "DISABLE_DEFAULT_ARGOCD_CONSOLELINK is set to false and consoleLink doesn't get deleted",
			setEnvVarFunc: func(t *testing.T, envVar string) {
				t.Setenv(disableArgoCDConsoleLink, envVar)
			},
			envVar:                 "false",
			consoleLinkShouldExist: true,
			wantErr:                false,
		},
		{
			name:                   "DISABLE_DEFAULT_ARGOCD_CONSOLELINK isn't set and consoleLink doesn't get deleted",
			setEnvVarFunc:          nil,
			envVar:                 "",
			consoleLinkShouldExist: true,
			wantErr:                false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reconcileArgoCD, fakeClient := newFakeReconcileArgoCD(argoCDRoute, consoleLink)
			consoleLink := newConsoleLink("https://test.com", "Cluster Argo CD")
			fakeClient.Create(context.TODO(), consoleLink)

			if test.setEnvVarFunc != nil {
				test.setEnvVarFunc(t, test.envVar)
			}

			result, err := reconcileArgoCD.Reconcile(context.TODO(), newRequest(argocdNS, argocdInstanceName))
			if !test.consoleLinkShouldExist {
				assertConsoleLinkDeletion(t, fakeClient, reconcileResult{result, err})
			} else {
				assertConsoleLinkExists(t, fakeClient, reconcileResult{result, err}, consoleLink)
			}
			if err != nil {
				if !test.wantErr {
					t.Errorf("Got unexpected error")
				} else {
					assert.Equal(t, test.Err, err)
				}
			} else {
				if test.wantErr {
					t.Errorf("expected error but didn't get one")
				}
			}
		})
	}

}

func TestReconcile_update_consolelink(t *testing.T) {
	defer util.SetConsoleAPIFound(util.IsConsoleAPIFound())
	util.SetConsoleAPIFound(true)

	reconcileArgoCD, fakeClient := newFakeReconcileArgoCD(argoCDRoute, consoleLink)

	argoCDRoute.Spec.Host = "updated-test.com"
	err := fakeClient.Update(context.TODO(), argoCDRoute)
	assertNoError(t, err)

	_, err = reconcileArgoCD.Reconcile(context.TODO(), newRequest(argocdNS, argocdRouteName))
	assertNoError(t, err)

	cl, err := getConsoleLink(fakeClient)
	assertNoError(t, err)
	url, err := url.Parse(cl.Spec.Href)
	assertNoError(t, err)
	if diff := cmp.Diff(argoCDRoute.Spec.Host, url.Hostname()); diff != "" {
		t.Fatalf("ConsoleLink URL mismatch: %v", diff)
	}
}

func TestReconcile_consolelink_no_consoleapi(t *testing.T) {
	defer util.SetConsoleAPIFound(util.IsConsoleAPIFound())
	util.SetConsoleAPIFound(false)

	reconcileArgoCD, fakeClient := newFakeReconcileArgoCD(argoCDRoute)

	result, err := reconcileArgoCD.Reconcile(context.TODO(), newRequest(argocdNS, argocdInstanceName))
	assertConsoleLinkDeletion(t, fakeClient, reconcileResult{result, err})
}

func newFakeReconcileArgoCD(objs ...runtime.Object) (*ReconcileArgoCDRoute, client.Client) {
	s := scheme.Scheme
	s.AddKnownTypes(routev1.GroupVersion, &routev1.Route{})
	s.AddKnownTypes(console.GroupVersion, &console.ConsoleLink{})
	s.AddKnownTypes(configv1.GroupVersion, &configv1.ClusterVersion{})
	fakeClient := fake.NewFakeClient(objs...)
	return &ReconcileArgoCDRoute{
		Client: fakeClient,
		Scheme: s,
	}, fakeClient
}

func getConsoleLink(c client.Client) (*console.ConsoleLink, error) {
	cl := &console.ConsoleLink{}
	err := c.Get(context.TODO(), types.NamespacedName{Name: consoleLinkName}, cl)
	if err != nil {
		return nil, err
	}
	return cl, nil
}

func assertConsoleLinkExists(t *testing.T, c client.Client, r reconcileResult, want *console.ConsoleLink) {
	t.Helper()
	assertNoError(t, r.err)

	if r.result.Requeue {
		t.Fatalf("Expected ConsoleLink to be deleted without requeuing")
	}

	got, err := getConsoleLink(c)
	assertNoError(t, err)
	if diff := cmp.Diff(want.Spec, got.Spec); diff != "" {
		t.Fatalf("ConsoleLink mismatch: %v", diff)
	}
}

func assertConsoleLinkDeletion(t *testing.T, c client.Client, r reconcileResult) {
	t.Helper()
	assertNoError(t, r.err)

	if r.result.Requeue {
		t.Fatalf("Expected ConsoleLink to be created without requeuing")
	}

	_, err := getConsoleLink(c)

	wantErr := `consolelinks.console.openshift.io "argocd" not found`
	if err == nil {
		t.Fatalf("was expecting an error %s, but got nil", wantErr)
	}

	if err.Error() != wantErr {
		t.Fatalf("got %s, want %s", err, wantErr)
	}
}

type reconcileResult struct {
	result reconcile.Result
	err    error
}
