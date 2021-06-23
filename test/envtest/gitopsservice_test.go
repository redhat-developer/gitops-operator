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
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	console "github.com/openshift/api/console/v1"
	routev1 "github.com/openshift/api/route/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("GitOpsServiceController", func() {
	Context("Check if gitops backend resources are created", func() {
		name := "cluster"
		It("backend deployment is created", func() {
			checkIfPresent(types.NamespacedName{Name: name, Namespace: argoCDNamespace}, &appsv1.Deployment{})
		})

		It("backend service is created", func() {
			checkIfPresent(types.NamespacedName{Name: name, Namespace: argoCDNamespace}, &corev1.Service{})
		})

		It("backend route is created", func() {
			checkIfPresent(types.NamespacedName{Name: name, Namespace: argoCDNamespace}, &routev1.Route{})
		})
	})

	Context("Check if kam resources are created", func() {
		name := "kam"
		It("deployment that hosts kam is created", func() {
			checkIfPresent(types.NamespacedName{Name: name, Namespace: argoCDNamespace}, &appsv1.Deployment{})
		})

		It("service that serves kam is created", func() {
			checkIfPresent(types.NamespacedName{Name: name, Namespace: argoCDNamespace}, &corev1.Service{})
		})

		It("console CLI download resource that adds kam route to OpenShift's CLI download page", func() {

			By("route that serves kam is created")
			route := &routev1.Route{}
			checkIfPresent(types.NamespacedName{Name: name, Namespace: argoCDNamespace}, route)

			By("CLI download link is created")
			consoleCLIDownload := &console.ConsoleCLIDownload{}
			checkIfPresent(types.NamespacedName{Name: name}, consoleCLIDownload)

			By("CLI download link should match the kam route")
			consoleCLILink := strings.TrimLeft(consoleCLIDownload.Spec.Links[0].Href, "https://")
			Expect(route.Spec.Host + "/kam/").Should(Equal(consoleCLILink))
		})
	})
})
