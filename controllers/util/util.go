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
	"context"
	stderrors "errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/argoproj-labs/gitops-operator/argocd-operator/controllers/argoutil"
	oappsv1 "github.com/openshift/api/apps/v1"
	configv1 "github.com/openshift/api/config/v1"
	console "github.com/openshift/api/console/v1"
	oauthv1 "github.com/openshift/api/oauth/v1"
	routev1 "github.com/openshift/api/route/v1"
	templatev1 "github.com/openshift/api/template/v1"
	operatorsv1 "github.com/operator-framework/api/pkg/operators/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"golang.org/x/mod/semver"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	clusterVersionName       = "version"
	operatorPodNamespacePath = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
	DefaultOperatorNamespace = "openshift-gitops-operator"
)

var (
	consoleAPIFound    = false
	routeAPIFound      = false
	monitoringAPIFound = false
	configAPIFound     = false
	templateAPIFound   = false
	appsAPIFound       = false
	oauthAPIFound      = false
	olmAPIFound        = false

	// verifyAPI is the function used to check API group availability.
	// It defaults to argoutil.VerifyAPI and can be overridden in tests.
	verifyAPI = argoutil.VerifyAPI
)

// GetClusterVersion returns the OpenShift Cluster version in which the operator is installed
func GetClusterVersion(client client.Client) (string, error) {
	clusterVersion := &configv1.ClusterVersion{}
	err := client.Get(context.TODO(), types.NamespacedName{Name: clusterVersionName}, clusterVersion)
	if err != nil {
		if errors.IsNotFound(err) {
			return "", nil
		}
		return "", err
	}
	return clusterVersion.Status.Desired.Version, nil
}

// NewClusterVersion returns a cluster version object
func NewClusterVersion(version string) *configv1.ClusterVersion {
	return &configv1.ClusterVersion{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterVersionName,
		},
		Spec: configv1.ClusterVersionSpec{
			Channel: "stable",
		},
		Status: configv1.ClusterVersionStatus{
			Desired: configv1.Release{
				Version: version,
			},
		},
	}
}

// InspectCluster probes the API server to determine which optional API groups
// (OLM, Monitoring, Route, Config, Console, Template, Apps, OAuth) are available
// in the cluster and sets the corresponding package-level flags. On non-OpenShift
// clusters where config.openshift.io is absent, only OLM, Monitoring, and Route
// APIs are checked; remaining OpenShift-specific groups are skipped.
func InspectCluster() error {
	var errs []error
	if err := verifyOLMAPI(); err != nil {
		errs = append(errs, err)
	}
	if err := verifyMonitoringAPI(); err != nil {
		errs = append(errs, err)
	}
	if err := verifyRouteAPI(); err != nil {
		errs = append(errs, err)
	}

	if err := verifyConfigAPI(); err != nil {
		errs = append(errs, err)
		return stderrors.Join(errs...)
	}
	if !configAPIFound {
		return stderrors.Join(errs...)
	}

	for _, check := range []func() error{
		verifyConsoleAPI,
		verifyTemplateAPI,
		verifyAppsAPI,
		verifyOAuthAPI,
	} {
		if err := check(); err != nil {
			errs = append(errs, err)
		}
	}
	return stderrors.Join(errs...)
}

// IsConfigAPIFound return true if the CRD config.openshift.io is available in the cluster and false otherwise.
func IsConfigAPIFound() bool {
	return configAPIFound
}

// IsOpenShiftCluster uses IsConfigAPIFound to check if the cluster is an OpenShift cluster.
func IsOpenShiftCluster() bool {
	return IsConfigAPIFound()
}

// verify if the Config.Openshift.io API is found
func verifyConfigAPI() error {
	found, err := verifyAPI(configv1.GroupName, configv1.GroupVersion.Version)
	if err != nil {
		return err
	}
	configAPIFound = found
	return nil
}

// IsConsoleAPIFound return true if the CRD console.openshift.io is available in the cluster.
func IsConsoleAPIFound() bool {
	return consoleAPIFound
}

func verifyConsoleAPI() error {
	found, err := verifyAPI(console.GroupName, console.GroupVersion.Version)
	if err != nil {
		return err
	}
	consoleAPIFound = found
	return nil
}

// IsRouteAPIFound return true if the CRD route.openshift.io is available in the cluster.
func IsRouteAPIFound() bool {
	return routeAPIFound
}

func verifyRouteAPI() error {
	found, err := verifyAPI(routev1.GroupName, routev1.GroupVersion.Version)
	if err != nil {
		return err
	}
	routeAPIFound = found
	return nil
}

func verifyMonitoringAPI() error {
	found, err := verifyAPI(
		monitoringv1.SchemeGroupVersion.Group,
		monitoringv1.SchemeGroupVersion.Version,
	)
	if err != nil {
		return err
	}
	monitoringAPIFound = found
	return nil
}

// IsMonitoringAPIFound return true if the CRD monitoring.coreos.com is available in the cluster.
func IsMonitoringAPIFound() bool {
	return monitoringAPIFound
}

// IsTemplateAPIFound return true if the CRD template.openshift.io is available in the cluster.
func IsTemplateAPIFound() bool {
	return templateAPIFound
}

func verifyTemplateAPI() error {
	found, err := verifyAPI(templatev1.GroupName, templatev1.GroupVersion.Version)
	if err != nil {
		return err
	}
	templateAPIFound = found
	return nil
}

// IsAppsAPIFound return true if the CRD apps.openshift.io is available in the cluster.
func IsAppsAPIFound() bool {
	return appsAPIFound
}

func verifyAppsAPI() error {
	found, err := verifyAPI(oappsv1.GroupName, oappsv1.GroupVersion.Version)
	if err != nil {
		return err
	}
	appsAPIFound = found
	return nil
}

// IsOAuthAPIFound return true if the CRD oauth.openshift.io is available in the cluster.
func IsOAuthAPIFound() bool {
	return oauthAPIFound
}

func verifyOAuthAPI() error {
	found, err := verifyAPI(oauthv1.GroupName, oauthv1.GroupVersion.Version)
	if err != nil {
		return err
	}
	oauthAPIFound = found
	return nil
}

// IsOLMAPIFound return true if the CRD operators.coreos.com is available in the cluster.
func IsOLMAPIFound() bool {
	return olmAPIFound
}

func verifyOLMAPI() error {
	found, err := verifyAPI(operatorsv1.GroupVersion.Group, operatorsv1.GroupVersion.Version)
	if err != nil {
		return err
	}
	olmAPIFound = found
	return nil
}

func ProxyEnvVars(vars ...corev1.EnvVar) []corev1.EnvVar {
	result := []corev1.EnvVar{}
	result = append(result, vars...)
	proxyKeys := []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY"}
	for _, p := range proxyKeys {
		if k, v := caseInsensitiveGetenv(p); k != "" {
			if p == "NO_PROXY" {
				v = addToNoProxy(v, ".cluster.local.") // add .cluster.local. (with trailing dot) to allow entry typically not added by OpenShift
			}
			result = append(result, corev1.EnvVar{Name: k, Value: v})
		}
	}
	return result
}

func caseInsensitiveGetenv(s string) (string, string) {
	if v := os.Getenv(s); v != "" {
		return s, v
	}
	ls := strings.ToLower(s)
	if v := os.Getenv(ls); v != "" {
		return ls, v
	}
	return "", ""
}

func addToNoProxy(noProxy string, entry string) string {
	if noProxy == "" {
		return entry
	}

	if !slices.Contains(strings.Split(noProxy, ","), entry) {
		return fmt.Sprintf("%s,%s", noProxy, entry)
	}
	return noProxy
}

func AddSeccompProfileForOpenShift(client client.Client, podspec *corev1.PodSpec) {

	version, _ := GetClusterVersion(client)
	if version == "" || semver.Compare(fmt.Sprintf("v%s", version), "v4.10.999") > 0 {
		if podspec.SecurityContext == nil {
			podspec.SecurityContext = &corev1.PodSecurityContext{}
		}
		if podspec.SecurityContext.SeccompProfile == nil {
			podspec.SecurityContext.SeccompProfile = &corev1.SeccompProfile{}
		}
		if len(podspec.SecurityContext.SeccompProfile.Type) == 0 {
			podspec.SecurityContext.SeccompProfile.Type = corev1.SeccompProfileTypeRuntimeDefault
		}
		if podspec.Containers[0].SecurityContext == nil {
			podspec.Containers[0].SecurityContext = &corev1.SecurityContext{
				AllowPrivilegeEscalation: new(false),
				Capabilities: &corev1.Capabilities{
					Drop: []corev1.Capability{
						"ALL",
					},
				},
				RunAsNonRoot: new(true),
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			}
		}
	}
}

// GetOperatorNamespace returns the namespace the operator is running in by reading
// the serviceaccount namespace file. If the file is not found (e.g. running locally),
// it returns the default operator namespace and a nil error.
func GetOperatorNamespace() (string, error) {
	data, err := os.ReadFile(operatorPodNamespacePath)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultOperatorNamespace, nil
		}
		return "", fmt.Errorf("error retrieving operator namespace: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}
