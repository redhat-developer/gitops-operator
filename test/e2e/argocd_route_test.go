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
	"context"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	console "github.com/openshift/api/console/v1"
	routev1 "github.com/openshift/api/route/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("Argo CD ConsoleLink controller", func() {
	Context("Check if ConsoleLink resources are created", func() {
		route := &routev1.Route{}
		consoleLink := &console.ConsoleLink{}

		It("Argocd route is present", func() {
			checkIfPresent(types.NamespacedName{Name: argoCDRouteName, Namespace: argoCDNamespace}, route)
		})

		It("ConsoleLink is created", func() {
			checkIfPresent(types.NamespacedName{Name: consoleLinkName}, consoleLink)
		})

		It("ConsoleLink and argocd route should match", func() {
			Eventually(func() error {
				err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: consoleLinkName}, consoleLink)
				if err != nil {
					return err
				}
				err = k8sClient.Get(context.TODO(), types.NamespacedName{Name: argoCDRouteName, Namespace: argoCDNamespace}, route)
				if err != nil {
					return err
				}
				clLink := strings.TrimLeft(consoleLink.Spec.Href, "https://")
				routeLink := route.Spec.Host
				if clLink != routeLink {
					return fmt.Errorf("URL mismatch, route: %s, consoleLink: %s", routeLink, clLink)
				}
				return nil
			}, timeout, interval).ShouldNot(HaveOccurred())
		})
	})
})
