// Copyright 2025 ArgoCD Operator Developers
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package argoutil

import (
	"fmt"
	"os"
	"strings"
)

func IsNamespaceClusterConfigNamespace(ns string) bool {
	return allowedNamespace(ns, os.Getenv("ARGOCD_CLUSTER_CONFIG_NAMESPACES"))
}

func allowedNamespace(current string, namespaces string) bool {
	clusterConfigNamespaces := splitList(namespaces)
	if len(clusterConfigNamespaces) > 0 {
		if clusterConfigNamespaces[0] == "*" {
			return true
		}

		for _, n := range clusterConfigNamespaces {
			if n == current {
				return true
			}
		}
	}
	return false
}

const OperatorNamespaceFile = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

func GetOperatorNamespace() (string, error) {
	if _, err := os.Stat(OperatorNamespaceFile); os.IsNotExist(err) {
		// read from env variable ARGOCD_OPERATOR_NAMESPACE for local run
		if os.Getenv("ARGOCD_OPERATOR_NAMESPACE") != "" {
			return os.Getenv("ARGOCD_OPERATOR_NAMESPACE"), nil
		}
		// If you are seeing this error:
		// - You are likely running the operator outside a cluster (e.g. within development/test environment via Makefile)
		// - You likely need to set `ARGOCD_OPERATOR_NAMESPACE` env var before starting operator (or running unit test). You could also temporarily hardcode it if that's easier for your use case.
		// - In most cases, this should already be handled by Makefile.
		// - See Makefile for an example of how this looks.
		// - You should never see this error in production.
		return "", fmt.Errorf("operator namespace file does not exist and if running locally set ARGOCD_OPERATOR_NAMESPACE")
	}

	data, err := os.ReadFile(OperatorNamespaceFile)
	if err != nil {
		return "", fmt.Errorf("failed to read operator namespace: %w", err)
	}
	ns := strings.TrimSpace(string(data))
	if ns == "" {
		return "", fmt.Errorf("operator namespace file is empty")
	}
	return ns, nil
}

func splitList(s string) []string {
	elems := strings.Split(s, ",")
	for i := range elems {
		elems[i] = strings.TrimSpace(elems[i])
	}
	return elems
}
