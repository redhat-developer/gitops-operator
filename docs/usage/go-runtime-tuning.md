# Go Runtime Tuning

The GitOps Operator automatically tunes the Go runtime of the Argo CD Application Controller based on the resource limits configured in the Argo CD Custom Resource. When enabled, the operator computes and injects the `GOMEMLIMIT` and `GOMAXPROCS` environment variables into the Application Controller StatefulSet so that the Go garbage collector and scheduler stay aligned with the container's CPU and memory limits.

Tuning is enabled by default for all Argo CD instances that define resource limits on the Application Controller.

## How the values are computed

- **`GOMEMLIMIT`** is derived from the Application Controller memory limit (`.spec.controller.resources.limits.memory`), multiplied by the configured `memoryLimitPercent` (default `80`). The remaining headroom is reserved for non-heap memory and garbage collection overhead, reducing the risk of the container being OOM-killed.
- **`GOMAXPROCS`** is derived from the CPU limit (`.spec.controller.resources.limits.cpu`) at 100% of the limit, rounded down to whole cores. For CPU limits below one core (for example `250m`), the value is floored at `1` because `GOMAXPROCS=0` is invalid.

If a resource limit is not set, the corresponding environment variable is not injected.

## Configuring tuning per instance

The `.spec.controller.goRuntime` field controls tuning for a single Argo CD instance:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: ArgoCD
metadata:
  name: example
spec:
  controller:
    resources:
      limits:
        cpu: '2'
        memory: 2Gi
    goRuntime:
      enabled: true
      memoryLimitPercent: 75
```

| Field | Default | Description |
| --- | --- | --- |
| `enabled` | `true` | Toggles runtime tuning for this instance. Setting it to `false` opts the instance out. |
| `memoryLimitPercent` | `80` | Percentage (1–100) of the memory limit used to compute `GOMEMLIMIT`. |

For example, to disable tuning for an instance:

```sh
kubectl -n argocd patch argocd example --type='merge' -p='{"spec": {"controller": {"goRuntime": {"enabled": false}}}}'
```

For example, to set the memory limit percentage to 75 for an instance:

```sh
kubectl -n argocd patch argocd example --type='merge' -p='{"spec": {"controller": {"goRuntime": {"memoryLimitPercent": 75}}}}'
```

## Disabling tuning cluster-wide

Cluster administrators can disable Go runtime tuning for all Argo CD instances by setting the `DISABLE_GO_RUNTIME_TUNING` environment variable to `"true"` in the operator Subscription. This is a global kill switch: per-instance `.spec.controller.goRuntime` settings cannot re-enable tuning while it is set.

## Precedence rules

Environment variables defined by the user in `.spec.controller.env` always take precedence over the operator-computed values. For example, to set an explicit `GOMEMLIMIT`:

```yaml
spec:
  controller:
    env:
      - name: GOMEMLIMIT
        value: "1610612736"
```

!!! note
    Because user-defined environment variables override the computed values, the operator will not reconcile changes to resource limits into manually-set `GOMEMLIMIT` or `GOMAXPROCS` values. Remove the manual entry to return to automatic tuning.
