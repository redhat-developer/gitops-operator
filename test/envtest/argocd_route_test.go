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
	"log"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	console "github.com/openshift/api/console/v1"
	routev1 "github.com/openshift/api/route/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("Argo CD ConsoleLink controller", func() {
	Context("Check if ConsoleLink resources are created", func() {
		route := &routev1.Route{}
		consoleLink := &console.ConsoleLink{}

		It("argocd route is present", func() {
			checkIfPresent(types.NamespacedName{Name: argoCDRouteName, Namespace: argoCDNamespace}, route)
		})

		It("ConsoleLink is created", func() {
			checkIfPresent(types.NamespacedName{Name: consoleLinkName}, consoleLink)
		})

		It("ConsoleLink and argocd route should match", func() {
			checkIfPresent(types.NamespacedName{Name: consoleLinkName}, consoleLink)
			log.Println(consoleLink.Spec.Href, route.Spec.Host)
			Expect(strings.TrimLeft(consoleLink.Spec.Href, "https://")).Should(Equal(route.Spec.Host))
		})
	})
})
