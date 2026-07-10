# gitops-operator-tests-ext

OpenShift Tests Extension (OTE) binary for GitOps Operator parallel E2E tests.

It registers tests from `test/openshift/e2e/ginkgo/parallel` and exposes the standard OTE commands (`list`, `run-test`, `run-suite`, `update`, etc.). This mirrors the parallel portion of the CI [`gitops-operator-tests`](https://github.com/openshift/release/tree/main/ci-operator/step-registry/gitops-operator/tests) step.

## Build

From the repository root:

```bash
make gitops-operator-tests-ext
```

Or from this directory:

```bash
GO_COMPLIANCE_POLICY=exempt_all go build -o gitops-operator-tests-ext .
```

The binary is written to `bin/gitops-operator-tests-ext` when built via `make`.

For local development against an unpublished extension, add a replace before building:

```bash
go mod edit -replace github.com/openshift-eng/openshift-tests-extension=/path/to/openshift-tests-extension
```

## Prerequisites

- Logged-in `oc` access to a cluster
- `openshift-gitops-operator` installed
- Cluster prep from `scripts/openshift-CI-kuttl-tests.sh` (creates `test-argocd` and waits for Argo CD pods)

## Usage

```bash
./gitops-operator-tests-ext info
./gitops-operator-tests-ext list

./gitops-operator-tests-ext run-suite openshift/gitops-operator/parallel \
  --junit-path /tmp/openshift-gitops-parallel-e2e.xml

./gitops-operator-tests-ext run-test '<test name from list>'
```

After adding or renaming tests, refresh metadata from the repository root:

```bash
./gitops-operator-tests-ext update
```
