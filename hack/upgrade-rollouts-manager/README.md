# Update gitops-operator to latest release of argo-rollouts-manager

The Go code and script in this directory will automatically open a pull request to update the gitops-operator to the latest commit of argo-rollouts-manager:
- Update `go.mod` to point to latest module version
- Update CRDs to latest
- Regenerate manifests and bundles
- (Currently disabled): Update target Rollouts version in `hack/run-upstream-argo-rollouts-e2e-tests.sh`
- Open Pull Request using 'gh' CLI

## Instructions

### Prerequisites
- GitHub CLI (_gh_) installed and on PATH
- Go installed and on PATH
- You must have your own fork of the [argo-rollouts-manager](https://github.com/argoproj-labs/argo-rollouts-manager) repository in GitHub(e.g. `jgwest/argo-rollouts-manager`)
- Your local SSH key registered (e.g. `~/.ssh/id_rsa.pub`) with GitHub to allow git clone via SSH

### Configure and run the tool

```bash

# Set required Environment Variables
export GITHUB_FORK_USERNAME="(your username here)"
export GH_TOKEN="(a GitHub personal access token that can clone/push/open PRs against argo-rollouts-manager repo)"

# or, you can set these values in the settings.env file:
#   cp settings_template.env settings.env
# Then set env vars in settings.env (which is excluded in the .gitignore)

./init-repo.sh
./go-run.sh
```
