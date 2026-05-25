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

package util

import (
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGetClusterVersion(t *testing.T) {
	s := scheme.Scheme
	addKnownTypesToScheme(s)

	t.Run("Valid Cluster Version", func(t *testing.T) {
		version := "4.7.1"
		fakeClient := fake.NewFakeClient(NewClusterVersion(version))
		clusterVersion, err := GetClusterVersion(fakeClient)
		assertNoError(t, err)
		if clusterVersion != version {
			t.Fatalf("got %s, want %s", clusterVersion, version)
		}
	})
	t.Run("Cluster Version not found", func(t *testing.T) {
		fakeClient := fake.NewFakeClient()
		clusterVersion, err := GetClusterVersion(fakeClient)
		assertNoError(t, err)
		if clusterVersion != "" {
			t.Fatalf("got %s, want %s", clusterVersion, "")
		}
	})
}

func addKnownTypesToScheme(scheme *runtime.Scheme) {
	scheme.AddKnownTypes(configv1.GroupVersion, &configv1.ClusterVersion{})
}

func TestRouteAPIFound(t *testing.T) {
	t.Run("Route API not found by default", func(t *testing.T) {
		defer SetRouteAPIFound(false)
		SetRouteAPIFound(false)
		if IsRouteAPIFound() {
			t.Fatal("expected Route API to not be found")
		}
	})
	t.Run("SetRouteAPIFound sets the value", func(t *testing.T) {
		defer SetRouteAPIFound(false)
		SetRouteAPIFound(true)
		if !IsRouteAPIFound() {
			t.Fatal("expected Route API to be found after SetRouteAPIFound(true)")
		}
	})
}

func TestConsoleAPIFound(t *testing.T) {
	t.Run("Console API not found by default", func(t *testing.T) {
		defer SetConsoleAPIFound(false)
		SetConsoleAPIFound(false)
		if IsConsoleAPIFound() {
			t.Fatal("expected Console API to not be found")
		}
	})
	t.Run("SetConsoleAPIFound sets the value", func(t *testing.T) {
		defer SetConsoleAPIFound(false)
		SetConsoleAPIFound(true)
		if !IsConsoleAPIFound() {
			t.Fatal("expected Console API to be found after SetConsoleAPIFound(true)")
		}
	})
}

func assertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
