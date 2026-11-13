# AI-Assisted Troubleshooting — Design

## Overview

The OpenShift GitOps Operator can automatically diagnose failing Argo CD Applications
using the OpenShift Lightspeed agentic framework. When an Application becomes
unhealthy, the operator creates an `AgenticRun` CR in the Lightspeed namespace. The
agent runs AI-powered analysis against live cluster state and writes its findings to
an `AnalysisResult`. The console plugin links directly to the `AgenticRun` page in
the Lightspeed console for users to view the diagnosis and recommendations.

Diagnosis is **advisory only**. The controller never modifies cluster resources
and never executes remediation.

## Architecture

```
ArgoCD Notifications
       │  POST /api/v1/diagnose
       ▼
gitops-ai-controller (operator namespace)
       │  creates
       ▼
AgenticRun (openshift-lightspeed)
       │  produces
       ▼
AnalysisResult (openshift-lightspeed)
       │  linked by console plugin
       ▼
User (Lightspeed console AgenticRun page)
```

The `gitops-ai-controller` runs as a sidecar in the operator namespace. It does not
watch or list Applications. All Application state used to build an `AgenticRun` is
read from the live API at the moment of creation, using trusted service configuration
rather than notification payload fields.

## Feature gates

The feature has two independent gates that must both be enabled:

| Gate | Scope | Controls |
|---|---|---|
| `GITOPS_AI_ENABLED=true` on the Subscription | Operator | `gitops-ai-controller` Deployment, ClusterRole/Binding, Role/Binding in `openshift-lightspeed` |
| `spec.ai.troubleshooting: true` on the ArgoCD CR | Per ArgoCD instance | Notifications SA, token Secret, CA ConfigMap, `NotificationsConfiguration` CR in the ArgoCD namespace |

The operator-level gate is checked at startup against the cluster version; the feature
requires OCP 5.0 or later and is silently forced off on older clusters.

## Health policy and incidents

### Qualifying health states

| Health | Qualifies immediately | Requires grace period |
|---|---|---|
| `Degraded`, `Unknown` | Yes | — |
| `Missing`, `Progressing` | No | `lastTransitionTime` must be ≥ grace period old |
| `Healthy`, `Suspended` | — | Closes any open incident |

The grace period is controlled by `GITOPS_AI_DIAGNOSIS_UNHEALTHY_DURATION` (default
`10m`). All Applications also wait for a creation grace period before any diagnosis
is triggered.

### Incident lifecycle

An **incident** tracks one degradation episode for an Application. It is persisted
as an annotation on the Application object. The annotation stores:

- A UUID incident ID
- The revision at which the incident was opened
- The deterministic `AgenticRun` name and namespace
- Creation confirmation status and retry budget

An incident permits at most two runs: an initial diagnosis and one retry after a
terminal analysis failure, subject to `GITOPS_AI_DIAGNOSIS_COOLDOWN` (default `10m`).

A new incident starts when:
- The Application was previously healthy (recovery observed) and degrades again
- The Application revision changes while degraded

A new incident does **not** start for health flaps within the same revision.

On recovery (`Healthy` or `Suspended`), the incident is closed and the visual
annotations (`DiagnosisState`, `AgenticRunName`, `AgenticRunNamespace`) are removed
from the Application so the console plugin stops surfacing the stale recommendation.
The internal incident annotation is retained so the next degradation episode is
treated as a fresh incident rather than a continuation.

### Run naming

`AgenticRun` names are deterministic: `gitops-diag-<sha256(uid+"/"+incidentID)[:16]>-<attempt>`.
This handles ambiguous create timeouts (the existing run is found by name on retry)
and prevents duplicate runs from duplicate webhook deliveries.

## Notifications integration

Argo CD Notifications triggers `POST /api/v1/diagnose` when an Application health
event matches the `on-gitops-ai-diagnose` trigger. The notification key includes the
Application UID, revision, health transition time, and a one-minute delivery epoch.
The epoch permits redelivery after a failed webhook or controller restart without
resetting the incident or run budget.

The `gitops-ai-controller-notifications` ServiceAccount token is used as the bearer
token for all notification-originated requests. The controller validates it via
TokenReview against the ArgoCD namespace.

## HTTP API

Requests require a bearer token whose TokenReview identity is exactly
`system:serviceaccount:<argocd-namespace>:gitops-ai-controller-notifications`.

### POST `/api/v1/diagnose`

Queues an Application for diagnosis. Returns 202 immediately; diagnosis is
asynchronous.

Request:
```json
{"application": "my-app", "applicationNamespace": "openshift-gitops"}
```

Response (202):
```json
{"accepted": true, "reason": "diagnosis queued"}
```

### Viewing results

The console plugin links directly to the `AgenticRun` page in the Lightspeed
console. Users follow that link to view the diagnosis and recommendations.

## Resource ownership and RBAC

Resources managed by this feature carry the label
`gitops.openshift.io/managed-by: gitops-ai-controller`. The reconciler rejects
existing resources without that label rather than adopting them.

**Operator namespace** (gated by `GITOPS_AI_ENABLED`):
- `gitops-ai-controller` Deployment and Service
- `gitops-ai-controller` ServiceAccount, ClusterRole, ClusterRoleBinding
- Role and RoleBinding in `openshift-lightspeed`

**ArgoCD namespace** (gated by `spec.ai.troubleshooting`):
- `gitops-ai-controller-notifications` ServiceAccount and token Secret
- `gitops-ai-controller-serving-ca` ConfigMap (OpenShift CA injection)
- `gitops-ai-controller-notifications` NotificationsConfiguration CR

The controller requires:
- `get, patch` on `argoproj.io/applications`
- `create` on `authentication.k8s.io/tokenreviews`
- `create, get, list, watch` on `agentic.openshift.io/agenticruns` in the run namespace

## Disablement

Setting `spec.ai.troubleshooting: false` removes all ArgoCD-namespace resources,
notification entries, the injected token, and the service CA entry. Existing
`AgenticRun` and `AnalysisResult` objects in Lightspeed are not deleted. Other
notification subscriptions, credentials, and certificates are preserved.

Setting `GITOPS_AI_ENABLED` to absent or any value other than `"true"` on the
Subscription removes the `gitops-ai-controller` Deployment and all operator-namespace
resources. Disabling does not cancel a run already submitted to Lightspeed.
