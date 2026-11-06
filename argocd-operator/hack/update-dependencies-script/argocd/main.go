package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/argoproj-labs/argocd-operator/dependency-upgrade/utils"
)

const (
	ArgoCDGitHubRepoURL = "https://github.com/argoproj/argo-cd"

	// Argo CD's install manifests reference images on public.ecr.aws, but the operator
	// deploys mirrored images from quay.io (see common/defaults.go). Resolve the digest
	// against the mirror so we never write a public.ecr.aws digest for a quay.io image.
	// This constant should be consistent with ArgoCDDefaultRedisImage in common/defaults.go.
	redisMirrorImageURL = "quay.io/akuity/redis"

	// redisMirrorImageEnvVar optionally overrides redisMirrorImageURL, for example when the
	// default mirror has not picked up an upstream redis release yet.
	redisMirrorImageEnvVar = "REDIS_MIRROR_IMAGE"
)

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

	// Version variable is in the form of 'v3.1.1', note the 'v' prefix
	targetArgoCDVersion := readTargetVersionFromMakefile(argocdOperatorRoot)

	fmt.Println()
	fmt.Println("Target Argo CD version from Makefile is", targetArgoCDVersion)
	fmt.Println()

	// Clone Argo CD into a temporary directory
	argoCDRepoRoot, err := utils.CloneRepoIntoTempDir(ArgoCDGitHubRepoURL, targetArgoCDVersion)
	if err != nil {
		utils.ExitWithError(fmt.Errorf("unable to checkout Argo CD: %v", err))
		return
	}

	fmt.Println("Using Argo CD temporary directory:", argoCDRepoRoot)

	// Sanity test that that we have the correct root path for argocd-operator repo
	if rootGoModExists, err := utils.FileExists(filepath.Join(argocdOperatorRoot, "go.mod")); err != nil || !rootGoModExists {
		utils.ExitWithError(fmt.Errorf("script should be run from 'hack/update-dependencies-script/argo-cd' directory: %v", err))
		return
	}

	// Sanity test that the parent gitops-operator module (which owns config/ and bundle/) exists
	if rootGoModExists, err := utils.FileExists(filepath.Join(gitopsOperatorRoot, "go.mod")); err != nil || !rootGoModExists {
		utils.ExitWithError(fmt.Errorf("gitops-operator go.mod not found in '%s': %v", gitopsOperatorRoot, err))
		return
	}

	// update 'common/defaults.go': container images for dex, redis, redis HA, argo-cd, etc
	_, dexContainerInfo := upgradeCommonDefaultsGo(argoCDRepoRoot, argocdOperatorRoot)

	// update Argo CD crds
	updateArgoCDCRDs(argoCDRepoRoot, gitopsOperatorRoot)

	// update go.mod and regenerate the manifests/bundle: acquire target argo cd version, and ensures replace block matches upstream
	updateGoModAndRegenBundle(targetArgoCDVersion, argocdOperatorRoot, gitopsOperatorRoot, argoCDRepoRoot)

	// update controllers/argocd/dex_test.go: test references a dex container image
	replaceDexImageReferenceInDexTest(argocdOperatorRoot, dexContainerInfo)

	fmt.Println()
	fmt.Println("Dependency update is complete:")
	fmt.Println("- You may wish to use a comparison tool to compare the 'manifests/install.yaml' file from the old and new versions of argo-cd, to verify no additional operator changes are required. ")
	fmt.Println("- You may wish to run 'make test' to verify the dependencies are in a consistent state.")
	fmt.Println()
}

func readTargetVersionFromMakefile(argocdOperatorRoot string) string {
	// Target version in Makefile looks like this:
	// ARGO_CD_TARGET_VERSION ?= 3.1.1

	fileToRead := filepath.Join(argocdOperatorRoot, "Makefile")

	bytes, err := os.ReadFile(fileToRead)
	if err != nil {
		utils.ExitWithError(fmt.Errorf("unable to read %s: %v", fileToRead, err))
		return ""
	}

	matches := utils.GrepForString(string(bytes), "ARGO_CD_TARGET_VERSION ?= ")

	if len(matches) != 1 {
		utils.ExitWithError(fmt.Errorf("unexpected number of matches for ARGO_CD_TARGET_VERSION: %v", matches))
		return ""
	}

	line := matches[0]

	if strings.Contains(line, "#") {
		utils.ExitWithError(fmt.Errorf("version line should not contain any comments"))
		return ""
	}

	setIndex := strings.Index(line, "?=")

	res := strings.TrimSpace(line[setIndex+2:])

	if !strings.HasPrefix(res, "v") {
		res = "v" + res
	}

	return res
}

func replaceDexImageReferenceInDexTest(argocdOperatorRoot string, dexContainerImage *processedContainerImage) {
	fileToUpdate := filepath.Join(argocdOperatorRoot, "controllers", "argocd", "dex_test.go")

	lines := utils.GetFileContentsAsLines(fileToUpdate)

	// Entry to replace looks like this:
	// Name:  "dex",
	// Image: "ghcr.io/dexidp/dex@sha256:d5f887574312f606c61e7e188cfb11ddb33ff3bf4bd9f06e6b1458efca75f604",

	newContent := ""

	match := false

	for idx := 0; idx < len(lines); idx++ {
		if strings.HasPrefix(lines[idx], "						Name:  \"dex\",") &&
			strings.HasPrefix(lines[idx+1], "						Image: \"ghcr.io/dexidp/dex@sha256:") {

			newContent += lines[idx] + "\n" // No modification of first line required
			newContent += "						Image: \"ghcr.io/dexidp/dex@" + dexContainerImage.sha256Digest + "\", // (" + dexContainerImage.version + ") NOTE: this value is modified by dependency update script\n"

			idx++ // Skip to the line after these 2
			match = true
		} else {
			newContent += lines[idx] + "\n"
		}
	}

	if !match {
		utils.ExitWithError(fmt.Errorf("unable to locate reference to dex image in dex_test.go"))
		return
	}

	newContent = strings.TrimSpace(newContent) + "\n"

	if err := os.WriteFile(fileToUpdate, []byte(newContent), 0o600); err != nil {
		utils.ExitWithError(fmt.Errorf("unable to update %s: %v", fileToUpdate, err))
		return
	}
}

// upgrade common/defaults.go
func upgradeCommonDefaultsGo(argoCDRepoRoot string, argocdOperatorRoot string) (*processedContainerImage, *processedContainerImage) {
	// Parse install YAML
	fileToParse := filepath.Join(argoCDRepoRoot, "manifests/ha/install.yaml")

	installYamlBytes, err := os.ReadFile(fileToParse)
	if err != nil {
		utils.ExitWithError(fmt.Errorf("unable to read Argo CD install yaml"))
		return nil, nil
	}

	installYAMLContents := string(installYamlBytes)

	targetDexImageLine := utils.GrepForString(installYAMLContents, "ghcr.io/dexidp/dex:")
	if len(targetDexImageLine) != 1 {
		utils.ExitWithError(fmt.Errorf("unexpected target dex image value: %v", targetDexImageLine))
		return nil, nil
	}
	targetDexImage := utils.StripImagePrefix(targetDexImageLine[0])

	targetRedisImageLine := utils.RemoveDuplicateLines(utils.GrepForString(installYAMLContents, "public.ecr.aws/docker/library/redis:"))
	if len(targetRedisImageLine) != 1 {
		utils.ExitWithError(fmt.Errorf("unexpected target redis image value: %v", targetRedisImageLine))
		return nil, nil
	}
	targetRedisImage := utils.StripImagePrefix(targetRedisImageLine[0])

	// ArgoCDDefaultRedisHAProxyVersion is intentionally not derived from Argo CD's manifests:
	// the operator deliberately pins a newer haproxy (3.3.17-alpine, built with OpenSSL 3.5)
	// than the one Argo CD references, so that TLS curve preferences can be configured for the
	// Redis HA proxy. Update that constant manually when bumping haproxy.

	targetArgoCDImageLine := utils.RemoveDuplicateLines(utils.GrepForString(installYAMLContents, "quay.io/argoproj/argocd:"))
	if len(targetArgoCDImageLine) != 1 {
		utils.ExitWithError(fmt.Errorf("unexpected target argo cd image value: %v", targetArgoCDImageLine))
		return nil, nil
	}
	targetArgoCDImage := utils.StripImagePrefix(targetArgoCDImageLine[0])

	fmt.Println()
	fmt.Println("Found the following images in Argo CD manifests:")
	fmt.Println("-", targetArgoCDImage)
	fmt.Println("-", targetDexImage)
	fmt.Println("-", targetRedisImage)

	dexContainerInfo, err := retrieveSHA256DigestUsingSkopeo(targetDexImage)
	if err != nil {
		utils.ExitWithError(fmt.Errorf("unable to resolve dex image digest: %v", err))
		return nil, nil
	}

	argoCDContainerInfo, err := retrieveSHA256DigestUsingSkopeo(targetArgoCDImage)
	if err != nil {
		utils.ExitWithError(fmt.Errorf("unable to resolve Argo CD image digest: %v", err))
		return nil, nil
	}

	// Argo CD's install manifests reference redis on public.ecr.aws, but the operator deploys the
	// mirrored image from quay.io (see ArgoCDDefaultRedisImage in common/defaults.go), so resolve
	// the digest against the mirror.
	redisTag, err := imageTagFromRef(targetRedisImage)
	if err != nil {
		utils.ExitWithError(err)
		return nil, nil
	}

	redisMirrorImage := redisMirrorImageURL
	if override := os.Getenv(redisMirrorImageEnvVar); override != "" {
		redisMirrorImage = override
	}

	redisContainerInfo, redisErr := retrieveSHA256DigestUsingSkopeo(redisMirrorImage + ":" + redisTag)
	if redisErr != nil {
		// The mirror is a project-specific extra, not something Argo CD ships. Do not abort the
		// whole dependency update when it lags upstream: keep the currently pinned redis digests
		// (they still point at the previously shipped image) and let everything else update.
		fmt.Println()
		fmt.Printf("WARNING: unable to resolve redis digest for %s:%s: %v\n", redisMirrorImage, redisTag, redisErr)
		fmt.Printf("WARNING: leaving ArgoCDDefaultRedisVersion and ArgoCDDefaultRedisVersionHA unchanged; re-run once the tag is mirrored, or set %s to an alternative mirror\n", redisMirrorImageEnvVar)
		fmt.Println()
	}

	if err := replaceLineInCommonDefaultGo(argocdOperatorRoot, "ArgoCDDefaultArgoVersion", *argoCDContainerInfo); err != nil {
		utils.ExitWithError(fmt.Errorf("unable to replace dex version: %v", err))
		return nil, nil
	}

	if err := replaceLineInCommonDefaultGo(argocdOperatorRoot, "ArgoCDDefaultDexVersion", *dexContainerInfo); err != nil {
		utils.ExitWithError(fmt.Errorf("unable to replace dex version: %v", err))
		return nil, nil
	}

	if redisContainerInfo != nil {
		if err := replaceLineInCommonDefaultGo(argocdOperatorRoot, "ArgoCDDefaultRedisVersionHA", *redisContainerInfo); err != nil {
			utils.ExitWithError(fmt.Errorf("unable to replace redis HA version: %v", err))
			return nil, nil
		}

		if err := replaceLineInCommonDefaultGo(argocdOperatorRoot, "ArgoCDDefaultRedisVersion", *redisContainerInfo); err != nil {
			utils.ExitWithError(fmt.Errorf("unable to replace redis version: %v", err))
			return nil, nil
		}
	}

	return argoCDContainerInfo, dexContainerInfo
}

// updateArgoCDCRDs copies the Argo CD CRDs into the gitops-operator module, which owns
// the operator manifests (config/crd/bases).
func updateArgoCDCRDs(argoCDRepoRoot string, gitopsOperatorRoot string) {
	argoCDCRDSourcePath := filepath.Join(argoCDRepoRoot, "manifests", "crds")

	entries, err := os.ReadDir(argoCDCRDSourcePath)
	if err != nil {
		utils.ExitWithError(fmt.Errorf("unable to list Argo CD CRDS: %v", err))
		return
	}

	var count int
	for _, entry := range entries {
		fname := entry.Name()
		if entry.IsDir() || fname == "kustomization.yaml" {
			continue
		}

		if strings.HasSuffix(fname, ".yaml") {
			count++
		}
	}

	filesToCopy := map[string]string{
		// argo cd repo YAML filename -> argocd-operator YAML filename
		"application-crd.yaml":    "argoproj.io_applications.yaml",
		"applicationset-crd.yaml": "argoproj.io_applicationsets.yaml",
		"appproject-crd.yaml":     "argoproj.io_appprojects.yaml",
	}

	// Sanity test: The CRDs found in Argo CD directory should match the values in the map above
	if len(filesToCopy) != count {
		utils.ExitWithError(fmt.Errorf("unexpected number of YAML files found in Argo CD CRD directory '%s': %d", argoCDCRDSourcePath, count))
		return
	}

	for k, v := range filesToCopy {
		srcFile := filepath.Join(argoCDCRDSourcePath, k)
		destFile := filepath.Join(gitopsOperatorRoot, "config", "crd", "bases", v)

		if err := utils.CopyFile(srcFile, destFile); err != nil {
			utils.ExitWithError(fmt.Errorf("unable to copy %s to %s: %v", srcFile, destFile, err))
			return
		}

	}
}

// readReplaceBlockFromGoMod will read the following block from a go.mod:
//
// module github.com/argoproj-labs/argocd-operator
//
// go 1.24.6
// require (
//
//	(...)
//
// )
// replace (
//
//	// <=== The values from inside here are returned
//
// )
//
// This function expects only one replace block to exist, and will fail if 0 or >1 are found.
func readReplaceBlockFromGoMod(pathToGoMod string) ([]string, int, int) {
	lines := utils.GetFileContentsAsLines(pathToGoMod)

	replaceBlockStart := -1
	replaceBlockEnd := -1
	for x := 0; x < len(lines); x++ {

		line := lines[x]
		if strings.HasPrefix(line, "replace (") {

			if replaceBlockStart != -1 {
				utils.ExitWithError(fmt.Errorf("multiple replace blocks detected in argocd go.mod"))
				return nil, replaceBlockStart, replaceBlockEnd
			}

			replaceBlockStart = x + 1
		}

		if replaceBlockStart != -1 && strings.HasPrefix(line, ")") {
			replaceBlockEnd = x - 1
		}
	}

	if replaceBlockStart == -1 {
		utils.ExitWithError(fmt.Errorf("replace block start not found"))
		return nil, replaceBlockStart, replaceBlockEnd
	}

	if replaceBlockEnd == -1 {
		utils.ExitWithError(fmt.Errorf("replace block end not found"))
		return nil, replaceBlockStart, replaceBlockEnd
	}

	return lines, replaceBlockStart, replaceBlockEnd
}

// copyGoModReplaceBlockFromArgoCDToArgoCDOperator will look at the replace ( ... ) block from argocd's go.mod, and copy it, as is, into the replace block of argocd-operator go.mod.
// - This code assumes that only 1 replace ( ... ) block exists in both argocd go.mod and argocd-operator go.mod
// - It also assumes that no other non-argo-cd values have been inserted into argocd-operator go.mod
func copyGoModReplaceBlockFromArgoCDToArgoCDOperator(argoCDRepoRoot string, argocdOperatorRoot string, targetArgoCDVersion string) {
	argoCDRepoRootGoModPath := filepath.Join(argoCDRepoRoot, "go.mod")

	replaceBlockFromArgoCDLines, replaceBlockStart, replaceBlockEnd := readReplaceBlockFromGoMod(argoCDRepoRootGoModPath)
	replaceBlockFromArgoCD := replaceBlockFromArgoCDLines[replaceBlockStart : replaceBlockEnd+1]

	argocdOperatorGoModPath := filepath.Join(argocdOperatorRoot, "go.mod")

	argocdOperatorLines, argocdOperatorReplaceStart, argocdOperatorReplaceEnd := readReplaceBlockFromGoMod(argocdOperatorGoModPath)

	newArgoCDOperatorGoModFileContents := ""

	for x, line := range argocdOperatorLines {
		if x == argocdOperatorReplaceStart {

			newArgoCDOperatorGoModFileContents += "\t// This replace block is from Argo CD " + targetArgoCDVersion + " go.mod\n"

			// inject the replace block from argocd's go.mod
			for _, replaceBlockFromArgoCDLine := range replaceBlockFromArgoCD {
				newArgoCDOperatorGoModFileContents += replaceBlockFromArgoCDLine + "\n"
			}

			// argo-cd declares gitops-engine at a pseudo-version where go.mod didn't exist yet, then
			// overrides it with a local replace (=> ./gitops-engine) that only works inside its own
			// monorepo. Downstream consumers must pin it to the actual argo-cd commit instead.
			gitopsEngineLine, err := buildGitopsEngineReplaceLine(argoCDRepoRoot, targetArgoCDVersion)
			if err != nil {
				utils.ExitWithError(fmt.Errorf("unable to build gitops-engine replace line: %v", err))
				return
			}
			newArgoCDOperatorGoModFileContents += gitopsEngineLine + "\n"

		} else if x >= argocdOperatorReplaceStart && x <= argocdOperatorReplaceEnd {
			// skip existing replace block in argocd-operator's go.mod
			continue
		} else {
			newArgoCDOperatorGoModFileContents += line + "\n"
		}
	}
	if err := os.WriteFile(argocdOperatorGoModPath, ([]byte)(newArgoCDOperatorGoModFileContents), 0o600); err != nil {
		utils.ExitWithError(fmt.Errorf("unable to write to file: %s %v", argocdOperatorGoModPath, err))
		return
	}
}

// buildGitopsEngineReplaceLine generates the replace directive that pins
// github.com/argoproj/argo-cd/gitops-engine to the actual argo-cd commit, e.g.:
//
//	// argo-cd v3.4.2 declares gitops-engine at a pseudo-version where go.mod
//	// didn't exist yet, then overrides with replace => ./gitops-engine locally.
//	// Downstream consumers must resolve it themselves; pin to the v3.4.2 commit.
//	github.com/argoproj/argo-cd/gitops-engine => github.com/argoproj/argo-cd/gitops-engine v0.0.0-20260512203152-0dc6b1b57dd5
func buildGitopsEngineReplaceLine(argoCDRepoRoot string, targetArgoCDVersion string) (string, error) {
	// Fetch hash and Unix timestamp in one call. %ct is timezone-agnostic (seconds since epoch),
	// which we then convert to UTC — pseudo-versions must use UTC timestamps.
	out, _, err := utils.RunCommandWithWorkDir(argoCDRepoRoot, "git", "log", "-1", "--format=%H %ct")
	if err != nil {
		return "", fmt.Errorf("unable to get git commit info: %v", err)
	}
	parts := strings.Fields(strings.TrimSpace(out))
	if len(parts) != 2 || len(parts[0]) < 12 {
		return "", fmt.Errorf("unexpected git log output: %q", out)
	}
	hash := parts[0]
	unixSec, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return "", fmt.Errorf("unable to parse commit timestamp %q: %v", parts[1], err)
	}
	ts := time.Unix(unixSec, 0).UTC().Format("20060102150405")
	pseudoVersion := "v0.0.0-" + ts + "-" + hash[:12]

	return fmt.Sprintf(
		"\t// %s declares gitops-engine at a pseudo-version where go.mod\n"+
			"\t// didn't exist yet, then overrides with replace => ./gitops-engine locally.\n"+
			"\t// Downstream consumers must resolve it themselves; pin to the %s commit.\n"+
			"\tgithub.com/argoproj/argo-cd/gitops-engine => github.com/argoproj/argo-cd/gitops-engine %s",
		targetArgoCDVersion, targetArgoCDVersion, pseudoVersion,
	), nil
}

// updateGoModAndRegenBundle ensures that both Go modules are up to date with the target Argo CD
// version, and regenerates the generated assets. argocd-operator is a submodule that carries the
// code, while the parent gitops-operator module owns config/, bundle/ and the vendor tree.
func updateGoModAndRegenBundle(targetArgoCDVersion string, argocdOperatorRoot string, gitopsOperatorRoot string, argoCDRepoRoot string) {
	// Both modules require argo-cd directly, so bump them in lockstep.
	for _, moduleRoot := range []string{argocdOperatorRoot, gitopsOperatorRoot} {
		if err := utils.RunCommandListWithWorkDir(moduleRoot,
			[][]string{
				{"go", "get", "github.com/argoproj/argo-cd/v3@" + targetArgoCDVersion},
			}); err != nil {
			utils.ExitWithError(fmt.Errorf("unable to update go.mod in %s: %v", moduleRoot, err))
			return
		}
	}

	copyGoModReplaceBlockFromArgoCDToArgoCDOperator(argoCDRepoRoot, argocdOperatorRoot, targetArgoCDVersion)

	// The parent module has its own replace block (it imports argo-cd directly too), so keep
	// its gitops-engine pin in sync without overwriting its additional CVE/k8s replaces.
	updateGitopsEngineReplaceLineInGoMod(filepath.Join(gitopsOperatorRoot, "go.mod"), argoCDRepoRoot, targetArgoCDVersion)

	for _, moduleRoot := range []string{argocdOperatorRoot, gitopsOperatorRoot} {
		if err := utils.RunCommandListWithWorkDir(moduleRoot,
			[][]string{
				{"go", "mod", "tidy"},
				{"go", "mod", "vendor"},
			}); err != nil {
			utils.ExitWithError(fmt.Errorf("unable to tidy/vendor go.mod in %s: %v", moduleRoot, err))
			return
		}
	}

	// config/, bundle/ and the generated code live in the parent gitops-operator module.
	err := utils.RunCommandListWithWorkDir(gitopsOperatorRoot,
		[][]string{
			{"rm", "-f", filepath.Join(gitopsOperatorRoot, "bin", "controller-gen")}, // Erase the controller-gen binary to ensure it is re-downloaded to the correct version
			{"make", "generate", "manifests"},
			{"make", "bundle"},
			{"make", "fmt"},
		})
	if err != nil {
		utils.ExitWithError(fmt.Errorf("unable to regenerate manifests/bundle in %s: %v", gitopsOperatorRoot, err))
		return
	}

	if err := utils.RunCommandListWithWorkDir(argocdOperatorRoot, [][]string{{"go", "fmt", "./..."}}); err != nil {
		utils.ExitWithError(fmt.Errorf("unable to run go fmt in %s: %v", argocdOperatorRoot, err))
	}
}

// updateGitopsEngineReplaceLineInGoMod rewrites the gitops-engine replace directive (and the
// version comments around it) in the given go.mod. argo-cd overrides gitops-engine with a local
// replace that does not propagate to downstream modules, so every consumer must pin it to the
// argo-cd release commit.
func updateGitopsEngineReplaceLineInGoMod(goModPath string, argoCDRepoRoot string, targetArgoCDVersion string) {
	replaceBlock, err := buildGitopsEngineReplaceLine(argoCDRepoRoot, targetArgoCDVersion)
	if err != nil {
		utils.ExitWithError(fmt.Errorf("unable to build gitops-engine replace line: %v", err))
		return
	}

	replaceBlockLines := strings.Split(replaceBlock, "\n")
	directive := replaceBlockLines[len(replaceBlockLines)-1]

	lines := utils.GetFileContentsAsLines(goModPath)
	newContent := ""
	matched := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "github.com/argoproj/argo-cd/gitops-engine =>"):
			newContent += directive + "\n"
			matched = true
		case strings.HasPrefix(trimmed, "// This replace block is from Argo CD v"):
			newContent += "\t// This replace block is from Argo CD " + targetArgoCDVersion + " go.mod\n"
		case strings.HasPrefix(trimmed, "// v") && strings.Contains(trimmed, "declares gitops-engine at a pseudo-version"):
			newContent += "\t// " + targetArgoCDVersion + " declares gitops-engine at a pseudo-version where go.mod\n"
		case strings.HasPrefix(trimmed, "// Downstream consumers must resolve it themselves; pin to the v"):
			newContent += "\t// Downstream consumers must resolve it themselves; pin to the " + targetArgoCDVersion + " commit.\n"
		default:
			newContent += line + "\n"
		}
	}

	if !matched {
		utils.ExitWithError(fmt.Errorf("unable to find gitops-engine replace directive in %s", goModPath))
		return
	}

	if err := os.WriteFile(goModPath, []byte(strings.TrimSpace(newContent)+"\n"), 0o600); err != nil {
		utils.ExitWithError(fmt.Errorf("unable to write to file: %s %v", goModPath, err))
	}
}

// replaceLineInCommonDefaultGo updates a line in 'common/defaults.go' beginning with 'variableToReplace' with a different container image digest and version comment
//
// Example:
//   - From:
//     ArgoCDDefaultArgoVersion = "sha256:dc4e00548b9e9fe31b6b2dca99b2278390faabd3610a04f4707dfddf66b5e90d" // v3.1.0-rc2
//   - To:
//     ArgoCDDefaultArgoVersion = "sha256:a36ab0c0860c77159c16e04c7e786e7a282f04889ba9318052f0b8897d6d2040" // v3.1.1
func replaceLineInCommonDefaultGo(pathToArgoCDGitRepoRoot string, variableToReplace string, toReplace processedContainerImage) error {
	path := filepath.Join(pathToArgoCDGitRepoRoot, "common/defaults.go")

	lines := utils.GetFileContentsAsLines(path)

	var res string

	var match bool
	for _, line := range lines {
		if strings.Contains(line, "\t"+variableToReplace+" ") {
			match = true

			res += "\t" + variableToReplace + " = \"" + toReplace.sha256Digest + "\" // " + toReplace.version + "\n"

		} else {
			res += line + "\n"
		}
	}

	if !match {
		return fmt.Errorf("no match found for '%s'", variableToReplace)
	}

	res = strings.TrimSpace(res) + "\n"

	if err := os.WriteFile(path, []byte(res), 0o600); err != nil {
		return err
	}

	return nil
}

// imageTagFromRef returns the tag portion of a container image reference, ignoring any digest.
// A reference without an explicit tag yields an error rather than an implicit "latest", which
// would resolve a digest from an unrelated image.
func imageTagFromRef(ref string) (string, error) {
	if idx := strings.Index(ref, "@"); idx >= 0 {
		ref = ref[:idx]
	}

	// Only treat a ':' after the last '/' as a tag separator, so registry ports are ignored.
	if idx := strings.LastIndex(ref, ":"); idx > strings.LastIndex(ref, "/") {
		if tag := ref[idx+1:]; tag != "" {
			return tag, nil
		}
	}

	return "", fmt.Errorf("unable to determine tag from image reference %q: expected an explicit tag", ref)
}

// retrieveSHA256DigestUsingSkopeo determines the SHA256 digest value for a given container image
func retrieveSHA256DigestUsingSkopeo(url string) (*processedContainerImage, error) {
	stdout, _, err := utils.RunCommandWithWorkDir("", "skopeo", "inspect", "--no-tags", "--override-os", "linux", "--override-arch", "amd64", "docker://"+url)
	if err != nil {
		return nil, fmt.Errorf("unexpected skopeo error: %w", err)
	}

	// Skopeo inspect output looks like this:
	// 	{
	//     "Name": "quay.io/argoproj/argocd",
	//     "Digest": "sha256:a36ab0c0860c77159c16e04c7e786e7a282f04889ba9318052f0b8897d6d2040",
	//     "RepoTags": [],
	//     "Created": "2025-08-25T15:56:23.164856076Z",
	// 	# (...)
	//     "Architecture": "amd64",
	//     "Os": "linux",
	// }

	// Extract Digest value from JSON
	var jsonMap map[string]any
	if err := json.Unmarshal([]byte(stdout), &jsonMap); err != nil {
		return nil, fmt.Errorf("unexpected unmarshal error: %w", err)
	}

	digestVal, ok := jsonMap["Digest"]
	if !ok || digestVal == nil {
		return nil, fmt.Errorf("unable to extract digest val for %s: key missing or nil", url)
	}

	sha256Digest, ok := digestVal.(string)
	if !ok || sha256Digest == "" {
		return nil, fmt.Errorf("unable to extract digest val for %s: not a valid string", url)
	}

	return &processedContainerImage{
		version:      url[strings.Index(url, ":")+1:],
		sha256Digest: sha256Digest,
	}, nil
}

// processedContainerImage is the return values for processedContainerImage
type processedContainerImage struct {
	version      string // version string portion of container image URL, e.g. 'v3.1.1'
	sha256Digest string // 'sha256:(...)' digest value of container image from skopeo. string includes 'sha256:' prefix
}
