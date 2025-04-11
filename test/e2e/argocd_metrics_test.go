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

package e2e

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"

	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"

	"k8s.io/apimachinery/pkg/types"
)

func runCommandWithOutput(cmdList ...string) (string, string, error) {

	// Output the commands to be run, so that if the test fails we can determine why
	fmt.Println(cmdList)

	cmd := exec.Command(cmdList[0], cmdList[1:]...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	stdoutStr := stdout.String()
	stderrStr := stderr.String()

	// Output the stdout/sterr text, so that if the test fails we can determine why
	fmt.Println(stdoutStr, stderrStr)

	return stdoutStr, stderrStr, err

}

var _ = Describe("Argo CD metrics controller", func() {
	Context("Check if monitoring resources are created", func() {
		It("Role is created", func() {
			buildYAML := filepath.Join("..", "appcrs", "build_appcr.yaml")
			ocPath, err := exec.LookPath("oc")
			Expect(err).NotTo(HaveOccurred())

			_, _, err = runCommandWithOutput(ocPath, "apply", "-f", buildYAML)
			Expect(err).NotTo(HaveOccurred())
			// Alert delay
			time.Sleep(60 * time.Second)

			role := rbacv1.Role{}
			readRoleName := fmt.Sprintf("%s-read", argoCDNamespace)
			checkIfPresent(types.NamespacedName{Name: readRoleName, Namespace: argoCDNamespace}, &role)
		})

		It("Rolebinding is created", func() {
			roleBinding := rbacv1.RoleBinding{}
			roleBindingName := fmt.Sprintf("%s-prometheus-k8s-read-binding", argoCDNamespace)
			checkIfPresent(types.NamespacedName{Name: roleBindingName, Namespace: argoCDNamespace}, &roleBinding)
		})

		It("Application service monitor is created", func() {
			serviceMonitor := monitoringv1.ServiceMonitor{}
			serviceMonitorName := argoCDInstanceName
			checkIfPresent(types.NamespacedName{Name: serviceMonitorName, Namespace: argoCDNamespace}, &serviceMonitor)
		})

		It("API server service monitor is created", func() {
			serviceMonitor := monitoringv1.ServiceMonitor{}
			serviceMonitorName := fmt.Sprintf("%s-server", argoCDInstanceName)
			checkIfPresent(types.NamespacedName{Name: serviceMonitorName, Namespace: argoCDNamespace}, &serviceMonitor)
		})

		It("Repo server service monitor is created", func() {
			serviceMonitor := monitoringv1.ServiceMonitor{}
			serviceMonitorName := fmt.Sprintf("%s-repo-server", argoCDInstanceName)
			checkIfPresent(types.NamespacedName{Name: serviceMonitorName, Namespace: argoCDNamespace}, &serviceMonitor)
		})

		It("Prometheus rule is created", func() {
			rule := monitoringv1.PrometheusRule{}
			ruleName := "gitops-operator-argocd-alerts"
			checkIfPresent(types.NamespacedName{Name: ruleName, Namespace: argoCDNamespace}, &rule)
		})
	})
})
