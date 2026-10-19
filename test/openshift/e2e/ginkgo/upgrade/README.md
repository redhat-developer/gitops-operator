# Upgrade test

The other suites test a clean installation. This one tests an installation that is in use:

1. install the operator from `PREVIOUS_CHANNEL`
2. apply the configuration in [`config/`](config), which turns on as many features as it can
3. check that every feature works
4. upgrade the operator to `NEW_CHANNEL`
5. run the same checks again, and check that
   - every value in the `.spec` of every object in `config/` is still there, unchanged
     (fields the new version adds are printed, not failed)
   - the pods of the deployed applications were not restarted

## Running it

You need to be logged in as a cluster admin to an OpenShift cluster that has at least three
nodes, **does not have the GitOps Operator installed**, and has a CatalogSource that
contains both channels.

```bash
PREVIOUS_CHANNEL=gitops-1.21 NEW_CHANNEL=gitops-1.22 make e2e-tests-upgrade-ginkgo
```

| Variable | | |
|---|---|---|
| `PREVIOUS_CHANNEL` | required | channel to install from |
| `NEW_CHANNEL` | required | channel to upgrade to |
| `PREVIOUS_VERSION` | optional | install exactly this version, for example `1.21.3`, instead of the head of the channel |
| `NEW_VERSION` | optional | stop the upgrade at exactly this version instead of the head of the channel |
| `CATALOG_SOURCE` | optional | CatalogSource in `openshift-marketplace` to install from; default `redhat-operators` |

The Subscription is named after the package, `openshift-gitops-operator`, unless `SUBSCRIPTION_NAME` is set. Set it when other suites run on the cluster afterwards: in CI mode (`CI` set) the e2e fixture finds the Subscription by the prefix `gitops-operator-`.

The Subscription uses manual approval and the test approves each InstallPlan itself. That is
what makes the two `*_VERSION` variables work.

The test does not clean up, so that a failed cluster can be looked at. To run it again on
the same cluster, remove what it created:

```bash
oc delete rollout,rolloutmanager --all -n upgrade-rollouts
# Applications first, while the Argo CD instance is still there to run their finalizers.
oc delete applicationset,application --all -n upgrade-argocd
oc delete appproject,argocd --all -n upgrade-argocd
oc delete namespace upgrade-rollouts upgrade-app upgrade-appset upgrade-argocd
oc delete subscription,csv,operatorgroup --all -n openshift-gitops-operator
oc delete namespace openshift-gitops-operator
# The default instance can only go once the operator is gone, or it is recreated; and with
# the operator gone nothing removes its finalizer.
oc patch argocd openshift-gitops -n openshift-gitops --type merge -p '{"metadata":{"finalizers":null}}'
oc delete namespace openshift-gitops
```

This is good enough for a throwaway cluster. It leaves the operator's CRDs and the default
instance's cluster-scoped RBAC behind.

## What is configured

| Feature | Where | How it is checked |
|---|---|---|
| Customised default instance (memory limit) | `01-default-instance.yaml` | limit is on the application controller StatefulSet |
| Second Argo CD instance | `03-argocd.yaml` | `Available`, every component `Running` |
| Redis HA | `03-argocd.yaml` | three ready Redis and three ready HAProxy replicas |
| ApplicationSet controller, notifications, Dex SSO | `03-argocd.yaml` | Deployments are ready |
| Server Route | `03-argocd.yaml` | Route is admitted |
| RBAC policy, kustomize build options, extra config, custom health check | `03-argocd.yaml` | values are in `argocd-cm` and `argocd-rbac-cm` |
| Cluster-scoped instance (`ARGOCD_CLUSTER_CONFIG_NAMESPACES`) | Subscription, in `olm_test.go` | ClusterRole and ClusterRoleBinding exist |
| Managed namespaces | `02-namespaces.yaml` | Role and RoleBinding exist in each |
| AppProject and Application | `04-applications.yaml` | `Synced` and `Healthy` |
| ApplicationSet | `04-applications.yaml` | generated Application is `Synced` and `Healthy` |
| Argo Rollouts outside `openshift-gitops` (`CLUSTER_SCOPED_ARGO_ROLLOUTS_NAMESPACES`) | `05-rollouts.yaml` and the Subscription | RolloutManager `Available`, Rollout `Healthy` |

## Adding a feature

Add it to a file in `config/` (or add a file; they are applied in name order), then add a
check for it to `verifyFeatures` in `features_test.go`. The check runs before and after the
upgrade without any further change.

Everything in `config/` has to be valid for the oldest operator version the test is run
against.
