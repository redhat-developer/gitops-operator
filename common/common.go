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

import "os"

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
	DefaultConsoleImage = "quay.io/redhat-developer/gitops-console-plugin"
	// Default console plugin version
	DefaultConsoleVersion = "v0.1.0"
	// Default console plugin installation OCP version
	DefaultDynamicPluginStartOCPVersion = "4.15.0"
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
