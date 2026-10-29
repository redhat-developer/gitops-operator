package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/argoproj-labs/argocd-operator/dependency-upgrade/utils"
)

const (
	GitOpsPromoterRepoURL       = "https://github.com/argoproj-labs/gitops-promoter"
	gitOpsPromoterModulePath    = "github.com/argoproj-labs/gitops-promoter"
	gitOpsPromoterVersionEnvVar = "GITOPS_PROMOTER_VERSION"

	// promoterCRDFilePrefix matches the CRD files mirrored from the upstream repository into
	// config/crd/bases, e.g. promoter.argoproj.io_promotionstrategies.yaml.
	promoterCRDFilePrefix = "promoter.argoproj.io_"

	// kustomizationScaffoldResource is the marker in config/crd/kustomization.yaml before which
	// new CRD resources are added by kubebuilder.
	kustomizationScaffoldResource = "#+kubebuilder:scaffold:crdkustomizeresource"
)

// TODO: Add debug statements
func main() {
	wd, err := os.Getwd()
	if err != nil {
		utils.ExitWithError(fmt.Errorf("unable to get working dir: %v", err))
		return
	}

	argocdOperatorRoot, err := filepath.Abs(wd + "/../../..")
	if err != nil {
		utils.ExitWithError(fmt.Errorf("unable to get absolute dir: %v", err))
		return
	}

	// argocd-operator is a Go submodule of the gitops-operator repository. The operator
	// manifests (config/, bundle/) are maintained by the parent module.
	gitopsOperatorRoot := filepath.Dir(argocdOperatorRoot)

	// The CRDs must match the gitops-promoter controller that is actually vendored, so the
	// go.mod pin (bumped by dependabot) is the source of truth. This script does not update
	// go.mod/vendor, so it must never copy CRDs from a different release.
	argocdOperatorGoMod := filepath.Join(argocdOperatorRoot, "go.mod")
	targetGitOpsPromoterVersion := readGitOpsPromoterVersionFromGoMod(argocdOperatorGoMod)
	if override := os.Getenv(gitOpsPromoterVersionEnvVar); override != "" && override != targetGitOpsPromoterVersion {
		utils.ExitWithError(fmt.Errorf("%s=%s does not match the %s version pinned in %s (%s); update go.mod/vendor first so the copied CRDs match the vendored controller",
			gitOpsPromoterVersionEnvVar, override, gitOpsPromoterModulePath, argocdOperatorGoMod, targetGitOpsPromoterVersion))
		return
	}

	fmt.Println()
	fmt.Println("Cloning GitOps Promoter Repo", targetGitOpsPromoterVersion)
	fmt.Println()

	// Clone GitOps Promoter into a temporary directory
	gitOpsPromoterRepoRoot, err := utils.CloneRepoIntoTempDir(GitOpsPromoterRepoURL, targetGitOpsPromoterVersion)
	if err != nil {
		utils.ExitWithError(fmt.Errorf("unable to checkout GitOps Promoter %s: %v", targetGitOpsPromoterVersion, err))
		return
	}

	// Sanity test that that we have the correct root path for argocd-operator repo
	if rootGoModExists, err := utils.FileExists(filepath.Join(argocdOperatorRoot, "go.mod")); err != nil || !rootGoModExists {
		utils.ExitWithError(fmt.Errorf("script should be run from 'hack/update-dependencies-script/gitops-promoter' directory: %v", err))
		return
	}

	// Sanity test that the parent gitops-operator module (which owns config/ and bundle/) exists
	if rootGoModExists, err := utils.FileExists(filepath.Join(gitopsOperatorRoot, "go.mod")); err != nil || !rootGoModExists {
		utils.ExitWithError(fmt.Errorf("gitops-operator go.mod not found in '%s': %v", gitopsOperatorRoot, err))
		return
	}

	fmt.Println()
	fmt.Println("Copying CRDs")
	fmt.Println()

	copiedCRDFiles := updateGitOpsPromoterCRDs(gitOpsPromoterRepoRoot, gitopsOperatorRoot)
	updateGitOpsPromoterCRDKustomization(gitopsOperatorRoot, copiedCRDFiles)

	// config/manifests includes config/crd, so the bundle must be regenerated after the CRDs are
	// copied. The aggregate update-dependencies runner processes the Argo CD updater first, whose
	// bundle generation would otherwise leave the copied GitOps Promoter CRDs out of the bundle.
	fmt.Println()
	fmt.Println("Regenerating bundle")
	fmt.Println()

	if err := utils.RunCommandListWithWorkDir(gitopsOperatorRoot, [][]string{{"make", "bundle"}}); err != nil {
		utils.ExitWithError(fmt.Errorf("unable to regenerate bundle in %s: %v", gitopsOperatorRoot, err))
		return
	}
}

// readGitOpsPromoterVersionFromGoMod returns the gitops-promoter version pinned in the given go.mod.
func readGitOpsPromoterVersionFromGoMod(goModPath string) string {
	lines := utils.GetFileContentsAsLines(goModPath)

	var versions []string
	for _, line := range lines {
		fields := strings.Fields(line)
		// Ignore replace directives ("module => ...") and unrelated dependencies.
		if len(fields) < 2 || fields[0] != gitOpsPromoterModulePath || fields[1] == "=>" {
			continue
		}
		versions = append(versions, fields[1])
	}

	if len(versions) != 1 {
		utils.ExitWithError(fmt.Errorf("expected exactly one requirement for %s in %s, found %v", gitOpsPromoterModulePath, goModPath, versions))
		return ""
	}

	return versions[0]
}

// updateGitOpsPromoterCRDs copies the GitOps Promoter CRDs into the gitops-operator module,
// which owns the operator manifests (config/crd/bases), and returns the copied filenames.
func updateGitOpsPromoterCRDs(promoterRepoRoot, gitopsOperatorRoot string) []string {
	crdSourcePath := filepath.Join(promoterRepoRoot, "config", "crd", "bases")

	entries, err := os.ReadDir(crdSourcePath)
	if err != nil {
		utils.ExitWithError(fmt.Errorf("unable to list GitOps Promoter CRDs: %v", err))
		return nil
	}

	var copied []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), promoterCRDFilePrefix) || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		srcFile := filepath.Join(crdSourcePath, entry.Name())
		destFile := filepath.Join(gitopsOperatorRoot, "config", "crd", "bases", entry.Name())

		if err := utils.CopyFile(srcFile, destFile); err != nil {
			utils.ExitWithError(fmt.Errorf("unable to copy %s to %s: %v", srcFile, destFile, err))
			return nil
		}

		copied = append(copied, entry.Name())
	}

	// Sanity test: if upstream reorganized its CRD directory, the loop above would silently
	// copy nothing and leave the operator pinned to the previous release.
	if len(copied) == 0 {
		utils.ExitWithError(fmt.Errorf("no GitOps Promoter CRDs found in %s; the upstream layout may have changed", crdSourcePath))
		return nil
	}

	return copied
}

// updateGitOpsPromoterCRDKustomization reconciles config/crd/kustomization.yaml with the CRDs
// copied from upstream: new CRD files are added, and entries (and mirrored files) for CRDs that
// upstream no longer ships are removed. config/manifests includes config/crd, so this list
// determines which CRDs end up in the OLM bundle.
func updateGitOpsPromoterCRDKustomization(gitopsOperatorRoot string, copiedCRDFiles []string) {
	kustomizationPath := filepath.Join(gitopsOperatorRoot, "config", "crd", "kustomization.yaml")
	basesDir := filepath.Join(gitopsOperatorRoot, "config", "crd", "bases")

	copied := make(map[string]bool, len(copiedCRDFiles))
	for _, fname := range copiedCRDFiles {
		copied[fname] = true
	}

	// Remove mirrored CRD files that upstream no longer ships, so they cannot linger in the bundle.
	entries, err := os.ReadDir(basesDir)
	if err != nil {
		utils.ExitWithError(fmt.Errorf("unable to list CRDs in %s: %v", basesDir, err))
		return
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, promoterCRDFilePrefix) || !strings.HasSuffix(name, ".yaml") || copied[name] {
			continue
		}

		if err := os.Remove(filepath.Join(basesDir, name)); err != nil {
			utils.ExitWithError(fmt.Errorf("unable to remove stale CRD %s: %v", name, err))
			return
		}
		fmt.Println("Removed stale CRD", name)
	}

	resources := make([]string, 0, len(copiedCRDFiles))
	for _, fname := range copiedCRDFiles {
		resources = append(resources, "- bases/"+fname)
	}
	sort.Strings(resources)

	lines := utils.GetFileContentsAsLines(kustomizationPath)

	var (
		newLines         []string
		resourcesWritten bool
	)
	writeResources := func() {
		if resourcesWritten {
			return
		}
		newLines = append(newLines, resources...)
		resourcesWritten = true
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Replace the existing block of promoter CRD resources in place, regardless of which
		// entries were added or removed upstream.
		if strings.HasPrefix(trimmed, "- bases/"+promoterCRDFilePrefix) {
			writeResources()
			continue
		}

		if !resourcesWritten && strings.HasPrefix(trimmed, kustomizationScaffoldResource) {
			writeResources()
		}

		newLines = append(newLines, line)
	}

	if !resourcesWritten {
		utils.ExitWithError(fmt.Errorf("unable to find where to add GitOps Promoter CRDs in %s; expected an existing '%s' resource or the '%s' marker",
			kustomizationPath, promoterCRDFilePrefix, kustomizationScaffoldResource))
		return
	}

	if err := os.WriteFile(kustomizationPath, []byte(strings.Join(newLines, "\n")), 0o600); err != nil {
		utils.ExitWithError(fmt.Errorf("unable to update %s: %v", kustomizationPath, err))
		return
	}
}
