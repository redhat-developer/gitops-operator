package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"os/exec"

	"github.com/google/go-github/v58/github"
)

// These can be set while debugging
const (
	// if readOnly is true:
	// - PRs will not be opened
	// - Git commits will not be pushed to fork
	// This is roughly equivalent to a dry run
	readOnly = false // default to false

	PRTitlePrefix = "Update to latest commit of argo-rollouts-manager "
)

func main() {

	pathToGitOpsOperatorGitRepo := "gitops-operator"

	// Checkout argo-rollouts-manager repo into a temporary directory
	pathToRolloutsManagerGitRepo, err := checkoutRolloutsManagerRepoIntoTempDir()
	if err != nil {
		exitWithError(err)
		return
	}

	// Get latest commit ID from argo-rollouts-manager
	stdout, _, err := runCommandWithWorkDir(pathToRolloutsManagerGitRepo, "git", "log", "--format=%H")
	if err != nil {
		exitWithError(err)
		return
	}
	commitIds := strings.Split(stdout, "\n")
	if len(commitIds) == 0 {
		exitWithError(fmt.Errorf("unable to retrieve commit ids"))
	}

	mostRecentCommitID := commitIds[0]

	newBranchName := "upgrade-rollouts-manager"

	// Create, commit, and push a new branch
	if repoAlreadyUpToDate, err := createNewCommitAndBranch(mostRecentCommitID, newBranchName, pathToRolloutsManagerGitRepo, pathToGitOpsOperatorGitRepo); err != nil {

		if repoAlreadyUpToDate {
			fmt.Println("* Exiting as target repository is already up to date.")
			return
		}

		exitWithError(err)
		return
	}

	if !readOnly {

		var existingPRID *int

		{

			gitHubToken := os.Getenv("GH_TOKEN")
			if gitHubToken == "" {
				exitWithError(fmt.Errorf("missing GH_TOKEN"))
				return
			}

			client := github.NewClient(nil).WithAuthToken(gitHubToken)

			// 1) Check for existing version update PRs on the repo

			prList, _, err := client.PullRequests.List(context.Background(), "redhat-developer", "gitops-operator", &github.PullRequestListOptions{})
			if err != nil {
				exitWithError(err)
				return
			}
			for _, pr := range prList {
				if strings.HasPrefix(*pr.Title, PRTitlePrefix) {
					existingPRID = (*pr).Number
				}
			}
		}

		PRTitle := PRTitlePrefix + "'" + mostRecentCommitID + "'"

		bodyText := `

Update to most recent 'argo-rollouts-manager' commit: https://github.com/argoproj-labs/argo-rollouts-manager/commit/` + mostRecentCommitID

		if existingPRID != nil {
			//  Update PR title/body if it already exists
			if stdout, stderr, err := runCommandWithWorkDir(pathToGitOpsOperatorGitRepo, "gh", "pr", "edit", strconv.Itoa(*existingPRID),
				"-R", "redhat-developer/gitops-operator",
				"--title", PRTitle, "--body", bodyText); err != nil {
				fmt.Println(stdout, stderr)
				exitWithError(err)
				return
			}

		} else {
			//  Create PR if it doesn't exist
			if stdout, stderr, err := runCommandWithWorkDir(pathToGitOpsOperatorGitRepo, "gh", "pr", "create",
				"-R", "redhat-developer/gitops-operator",
				"--title", PRTitle, "--body", bodyText); err != nil {
				fmt.Println(stdout, stderr)
				exitWithError(err)
				return
			}

		}

	}

}

// return true if the argo-rollouts-manager repo is already up to date
func createNewCommitAndBranch(latestRolloutsManagerCommitId string, newBranchName, pathToArgoRolloutsManagerRepo string, pathToGitOpsOperatorGitRepo string) (bool, error) {

	commands := [][]string{
		{"git", "stash"},
		{"git", "fetch", "parent"},
		{"git", "checkout", "master"},
		{"git", "reset", "--hard", "parent/master"},
		{"git", "checkout", "-b", newBranchName},
	}

	if err := runCommandListWithWorkDir(pathToGitOpsOperatorGitRepo, commands); err != nil {
		return false, err
	}

	if goModGitCommit, err := extractCurrentRolloutsManagerGitCommitFromGoMod(pathToGitOpsOperatorGitRepo); err != nil {
		return false, fmt.Errorf("unable to extract current target version from repo")

	} else if strings.Contains(latestRolloutsManagerCommitId, goModGitCommit) {
		return false, fmt.Errorf("gitops-operator is already on the target git commit")
	}

	if err := regenerateGoMod(latestRolloutsManagerCommitId, pathToGitOpsOperatorGitRepo); err != nil {
		return false, err
	}

	if err := regenerateE2ETestScript(latestRolloutsManagerCommitId, pathToGitOpsOperatorGitRepo); err != nil {
		return false, err
	}

	if err := copyCRDsFromRolloutsManagerRepo(pathToArgoRolloutsManagerRepo, pathToGitOpsOperatorGitRepo); err != nil {
		return false, fmt.Errorf("unable to copy rollouts CRDs: %w", err)
	}

	commands = [][]string{
		{"go", "mod", "tidy"},
		{"make", "generate", "manifests"},
		{"make", "bundle"},
		{"make", "fmt"},
		{"git", "add", "--all"},
		{"git", "commit", "-s", "-m", PRTitlePrefix + "'" + latestRolloutsManagerCommitId + "'"},
	}
	if err := runCommandListWithWorkDir(pathToGitOpsOperatorGitRepo, commands); err != nil {
		return false, err
	}

	if !readOnly {
		commands = [][]string{
			{"git", "push", "-f", "--set-upstream", "origin", newBranchName},
		}
		if err := runCommandListWithWorkDir(pathToGitOpsOperatorGitRepo, commands); err != nil {
			return false, err
		}
	}

	return false, nil

}

func copyCRDsFromRolloutsManagerRepo(pathToRolloutsManagerGitRepo string, pathToGitRepo string) error {
	rolloutManagerCRDPath := filepath.Join(pathToRolloutsManagerGitRepo, "config", "crd", "bases")

	crdYamlDirEntries, err := os.ReadDir(rolloutManagerCRDPath)
	if err != nil {
		return err
	}

	var crdYAMLs []string
	for _, crdYamlDirEntry := range crdYamlDirEntries {

		if !crdYamlDirEntry.IsDir() {
			crdYAMLs = append(crdYAMLs, crdYamlDirEntry.Name())
		}
	}

	sort.Strings(crdYAMLs)

	// NOTE: If this line fails, check if any new CRDs have been added to Rollouts, and/or if they have changed the filenames.
	// - If so, this will require verifying the changes, then updating this list
	if !reflect.DeepEqual(crdYAMLs, []string{
		"analysis-run-crd.yaml",
		"analysis-template-crd.yaml",
		"argoproj.io_rolloutmanagers.yaml",
		"cluster-analysis-template-crd.yaml",
		"experiment-crd.yaml",
		"rollout-crd.yaml"}) {
		return fmt.Errorf("unexpected CRDs found: %v", crdYAMLs)
	}

	destinationPath := filepath.Join(pathToGitRepo, "config/crd/bases")
	for _, crdYAML := range crdYAMLs {

		// #nosec G304
		destFile, err := os.Create(filepath.Join(destinationPath, crdYAML))
		if err != nil {
			return fmt.Errorf("unable to create file for '%s': %w", crdYAML, err)
		}
		defer destFile.Close()

		// #nosec G304
		srcFile, err := os.Open(filepath.Join(rolloutManagerCRDPath, crdYAML))
		if err != nil {
			return fmt.Errorf("unable to open source file for '%s': %w", crdYAML, err)
		}
		defer srcFile.Close()

		_, err = io.Copy(destFile, srcFile)
		if err != nil {
			return fmt.Errorf("unable to copy file for '%s': %w", crdYAML, err)
		}

	}

	return nil
}

func regenerateE2ETestScript(commitID string, pathToGitRepo string) error {

	envName := "TARGET_ROLLOUT_MANAGER_COMMIT"
	// Format of string to modify:
	// TARGET_ROLLOUT_MANAGER_COMMIT=(commit id)

	path := filepath.Join(pathToGitRepo, "scripts/run-rollouts-e2e-tests.sh")

	// #nosec G304
	fileBytes, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var res string

	for _, line := range strings.Split(string(fileBytes), "\n") {

		if strings.HasPrefix(line, envName+"=") {

			res += envName + "=" + commitID + "\n"

		} else {
			res += line + "\n"
		}
	}

	// Trim trailing space
	res = strings.TrimSpace(res)

	if err := os.WriteFile(path, []byte(res), 0600); err != nil {
		return err
	}

	return nil

}

func checkoutRolloutsManagerRepoIntoTempDir() (string, error) {

	tmpDir, err := os.MkdirTemp("", "argo-rollouts-manager-src")
	if err != nil {
		return "", err
	}

	if _, _, err := runCommandWithWorkDir(tmpDir, "git", "clone", "https://github.com/argoproj-labs/argo-rollouts-manager"); err != nil {
		return "", err
	}

	newWorkDir := filepath.Join(tmpDir, "argo-rollouts-manager")

	return newWorkDir, nil
}

// extractCurrentRolloutsManagerGitCommitFromGoMod read the contents of the argo-rollouts-manager repo and determine which argo-rollouts version is being targeted.
func extractCurrentRolloutsManagerGitCommitFromGoMod(pathToGitOpsOperatorGitRepo string) (string, error) {

	// Style of text string to parse:
	// github.com/argoproj-labs/argo-rollouts-manager v0.0.2-0.20240221054348-027faa92ffdb

	path := filepath.Join(pathToGitOpsOperatorGitRepo, "go.mod")

	// #nosec G304
	fileBytes, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	for _, line := range strings.Split(string(fileBytes), "\n") {
		if strings.Contains(line, "github.com/argoproj-labs/argo-rollouts-manager v") {

			indexOfLastHyphen := strings.LastIndex(line, "-")
			if indexOfLastHyphen != -1 {
				return strings.TrimSpace(line[indexOfLastHyphen+1:]), nil
			}

		}
	}

	return "", fmt.Errorf("no version found in gitops-operator go.mod")
}

func regenerateGoMod(commitId string, pathToGitRepo string) error {

	if err := runCommandListWithWorkDir(pathToGitRepo, [][]string{
		{"go", "get", "github.com/argoproj-labs/argo-rollouts-manager@" + commitId},
		{"go", "mod", "tidy"}}); err != nil {
		return err
	}

	return nil

}

func runCommandListWithWorkDir(workingDir string, commands [][]string) error {

	for _, command := range commands {

		_, _, err := runCommandWithWorkDir(workingDir, command...)
		if err != nil {
			return err
		}
	}
	return nil
}

func runCommandWithWorkDir(workingDir string, cmdList ...string) (string, string, error) {

	fmt.Println(cmdList)

	// #nosec G204
	cmd := exec.Command(cmdList[0], cmdList[1:]...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Dir = workingDir
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	stdoutStr := stdout.String()
	stderrStr := stderr.String()

	fmt.Println(stdoutStr, stderrStr)

	return stdoutStr, stderrStr, err

}

func exitWithError(err error) {
	fmt.Println("ERROR:", err)
	os.Exit(1)
}
