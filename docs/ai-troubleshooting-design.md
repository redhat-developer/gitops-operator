# AI-Assisted Troubleshooting — Design

## Overview

The OpenShift GitOps Operator can automatically diagnose failing Argo CD Applications
using the OpenShift Lightspeed agentic framework. When an Application becomes
unhealthy, the operator creates an `AgenticRun` CR in the Lightspeed namespace. The
agent runs AI-powered analysis against live cluster state and writes its findings to
an `AnalysisResult`. The console plugin links directly to the `AgenticRun` page in
the Lightspeed console for users to view the diagnosis and recommendations.

Diagnosis is **advisory only**. The controller creates diagnosis runs and updates
Application annotations to track them; it never executes remediation or changes
the workloads being diagnosed.

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

One shared `gitops-ai-controller` Deployment runs in the operator namespace. Its Pod
configuration is independent of which ArgoCD instance triggers reconciliation. It
does not watch or list Applications: the webhook supplies an Application name and
namespace, and the controller authorizes that target before reading its live state.

## Feature gates

The feature has two independent gates that must both be enabled:

| Gate | Scope | Controls |
|---|---|---|
| `GITOPS_AI_ENABLED=true` on the Subscription | Operator | Shared controller Deployment and Service, controller ServiceAccount, ClusterRole/Binding, and Roles/Bindings for runs and enrollment |
| `spec.ai.troubleshooting: true` on the ArgoCD CR | Per ArgoCD instance | Notifications SA, token Secret, CA ConfigMap, `NotificationsConfiguration` CR in the ArgoCD namespace, and enrollment entry in the operator namespace |

The operator-level gate is checked at startup against the cluster version; the feature
requires OCP 5.0 or later and is silently forced off on older clusters.

## Health policy and incidents

### Qualifying health states

| Health | Qualifies immediately | Waiting or recovery behavior |
|---|---|---|
| `Degraded`, `Unknown` | Yes | — |
| `Missing`, `Progressing` | No | Waits for the diagnosis grace timer to expire |
| `Healthy` | No | Clears pending diagnosis and closes the incident after sustained health |
| `Suspended` | No | Immediately clears pending diagnosis and closes the incident |

The grace period is controlled by `GITOPS_AI_DIAGNOSIS_UNHEALTHY_DURATION` (default
`10m`). All Applications also wait for a creation grace period before any diagnosis
is triggered. Cached grace timers are bound to the Application UID and revision
within the enrolled ArgoCD instance. Recreating an Application with the same name
and revision starts a fresh timer; observing its deletion clears the old timer.

The same duration confirms recovery: an Application must remain `Healthy` for the
configured window before the controller clears its diagnosis timer or closes its
incident. During that window, it creates no diagnosis runs and schedules a recovery
check without needing another webhook. If health becomes poor again, the original
diagnosis countdown continues and the incident retains its retry budget. For
example, an OOM crash loop that briefly reports `Healthy` for one second before
returning to `Progressing` does not restart the diagnosis countdown.

Health timestamp updates alone do not extend an existing diagnosis timer. Recovery
uses a valid `status.health.lastTransitionTime` to determine how long health has
been stable, falling back to the first healthy observation when the timestamp is
missing, invalid, or in the future. These timers are in memory and are lost when
the controller restarts. Setting the duration to `0` removes the diagnosis and
recovery waits. `Suspended` still stops diagnosis immediately.

### Incident lifecycle

An **incident** tracks one degradation episode for an Application. It is persisted
as an annotation on the Application object. The annotation stores:

- A UUID incident ID
- The UID of the enrolled ArgoCD instance that authorized it
- The revision at which the incident was opened
- The deterministic `AgenticRun` name and namespace
- Creation confirmation status and retry budget

An incident permits at most two runs: an initial diagnosis and one retry after a
terminal analysis failure, subject to `GITOPS_AI_DIAGNOSIS_COOLDOWN` (default `10m`).

If the Application loses its incident annotation, recovery preserves the existing
run's incident ID and attempt number and verifies that they match its deterministic
name. Missing, malformed, or conflicting metadata stops diagnosis with a logged
error until corrected; it does not create a new incident or restore the retry budget.

A new incident starts when:

- The Application completed its healthy recovery window and degrades again
- The Application was suspended and becomes unhealthy after resuming
- The Application revision changes while degraded

A new incident does **not** start for brief health flaps within the same revision.

After confirmed `Healthy` recovery, or immediately on `Suspended`, the incident is
closed and the visual annotations (`DiagnosisState`, `AgenticRunName`,
`AgenticRunNamespace`) are removed
from the Application so the console plugin stops surfacing the stale recommendation.
The internal incident annotation is retained so the next degradation episode is
treated as a fresh incident rather than a continuation.

### Run naming

`AgenticRun` names are deterministic: `gitops-diag-<sha256(uid+"/"+incidentID)[:16]>-<attempt>`.
This handles ambiguous create timeouts (the existing run is found by name on retry)
and prevents duplicate runs from duplicate webhook deliveries.

### Skill selection

The operator forwards `GITOPS_AI_SKILLS_IMAGE` and `GITOPS_AI_SKILLS_PATHS` from its
environment to the shared controller. The controller splits the comma-separated
path list, trims whitespace, and ignores empty entries before writing the paths to
`spec.tools.skills[].paths` on each new AgenticRun. Paths identify directories inside
the skills image. A custom list replaces the two default GitOps skill paths; an
unset or empty list uses those defaults. The [configuration reference](ai-assisted-troubleshooting-with-lightspeed.md#configuration-reference)
lists the defaults and shows how to configure the operator Subscription.

## Notifications integration

Argo CD Notifications triggers `POST /api/v1/diagnose` when an Application health
event matches the `on-gitops-ai-diagnose` trigger. The notification key includes the
Application UID, revision, health transition time, and a one-minute delivery epoch.
The epoch permits redelivery after a failed webhook or controller restart without
resetting the incident or run budget.

The operator manages a separate `gitops-ai-controller-notifications`
NotificationsConfiguration in each opted-in ArgoCD namespace. It sets the ArgoCD
controller owner reference and management label, recreates the CR when deleted,
and restores its generated spec after manual edits. This is reconciliation-based
enforcement: Kubernetes accepts authorized edits before the operator reverts them.
The `default-notifications-configuration` CR remains the user's configuration.

The notifications controller merges the default and managed CRs into
`argocd-notifications-cm`. Managed values win conflicts in triggers, templates,
services, and context. The managed CR also reserves its subscription recipients:
the merge removes those recipients from default subscriptions before adding the
managed subscriptions, preserving other recipients and their selectors. For the
managed webhook, service definitions and recipients are compared by destination
service name, so aliases such as `gitops-ai-controller:any` cannot override its
subscriptions. Neither source CR is modified by the merge. Removing the managed
CR removes stale AI keys
and restores any default values previously overridden. An unrelated same-named CR
without the management metadata is not merged as operator configuration.

The `gitops-ai-controller-notifications` ServiceAccount token is used as the bearer
token for all notification-originated requests. The operator creates one such SA in
every AI-enabled ArgoCD namespace, so a single shared controller serves several
instances. The controller validates the token via TokenReview and then resolves the
verified SA namespace to the ArgoCD instance that sent the request.

The notification identity receives no RBAC permissions from this feature. Kubernetes
populates its `gitops-ai-controller-notifications-token` Secret, of type
`kubernetes.io/service-account-token`, with a signed token. The operator copies that
token into `argocd-notifications-secret` under the key `gitops-ai-controller-token`
for the notification service's `Authorization: Bearer <token>` header.

With TLS enabled, the operator creates `gitops-ai-controller-serving-ca` with
`service.beta.openshift.io/inject-cabundle: "true"`. OpenShift injects its service CA
bundle, which the operator copies into `argocd-tls-certs-cm` under the shared
controller's service hostname so notifications can verify the HTTPS endpoint.
The controller watches its mounted serving certificate and key, reloading valid
replacements without a restart. If a replacement is temporarily incomplete or
invalid, it retains the last valid certificate until a valid pair is available.

After completing notification setup, the operator records the ArgoCD UID and
notifications ServiceAccount UID in the `gitops-ai-controller-instances` ConfigMap
in the operator namespace. The controller has read-only access to this registry.
A matching ServiceAccount name or an AI-enabled ArgoCD CR alone does not enroll an
instance. The operator's configured ArgoCD label selector also applies to requests.

## HTTP API

Requests require a bearer token whose TokenReview identity is
`system:serviceaccount:<argocd-namespace>:gitops-ai-controller-notifications`, where
`<argocd-namespace>` holds exactly one operator-managed ArgoCD instance with
`spec.ai.troubleshooting: true`. The namespace comes from the verified token, never
from the request body — `applicationNamespace` names the Application to diagnose and
is authorized separately against the resolved instance.

Both the live ArgoCD UID and the TokenReview ServiceAccount UID must match the
operator's enrollment record. Queued work and AgenticRuns retain the originating
ArgoCD UID. The worker rechecks enrollment, instance identity, and AI opt-in when
processing delayed diagnoses and retries. Run events are checked the same way.

TokenReview runs for each webhook without an explicit audience, using the API
server's default audience for the current ServiceAccount token Secret. Missing or
invalid tokens return 401. A valid token whose ServiceAccount or instance fails the
authorization checks returns 403. Temporary API failures return 500 and do not
authorize the request.

Application authorization currently accepts only the verified instance's own
namespace. Applications in additional `spec.sourceNamespaces` are refused; the
worker reads the requested Application after admission to check its existence and
current state.

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

The shared controller's Deployment, Service, ServiceAccount, and RBAC resources
carry `gitops.openshift.io/managed-by: gitops-ai-controller`. Their reconciler
refuses to update resources without that label and checks ownership before deleting
each resource. The enrollment ConfigMap uses the same management label.

The operator watches its managed Deployment, Service, ServiceAccount, and RBAC
resources. Changes and deletions enqueue a shared reconciliation, so deleting a
supporting resource triggers recreation while AI is enabled and an ArgoCD instance
exists. Events are filtered by resource type, name, namespace, and management label.
The shared reconciliation does not depend on the instance that originally created
the resources; temporary API failures are retried. The ArgoCD reconciler separately
watches the enrollment ConfigMap to rebuild missing enrollment records.

| Scope | Resources |
|---|---|
| Operator namespace (for example, `openshift-gitops-operator`) | `gitops-ai-controller` Deployment, Service, and ServiceAccount; `gitops-ai-controller-instances` ConfigMap, Role, and RoleBinding |
| Cluster | `gitops-ai-controller` ClusterRole and ClusterRoleBinding |
| AgenticRun namespace (default: `openshift-lightspeed`) | `gitops-ai-controller` Role and RoleBinding |
| Each opted-in ArgoCD namespace | `gitops-ai-controller-notifications` ServiceAccount and NotificationsConfiguration; `gitops-ai-controller-notifications-token` Secret; `gitops-ai-controller-serving-ca` ConfigMap when TLS is enabled |

### Controller permissions

All Kubernetes API calls from the shared controller use its
`gitops-ai-controller` ServiceAccount in the operator namespace. The ClusterRoleBinding
and both namespaced RoleBindings bind their respective roles to that identity.

The `gitops-ai-controller` **ClusterRole** grants:

| API group | Resource | Verbs | Purpose |
|---|---|---|---|
| `argoproj.io` | `applications` | `get`, `patch` | Read Application state and write incident and visual annotations |
| `argoproj.io` | `argocds` | `get`, `list` | Resolve the caller's namespace to its ArgoCD instance and read its AI opt-in |
| `authentication.k8s.io` | `tokenreviews` | `create` | Validate notification bearer tokens |

These permissions are cluster-wide. The operator's label selector and enrollment
checks restrict which instances may request diagnosis in controller code; the
ClusterRole itself does not enforce those restrictions.

The `gitops-ai-controller` **Role in the AgenticRun namespace**, configured with
`GITOPS_AI_AGENTIC_RUN_NAMESPACE`, grants:

| API group | Resource | Verbs | Purpose |
|---|---|---|---|
| `agentic.openshift.io` | `agenticruns` | `create`, `get`, `list`, `watch` | Create and monitor diagnosis runs |
| `agentic.openshift.io` | `analysisresults` | `get` | Permit reading analysis results produced by Lightspeed |

The `gitops-ai-controller-instances` **Role in the operator namespace** grants
only `get` on the ConfigMap named `gitops-ai-controller-instances`, using
`resourceNames` to restrict access. It cannot write enrollments or read other
ConfigMaps through this Role. Its distinct name avoids a collision with the run
Role when the operator and AgenticRun namespaces are the same.

### Enrollment ownership

The ArgoCD reconciler writes enrollment after enforcing the operator's management
selector and completing notification setup. Each namespace's entry binds the
ArgoCD name and UID to the notifications ServiceAccount UID. A ConfigMap in a tenant
namespace cannot grant access. Cleanup revokes only an entry belonging to the same
ArgoCD UID and preserves entries for other instances.

## Disablement

Setting `spec.ai.troubleshooting: false` removes the AI resources owned by that
ArgoCD instance, notification entries, the injected token, and the service CA entry. Existing
`AgenticRun` and `AnalysisResult` objects in Lightspeed are not deleted. Other
notification subscriptions, credentials, and certificates are preserved. Its
enrollment entry is revoked, and subsequent worker checks stop queued diagnoses and
retries for the disabled instance. Deleting or replacing the ArgoCD instance also
invalidates its queued work and prevents its runs being adopted by another instance.
Cleanup checks management metadata and the owning ArgoCD UID and uses deletion
preconditions so unrelated objects and replacement resources are preserved.

Setting `GITOPS_AI_ENABLED` to absent or any value other than `"true"` on the
Subscription removes the `gitops-ai-controller` Deployment and its supporting RBAC
and Service. Notification reconciliation removes each instance's enrollment entry;
the registry ConfigMap may remain empty. Disabling does not cancel a run already
submitted to Lightspeed.

## Planned authorization improvements

The current implementation checks instance enrollment, identity, and AI opt-in at
request admission and on each worker pass. Further work includes:

- **Applications in Any Namespace:** support `spec.sourceNamespaces` patterns with
  explicit Application ownership checks. Applications outside the instance's own
  namespace remain refused until that authorization is implemented.
- **Admission checks:** verify Application existence and ownership before queueing,
  and enforce applicable AppProject restrictions. Application state is currently
  read by the worker after the request has been accepted.
- **Short-lived tokens:** replace the long-lived ServiceAccount token Secret with
  projected tokens and use a dedicated audience on both the token and TokenReview.
- **Namespace-scoped Application permissions:** replace cluster-wide Application
  access with Roles and RoleBindings in the namespaces authorized for each instance,
  maintained alongside its namespace configuration. TokenReview access would still
  require a cluster-scoped permission.
