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

package envtest

import (
	"fmt"

	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	. "github.com/onsi/ginkgo"
	rbacv1 "k8s.io/api/rbac/v1"

	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("Argo CD metrics controller", func() {
	Context("Check if monitoring resources are created", func() {
		It("role is created", func() {
			role := rbacv1.Role{}
			readRoleName := fmt.Sprintf("%s-read", argoCDNamespace)
			checkIfPresent(types.NamespacedName{Name: readRoleName, Namespace: argoCDNamespace}, &role)
		})

		It("rolebinding is created", func() {
			roleBinding := rbacv1.RoleBinding{}
			roleBindingName := fmt.Sprintf("%s-prometheus-k8s-read-binding", argoCDNamespace)
			checkIfPresent(types.NamespacedName{Name: roleBindingName, Namespace: argoCDNamespace}, &roleBinding)
		})

		It("application service monitor is created", func() {
			serviceMonitor := monitoringv1.ServiceMonitor{}
			serviceMonitorName := argoCDInstanceName
			checkIfPresent(types.NamespacedName{Name: serviceMonitorName, Namespace: argoCDNamespace}, &serviceMonitor)
		})

		It("api server service monitor is created", func() {
			serviceMonitor := monitoringv1.ServiceMonitor{}
			serviceMonitorName := fmt.Sprintf("%s-server", argoCDInstanceName)
			checkIfPresent(types.NamespacedName{Name: serviceMonitorName, Namespace: argoCDNamespace}, &serviceMonitor)
		})

		It("repo server service monitor is created", func() {
			serviceMonitor := monitoringv1.ServiceMonitor{}
			serviceMonitorName := fmt.Sprintf("%s-repo-server", argoCDInstanceName)
			checkIfPresent(types.NamespacedName{Name: serviceMonitorName, Namespace: argoCDNamespace}, &serviceMonitor)
		})

		It("prometheus rule is created", func() {
			rule := monitoringv1.PrometheusRule{}
			ruleName := "gitops-operator-argocd-alerts"
			checkIfPresent(types.NamespacedName{Name: ruleName, Namespace: argoCDNamespace}, &rule)
		})
	})
})
