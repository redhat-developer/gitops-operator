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
	routev1 "github.com/openshift/api/route/v1"
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

func TestInspectCluster_RouteDetectedWithoutConfigAPI(t *testing.T) {
	// Save and restore package-level state so this test is hermetic.
	origRoute := routeAPIFound
	origConfig := configAPIFound
	origOLM := olmAPIFound
	origMonitoring := monitoringAPIFound
	t.Cleanup(func() {
		routeAPIFound = origRoute
		configAPIFound = origConfig
		olmAPIFound = origOLM
		monitoringAPIFound = origMonitoring
		SetVerifyAPI(nil) // restore default
	})

	// Reset all flags before the test.
	routeAPIFound = false
	configAPIFound = false
	olmAPIFound = false
	monitoringAPIFound = false

	// Mock API verification: route.openshift.io is present, config.openshift.io is not.
	SetVerifyAPI(func(group, version string) (bool, error) {
		if group == routev1.GroupName {
			return true, nil
		}
		if group == configv1.GroupName {
			return false, nil
		}
		// All other API groups are absent.
		return false, nil
	})

	err := InspectCluster()
	assertNoError(t, err)

	// The bug: before the fix, verifyRouteAPI was gated behind configAPIFound,
	// so on xKS clusters with Route but without Config, Route was never detected.
	if !IsRouteAPIFound() {
		t.Fatal("IsRouteAPIFound() = false; want true when route.openshift.io is available without config.openshift.io")
	}
	if IsConfigAPIFound() {
		t.Fatal("IsConfigAPIFound() = true; want false because config.openshift.io is absent")
	}
}

func addKnownTypesToScheme(scheme *runtime.Scheme) {
	scheme.AddKnownTypes(configv1.GroupVersion, &configv1.ClusterVersion{})
}

func assertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
