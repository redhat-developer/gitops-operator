/*
Copyright 2025.

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

package upgrade

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture"
	nodeFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/node"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// This suite answers one question: does a GitOps Operator installation that is actually
// in use survive an upgrade?
//
// It installs the operator from PREVIOUS_CHANNEL, applies the configuration in config/
// (which turns on as many features as it can), checks the features work, upgrades to
// NEW_CHANNEL, and runs the same checks again.
//
// The steps run in order, and the first failure stops the rest. Nothing is cleaned up,
// so that a failed cluster can be inspected. See README.md for how to run it.

var (
	ctx       = context.Background()
	k8sClient client.Client
)

var _ = Describe("GitOps Operator upgrade", Ordered, func() {

	var (
		previousChannel = os.Getenv("PREVIOUS_CHANNEL")
		previousVersion = os.Getenv("PREVIOUS_VERSION") // optional
		newChannel      = os.Getenv("NEW_CHANNEL")
		newVersion      = os.Getenv("NEW_VERSION") // optional

		specsBeforeUpgrade map[string]any
		podsBeforeUpgrade  map[string]types.UID
	)

	BeforeAll(func() {
		Expect(previousChannel).ToNot(BeEmpty(), "PREVIOUS_CHANNEL must be set, for example gitops-1.18")
		Expect(newChannel).ToNot(BeEmpty(), "NEW_CHANNEL must be set, for example gitops-1.19")

		k8sClient, _ = utils.GetE2ETestKubeClient()

		fixture.EnsureRunningOnOpenShift()
		nodeFixture.ExpectHasAtLeastXNodes(3) // the configuration enables Redis HA

		expectOperatorIsNotInstalled()
	})

	It("installs the previous version of the operator", func() {
		installOperator(previousChannel, previousVersion)
	})

	It("applies the configuration", func() {
		applyConfig()
	})

	It("has every feature working before the upgrade", func() {
		verifyFeatures()

		specsBeforeUpgrade = configSpecs()
		podsBeforeUpgrade = workloadPods()
		Expect(podsBeforeUpgrade).To(HaveLen(3), "expected one nginx pod per workload namespace")
	})

	It("upgrades the operator", func() {
		upgradeOperator(newChannel, newVersion)
	})

	It("has every feature working after the upgrade", func() {
		verifyFeatures()
	})

	It("did not change the configuration", func() {
		specsAfterUpgrade := configSpecs()

		// A new operator version may add fields to a spec: 1.22 adds an empty commitServer
		// and sourceHydrator to the default instance. That is printed, but it is not a
		// failure. Losing or changing a value that was there before the upgrade is.
		var lost []string
		for name, before := range specsBeforeUpgrade {
			GinkgoWriter.Printf("%s (- before, + after):\n%s\n", name, cmp.Diff(before, specsAfterUpgrade[name]))
			lost = append(lost, removedOrChanged(name+" .spec", before, specsAfterUpgrade[name])...)
		}
		Expect(lost).To(BeEmpty(), "the upgrade removed or changed configuration:\n%s", strings.Join(lost, "\n"))
	})

	It("did not restart the workloads", func() {
		Expect(workloadPods()).To(Equal(podsBeforeUpgrade))
	})
})

// removedOrChanged lists every value in 'before' that is missing from 'after' or is
// different there. Values that are only in 'after' are ignored.
func removedOrChanged(path string, before any, after any) []string {
	beforeMap, isMap := before.(map[string]any)
	afterMap, _ := after.(map[string]any)
	if !isMap || afterMap == nil {
		if !reflect.DeepEqual(before, after) {
			return []string{fmt.Sprintf("%s: was %v, is now %v", path, before, after)}
		}
		return nil
	}

	var found []string
	for key, value := range beforeMap {
		afterValue, exists := afterMap[key]
		if !exists {
			found = append(found, fmt.Sprintf("%s.%s: was %v, is now removed", path, key, value))
			continue
		}
		found = append(found, removedOrChanged(path+"."+key, value, afterValue)...)
	}
	return found
}
