# Repository AI Guidelines

Before adding to this file, carefully consider that poorly written AGENTS.md files can hurt task success rates, while also increasing token usage.

## Dependencies

Keep the dependencies minimal and trusted to reduce the security risks associated with them.

Avoid depending on `github.com/argoproj-labs/argocd-operator/`, its sources have been effectively forked and made part of this repository, so external dependency is undesirable.

## E2E Testing Guidelines (`test/openshift/e2e/ginkgo/**/*`)

When creating or modifying Ginkgo E2E test files:

### General Guidelines

- **Avoid `k8sClient.Update(...)`**: This call will occasionally fail due to Kubernetes resources being modified between the Get and Update calls.
  - Instead, use fixture-specific `Update` functions. Most fixture packages include an `Update` function, for example for `ArgoCD` CR: use `argocdFixture.Update(...)`. For generic resource update, see `k8sFixture.Update(...)`.
- **Avoid explicit waiting**: Always prefer `gomega.Eventually`, or `gomega.Consistently`.

### Parallel Tests (`test/openshift/e2e/ginkgo/parallel/*_test.go`)

Prefer test implemented as parallel, if they can be written as such.

- Tests that run within this folder run in parallel against a single K8s cluster. Thus, these tests cannot step on each others toes by, for example, modifying cluster resources that are shared by other parallel tests. (It is fine to READ from shared resources, but NOT to write to them).
  - A non-exhaustive list of tests that should remain in sequential:
    - Tests that rely on managed-by label.
    - Tests that log in to an Argo CD instance via argocd cli.
    - Tests that use ArgoCD CR `.spec.sourceNamespaces`.
    - Tests that modify environment variables of `openshift-gitops-operator-controller-manager`.
    - Thats that explicitly verifies behavior of the default instance in `openshift-gitops` namespace
- Use `Describe("GitOps Operator Parallel E2E Tests", func() { ... })` as the top-level describe block
- Call `fixture.EnsureParallelCleanSlate()` in the `BeforeEach` function. (not `EnsureSequentialCleanSlate`)

### Sequential Tests (`test/openshift/e2e/ginkgo/sequential/*_test.go`)

- Use `Describe("GitOps Operator Sequential E2E Tests", func() { ... })` as the top-level describe block
- Call `fixture.EnsureSequentialCleanSlate()` in the `BeforeEach` function. (not `EnsureParallelCleanSlate`)
- Strive for speeding up the tests where possible to save the time needed for execution.
