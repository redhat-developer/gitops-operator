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
	"fmt"
	"time"

	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

const (
	timeout  = time.Second * 10
	duration = time.Second * 10
	interval = time.Millisecond * 250
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
	})
})

func checkIfPresent(ns types.NamespacedName, obj runtime.Object) {
	Eventually(func() bool {
		err := k8sClient.Get(context.TODO(), ns, obj)
		if err != nil {
			return false
		}
		return true
	}, timeout, interval).Should(BeTrue())
}
