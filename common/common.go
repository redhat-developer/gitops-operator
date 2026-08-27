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

package common

import (
	"os"
)

const (
	// ArgoCDInstanceName is the default Argo CD instance name
	ArgoCDInstanceName = "openshift-gitops"
	// DisableDefaultInstallEnvVar is an env variable to disable the default instance
	DisableDefaultInstallEnvVar = "DISABLE_DEFAULT_ARGOCD_INSTANCE"
	// DisableDefaultArgoCDConsoleLink is an env variable to disable the default Argo CD ConsoleLink
	DisableDefaultArgoCDConsoleLink = "DISABLE_DEFAULT_ARGOCD_CONSOLELINK"
	// InfraNodeLabelSelector is a nodeSelector for infrastructure nodes in Openshift
	InfraNodeLabelSelector = "node-role.kubernetes.io/infra"
	// Default console plugin image
	DefaultConsoleImage = "quay.io/redhat-user-workloads/rh-openshift-gitops-tenant/console-plugin-rhel9"
	// DefaultConsoleImagePF5 is Default console plugin image for PatternFly 5
	DefaultConsoleImagePF5 = "quay.io/redhat-user-workloads/rh-openshift-gitops-tenant/console-plugin-4-18-rhel9"
	// Default console plugin version
	DefaultConsoleVersion = "main"
	// DefaultConsoleVersionPF5 is a Default console plugin version for PatternFly 5
	DefaultConsoleVersionPF5 = "main"
	// DefaultDynamicPluginStartOCPVersion is the minimum OCP version that supports the console plugin
	DefaultDynamicPluginStartOCPVersion = "4.18.0"
	// PluginPF6MinOCPVersion is the minimum OCP version that should use the PF6-based plugin;
	// OCP versions >= 4.18 and < 4.19 use the PF5-based plugin instead.
	PluginPF6MinOCPVersion = "4.19.0"
	// ImagePullPolicyEnvVar is the environment variable for configuring image pull policy
	ImagePullPolicy = "IMAGE_PULL_POLICY"
	// InfraNodeSelectorAnnotation is the OpenShift namespace annotation that applies a default node selector to all pods
	InfraNodeSelectorAnnotation = "openshift.io/node-selector"
	// InfraNodeSelectorAnnotationValue is the value for the infra node selector annotation
	InfraNodeSelectorAnnotationValue = "node-role.kubernetes.io/infra="
)

// InfraNodeSelector returns openshift label for infrastructure nodes
func InfraNodeSelector() map[string]string {
	return map[string]string{
		"node-role.kubernetes.io/infra": "",
	}
}

func StringFromEnv(env string, defaultValue string) string {
	if str := os.Getenv(env); str != "" {
		return str
	}
	return defaultValue
}
