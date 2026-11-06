# update-dependencies-script

This is a simple go-based script that will upgrade the upstream dependencies in argocd-operator.

## To run this script:

In `(root)/Makefile`, modify `ARGO_CD_TARGET_VERSION` to target Argo CD version.

Example:
```
ARGO_CD_TARGET_VERSION ?= 3.1.8
```

Then run the script:
```
make update-dependencies
```

See each dependency subdirectory's `main.go` for the list of files it updates.

## Environment overrides

- `GITOPS_PROMOTER_VERSION`: GitOps Promoter release from which CRDs are copied. It must match
  the version pinned in `argocd-operator/go.mod`; the updater fails otherwise, since the copied
  CRDs have to match the vendored controller.
- `REDIS_MIRROR_IMAGE`: image repository used to resolve the Redis digest. Defaults to
  `quay.io/akuity/redis`. If the digest cannot be resolved (for example, the mirror does not have
  the tag yet), the Redis digests are left unchanged and the rest of the update continues.
