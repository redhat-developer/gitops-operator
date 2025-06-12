# Gitops-operator E2E Tests

gitops-operator E2E tests are defined within the `test/openshift/e2e/ginkgo` (as of this writing).

These tests are written with the Ginkgo/Gomega test framework, and were ported from previous Kuttl tests.

## Running tests

### A) Run tests against OpenShift GitOps installed via OLM

The E2E tests can be run from the `Makefile` at the root of the gitops-operator repository.

```bash
# Run Sequential tests
make e2e-tests-sequential-ginkgo
# You can add 'SKIP_HA_TESTS=true' if you are on a cluster with <3 nodes
# Example: 'SKIP_HA_TESTS=true  make e2e-tests-sequential-ginkgo'

# Run Parallel tests (up to 5 tests will run at a time)
make e2e-tests-parallel-ginkgo
# As above, can add SKIP_HA_TESTS, if necessary.
```

### B) Run E2E tests against local operator (operator running via `make run`)

```bash
# 1) Start operator locally
make run 

# 2) Start tests in LOCAL_RUN mode (this skips tests that require Subscription or CSVs)
LOCAL_RUN=true  make e2e-tests-sequential-ginkgo
# and/or
LOCAL_RUN=true  make e2e-tests-parallel-ginkgo
# Not all tests are supported when running locally. See 'Skip' messages for details.
```

### C) Run a specific test:

```bash
# 'make ginkgo' to download ginkgo, if needed
# Examples:
./bin/ginkgo -v -focus "1-106_validate_argocd_metrics_controller"  -r ./test/openshift/e2e/ginkgo/sequential
./bin/ginkgo -v -focus "1-099_validate_server_autoscale"  -r ./test/openshift/e2e/ginkgo/parallel
```

## Configuring which tests run

Not all tests support all configurations:
* For example, if you are running gitops-operator via `make run`, this blocks any tests that require changes to `Subscription`. 
* Thus, when running locally, you can set `LOCAL_RUN=true` to skip those unsupported tests.

There are a few environment variables that can be set to configure which tests run. 


### If you are running the gitops-operator via `make run` from your local machine

Some tests require the gitops-operator to be running on cluster (and/or installed via OLM). 

BUT, this is not true when you are running the operator on your local machine during the development process.

You can skip non-local-supported tests by setting `LOCAL_RUN=true`:
```bash
LOCAL_RUN=true  make e2e-tests-sequential-ginkgo
# and/or
LOCAL_RUN=true  make e2e-tests-sequential-parallel
```


### If you are running tests on a cluster with < 3 nodes:

Tests that verify operator HA (e.g. Redis HA) behaviour require a cluster with at least 3 nodes. If you are running on a cluster with less than 3 nodes, you can skip these tests by setting `SKIP_HA_TESTS=true`:
```bash
SKIP_HA_TESTS=true  make e2e-tests-sequential-ginkgo
```

### If you are testing a gitops-operator install that is running on K8s cluster, but that was NOT installed via Subscription (OLM)

In some cases, you may want to run the gitops-operator tests against an install of OpenShift GitOps that was NOT installed via OLM, but IS running on cluster. For example, via a plain `Deployment` in the gitops operator Namepsace.

For this, you may use the `NON_OLM` env var:
```bash
NON_OLM=true make e2e-tests-sequential-ginkgo
```

Note: If `LOCAL_RUN` is set, you do not need to set `NON_OLM` (it is assumed).


### You can specify multiple test env vars at the same time.

For example, if you are running operator via `make run`, on a non-HA cluster (<3 nodes):
```bash
SKIP_HA_TESTS=true LOCAL_RUN=true  make e2e-tests-sequential-ginkgo
```



## Test Code

gitops-operator E2E tests are defined within `test/openshift/e2e/ginkgo`.

These tests are written with the [Ginkgo/Gomega test frameworks](https://github.com/onsi/ginkgo), and were ported from previous Kuttl tests.

### Tests are currently grouped as follows:
- `sequential`: Tests that are not safe to run in parallel with other tests.
    - A test is NOT safe to run in parallel with other tests if:
        - It modifies resources in `openshift-gitops`
        - It modifies the GitOps operator `Subscription`
        - It modifies cluster-scoped resources, such as `ClusterRoles`/`ClusterRoleBindings`, or `Namespaces` that are shared between tests
        - More generally, if it writes to a K8s resource that is used by another test.
- `parallel`: Tests that are safe to run in parallel with other tests
    - A test is safe to run in paralel if it does not have any of the above problematic behaviours. 
    - It is fine for a parallel test to read cluster-scoped resources (such as resources in openshift-gitops namespace)
    - A parallel test should NEVER write to resources that may be shared with other tests (Subscriptions, some cluster-scoped resources, etc.)



### Test fixture:
- Utility functions for writing tests can be found within the `fixture/` folder.
- `fixture/fixture.go` contains utility functions that are generally useful to writing tests.
- `fixture/(name of resource)` contains functions that are specific to working with a particular resource.
    - For example, if you wanted to wait for an `Application` CR to be Synced/Healthy, you would use the functions defined in `fixture/application`.
    - Likewise, if you want to check a `Deployment`, see `fixture/deployment`.
    - Fixtures exist for nearly all interesting resources
- The goal of this test fixture is to make it easy to write tests, and to ensure it is easy to understand and maintain existing tests.
- See existing k8s tests for usage examples.

## Tips for debugging tests

### If you are debugging tests in CI
- If you are debugging a test failure, considering adding a call to the `fixture.OutputDebugOnFail()` function at the end of the test.
- `OutputDebugOnFail` will output helpful information when a test fails (such as namespace contents and operator pod logs)
- See existing test code for examples.


### If you are debugging tests locally
- Consider setting the `E2E_DEBUG_SKIP_CLEANUP` variable when debugging tests locally.
- The `E2E_DEBUG_SKIP_CLEANUP` environment variable will skip cleanup at the end of the test. 
    - The default E2E test behaviour is to clean up test resources at the end of the test. 
    - This is good when tests are succeeding, but when they are failing it can be helpful to look at the state of those K8s resources at the time of failure.
    - Those old tests resources WILL still be cleaned up when you next start the test again.
- This will allow you to `kubectl get` the test resource to see why the test failed. 

Example:
```bash
E2E_DEBUG_SKIP_CLEANUP=true ./bin/ginkgo -v -focus "1-099_validate_server_autoscale"  -r ./test/openshift/e2e/ginkgo/parallel
```
