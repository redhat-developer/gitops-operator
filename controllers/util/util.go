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
	"fmt"
	"os"
	"strings"

	"github.com/argoproj-labs/argocd-operator/controllers/argoutil"
	configv1 "github.com/openshift/api/config/v1"
	console "github.com/openshift/api/console/v1"
	"golang.org/x/mod/semver"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	clusterVersionName = "version"
)

var (
	consoleAPIFound = false
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

func InspectCluster() error {
	if err := verifyConsoleAPI(); err != nil {
		return err
	}
	return nil
}

func IsConsoleAPIFound() bool {
	return consoleAPIFound
}

// *** THIS SHOULD ONLY BE USED FOR UNIT TESTING ***
func SetConsoleAPIFound(found bool) {
	consoleAPIFound = found
}

func verifyConsoleAPI() error {
	found, err := argoutil.VerifyAPI(console.GroupName, console.GroupVersion.Version)
	if err != nil {
		return err
	}
	consoleAPIFound = found
	return nil
}

func ProxyEnvVars(vars ...corev1.EnvVar) []corev1.EnvVar {
	result := []corev1.EnvVar{}
	result = append(result, vars...)
	proxyKeys := []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY"}
	for _, p := range proxyKeys {
		if k, v := caseInsensitiveGetenv(p); k != "" {
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

// BoolPtr returns a pointer to val
func BoolPtr(val bool) *bool {
	return &val
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
				AllowPrivilegeEscalation: BoolPtr(false),
				Capabilities: &corev1.Capabilities{
					Drop: []corev1.Capability{
						"ALL",
					},
				},
				RunAsNonRoot: BoolPtr(true),
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			}
		}
	}
}
