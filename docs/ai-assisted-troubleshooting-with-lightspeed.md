# AI-Assisted Troubleshooting with OpenShift Lightspeed

OpenShift GitOps can automatically diagnose failing Argo CD Applications using the
OpenShift Lightspeed agentic operator. When an Application becomes unhealthy, the
operator triggers an `AgenticRun` that runs AI-powered diagnosis against the live
cluster and reports its findings.

## Prerequisites

- OpenShift 5.0 or later (the AI feature is not available on earlier versions)
- Cluster-admin access
- The OpenShift GitOps Operator installed
- The Lightspeed agentic operator installed and configured (see the Lightspeed configuration guide)

## 1. Enable the AI feature in the GitOps Operator

The feature is off by default. Enable it by setting `GITOPS_AI_ENABLED=true` in the
operator Subscription:

A merge patch replaces `spec.config.env` wholesale, which would drop any proxy
settings or image overrides already configured on the Subscription. Add the
variable to the existing list instead:

```sh
oc -n openshift-gitops-operator get subscription gitops-operator -o json \
  | jq '(.spec.config.env // []) as $env
        | .spec.config.env = (($env | map(select(.name != "GITOPS_AI_ENABLED")))
            + [{"name": "GITOPS_AI_ENABLED", "value": "true"}])' \
  | oc apply -f -
```

Or set it directly in the Subscription manifest:

```yaml
spec:
  config:
    env:
      - name: GITOPS_AI_ENABLED
        value: 'true'
```

This deploys the `gitops-ai-controller` and its supporting RBAC into the operator
namespace. The ArgoCD-namespace resources are not created until step 2.

## 2. Enable AI troubleshooting on the ArgoCD instance

```sh
oc -n openshift-gitops patch argocd openshift-gitops --type=merge \
  -p '{"spec":{"ai":{"troubleshooting":true}}}'
```

Or set it directly in the ArgoCD manifest:

```yaml
spec:
  ai:
    troubleshooting: true
```

This creates the notifications service account, token secret, CA ConfigMap, and
`NotificationsConfiguration` CR in the ArgoCD namespace, completing the setup.

The operator owns the `gitops-ai-controller-notifications` NotificationsConfiguration.
Manual configuration edits are automatically reverted, and deletion triggers
recreation while AI is enabled. Keep your custom notifications in
`default-notifications-configuration`.
The operator merges both configurations into `argocd-notifications-cm`, giving AI
values precedence on conflicting keys without changing the default CR. Subscriptions
to the AI webhook are controlled by the operator; other recipients are preserved.
Disabling AI removes its configuration and restores the default CR's settings.

## 3. Verify the installation

```sh
# gitops-ai-controller should be running in the operator namespace
oc -n openshift-gitops-operator get deployment gitops-ai-controller

# Notifications SA and token secret should exist in the ArgoCD namespace
oc -n openshift-gitops get sa gitops-ai-controller-notifications
oc -n openshift-gitops get secret gitops-ai-controller-notifications-token

# Notifications configuration CR should be present
oc -n openshift-gitops get notificationsconfiguration gitops-ai-controller-notifications

# Watch for AgenticRuns when an Application fails
oc -n openshift-lightspeed get agenticruns.agentic.openshift.io -w

# Tail controller logs
oc -n openshift-gitops-operator logs deployment/gitops-ai-controller --tail=100

# Tail operator logs
oc -n openshift-gitops-operator logs deployment/openshift-gitops-operator-controller-manager \
  -c manager --tail=100
```

## Configuration reference

All settings below are env vars configured via `spec.config.env` on the operator
`Subscription`. The operator forwards relevant values to the `gitops-ai-controller` pod.

| Env var | Default | Description |
|---|---|---|
| `GITOPS_AI_ENABLED` | _(not set — feature disabled)_ | Set to `true` to enable the AI feature |
| `GITOPS_AI_CONTROLLER_IMAGE` | `quay.io/redhat-developer/gitops-ai-controller:v<operator-version>` | Image for the gitops-ai-controller deployment |
| `GITOPS_AI_SKILLS_IMAGE` | `ghcr.io/redhat-developer/agentic-skills:latest` | Image containing the diagnosis skill scripts |
| `GITOPS_AI_SKILLS_PATHS` | Two GitOps skill paths listed below | Comma-separated paths within the skills image; a custom list replaces the defaults |
| `GITOPS_AI_AGENTIC_RUN_NAMESPACE` | `openshift-lightspeed` | Namespace where `AgenticRun` CRs are created |
| `GITOPS_AI_DIAGNOSIS_COOLDOWN` | `10m` | Minimum time between diagnoses for the same Application |
| `GITOPS_AI_DIAGNOSIS_UNHEALTHY_DURATION` | `10m` | Diagnosis grace period and sustained-Healthy recovery window; `0` removes both waits |
| `GITOPS_AI_CONTROLLER_TLS_ENABLED` | `true` | Whether the gitops-ai-controller uses TLS |
| `GITOPS_AI_DIAGNOSIS_MAX_ATTEMPTS` | `2` | Maximum diagnosis attempts per incident |

The default controller image follows the operator release version. The project's
release CI publishes the matching tag. Set
`GITOPS_AI_CONTROLLER_IMAGE` to use another tag or a digest.

An Application must remain `Healthy` for the recovery window before its pending
diagnosis timer is cleared or its incident is closed. Brief Healthy intervals,
such as a container starting for one second between OOM restarts, preserve the
original countdown and retry budget. Diagnosis resumes when the Application is
unhealthy again; no new run starts while it is Healthy. Recovery checks are
scheduled automatically. `Suspended` stops diagnosis immediately.

`GITOPS_AI_SKILLS_PATHS` selects directories inside `GITOPS_AI_SKILLS_IMAGE`.
It is optional; leave it unset to use the default paths:

- `/skills/gitops/argo-cluster-debug`
- `/skills/gitops/argo-knowledge`

For example, select one path while preserving the Subscription's other environment
settings:

```sh
oc -n openshift-gitops-operator get subscription gitops-operator -o json \
  | jq --arg paths '/skills/gitops/argo-cluster-debug' \
      '(.spec.config.env // []) as $env
       | .spec.config.env = (($env | map(select(.name != "GITOPS_AI_SKILLS_PATHS")))
           + [{"name": "GITOPS_AI_SKILLS_PATHS", "value": $paths}])' \
  | oc apply -f -
```

The controller trims whitespace around each path and ignores empty comma-separated
entries. An unset variable or a list with no nonempty paths uses the defaults.
Custom paths replace the entire default list and must exist in the selected skills
image. They are written to `spec.tools.skills[].paths` on new diagnosis AgenticRuns.

Per-ArgoCD-instance setting (on the `ArgoCD` CR):

| Field | Default | Description |
|---|---|---|
| `spec.ai.troubleshooting` | `false` | Enables AI troubleshooting for this ArgoCD instance |

