### Non-OLM operator: Ginkgo E2E tests

When the operator is installed without OLM (for example via a plain `Deployment` in `openshift-gitops-operator`), run the OpenShift E2E suite with `NON_OLM=true` so tests that require a `Subscription` or productized images are skipped.

From the repository root:

```bash
NON_OLM=true make e2e-tests-sequential-ginkgo
# and/or
NON_OLM=true make e2e-tests-parallel-ginkgo
```

See `test/openshift/e2e/README.md` for full options (`LOCAL_RUN`, `SKIP_HA_TESTS`, and so on).
