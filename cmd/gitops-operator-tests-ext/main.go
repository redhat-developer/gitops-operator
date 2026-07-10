package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/openshift-eng/openshift-tests-extension/pkg/cmd"
	e "github.com/openshift-eng/openshift-tests-extension/pkg/extension"
	g "github.com/openshift-eng/openshift-tests-extension/pkg/ginkgo"

	// Register parallel Ginkgo E2E tests from gitops-operator.
	_ "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/parallel"
)

func main() {
	registry := e.NewRegistry()
	ext := e.NewExtension("openshift", "payload", "gitops-operator")

	// Mirrors openshift/release gitops-operator-tests step: parallel ginkgo suite only.
	ext.AddSuite(e.Suite{
		Name: "openshift/gitops-operator/parallel",
		Qualifiers: []string{
			`name.contains("GitOps Operator Parallel E2E Test")`,
		},
	})

	specs, err := g.BuildExtensionTestSpecsFromOpenShiftGinkgoSuite()
	if err != nil {
		panic(fmt.Sprintf("couldn't build extension test specs from ginkgo: %+v", err.Error()))
	}

	ext.AddSpecs(specs)
	registry.Register(ext)

	root := &cobra.Command{
		Use:   "gitops-operator-tests-ext",
		Short: "OpenShift GitOps Operator tests extension",
		Long:  "Runs gitops-operator parallel E2E tests via openshift-tests-extension.",
	}

	root.AddCommand(cmd.DefaultExtensionCommands(registry)...)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
