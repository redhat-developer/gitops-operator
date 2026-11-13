# AI Troubleshooting — RBAC Reference

This document describes all Kubernetes RBAC resources created for the AI troubleshooting feature, from two perspectives: the **gitops-ai-controller** (the webhook receiver and AgenticRun manager) and the **ArgoCD instance** (the notification sender).

---

## gitops-ai-controller perspective

All resources are provisioned by the GitOps operator reconciler when AI features are enabled.

### ServiceAccount

| Field | Value |
|---|---|
| Name | `gitops-ai-controller` |
| Namespace | operator namespace (e.g. `openshift-gitops-operator`) |

The Deployment's pod identity. All Kubernetes API calls made by the controller run as this SA.

### ClusterRole — `gitops-ai-controller`

| API group | Resource | Verbs | Purpose |
|---|---|---|---|
| `argoproj.io` | `applications` | `get`, `patch` | Read application state; write incident and visual annotations |
| `authentication.k8s.io` | `tokenreviews` | `create` | Validate the bearer token sent by ArgoCD Notifications on each webhook call |

### ClusterRoleBinding — `gitops-ai-controller`

Binds the ClusterRole to the `gitops-ai-controller` ServiceAccount in the operator namespace. Cluster-scoped so the controller can read and patch Applications across all ArgoCD-managed namespaces.

### Role — `gitops-ai-controller` (in the AgenticRun namespace)

Scoped to the namespace where AgenticRuns live (default: `openshift-lightspeed`).

| API group | Resource | Verbs | Purpose |
|---|---|---|---|
| `agentic.openshift.io` | `agenticruns` | `create`, `get`, `list`, `watch` | Create and monitor AgenticRun objects |
| `agentic.openshift.io` | `analysisresults` | `get` | Read analysis results produced by Lightspeed |

### RoleBinding — `gitops-ai-controller` (in the AgenticRun namespace)

Binds the Role above to the `gitops-ai-controller` ServiceAccount in the operator namespace.

---

## ArgoCD instance perspective

Resources are provisioned by the ArgoCD operator reconciler in the ArgoCD namespace (e.g. `openshift-gitops`) when the instance opts in via `spec.ai.troubleshooting: true`.

### ServiceAccount — `gitops-ai-controller-notifications`

A dedicated, permission-less identity used exclusively by the ArgoCD Notifications controller to call the AI controller webhook. It holds no RBAC permissions of its own — its only purpose is to provide a verifiable identity token.

### Secret — `gitops-ai-controller-notifications-token`

A `kubernetes.io/service-account-token` Secret bound to the `gitops-ai-controller-notifications` SA. Kubernetes populates it with a signed token that ArgoCD Notifications attaches as the `Authorization: Bearer <token>` header on each webhook call.

The token is also copied into `argocd-notifications-secret` under the key `gitops-ai-controller-token` so the ArgoCD Notifications service can read it.

### ConfigMap — `gitops-ai-controller-serving-ca` (TLS only)

Created with the annotation `service.beta.openshift.io/inject-cabundle: "true"`. OpenShift automatically injects the cluster service CA bundle so ArgoCD Notifications can verify the controller's TLS certificate when calling the webhook over HTTPS.

---

## Webhook authentication flow

```
ArgoCD Notifications
  │  Bearer <notifications-SA-token>
  ▼
gitops-ai-controller (POST /api/v1/diagnose)
  │  TokenReview (no audience)
  ▼
kube-apiserver
  │  Authenticated: system:serviceaccount:<argocd-ns>:gitops-ai-controller-notifications
  ▼
gitops-ai-controller
  └─ Authorized: username matches expected SA → accept request
```

The controller creates a `TokenReview` for every incoming request and rejects any token that does not resolve to the exact SA `system:serviceaccount:<argocd-namespace>:gitops-ai-controller-notifications`. No additional RBAC is granted to that SA — it cannot read or write any cluster resource.

---

## Planned enhancements

### Webhook authorization hardening

The current check verifies the caller's identity (correct SA in the correct namespace) but does not validate request content against cluster state. The following checks are planned:

- **`spec.ai.troubleshooting` gate at request time** — read the ArgoCD CR at webhook time and reject the request if the instance has disabled AI troubleshooting. Today the flag is only checked at reconcile time, so a notification in-flight after the flag is turned off is still accepted.
- **Application-to-instance namespace binding** — verify that the `applicationNamespace` in the webhook body is a namespace managed by the ArgoCD instance associated with the authenticated SA. This prevents a caller from triggering diagnosis for Applications outside their instance's scope.
- **Application existence and ownership check** — confirm the Application named in the webhook exists in the claimed namespace before enqueuing, so a compromised caller cannot cause the controller to work on arbitrary names.

### Projected tokens with a dedicated audience

The notifications SA currently uses a long-lived `kubernetes.io/service-account-token` Secret and the `TokenReview` is performed without an explicit audience, which means Kubernetes validates it against the API server audience.

The planned improvement is to replace the long-lived Secret with a short-lived projected `ServiceAccountToken` volume (e.g. 1-hour expiry) and add a dedicated audience (e.g. `gitops-ai-controller`) to both the token projection and the `TokenReview` call. This limits the blast radius if a token is exposed: it expires quickly and is not accepted by any other endpoint.

### Namespace-scoped Application RBAC

The controller currently holds a `ClusterRole` with `get` and `patch` on `argoproj.io/applications` so it can write incident and visual annotations to Applications in any namespace. This is broader than necessary.

The planned improvement is to replace the ClusterRole/ClusterRoleBinding with per-namespace `Role` and `RoleBinding` resources, one pair per namespace managed by the ArgoCD instance. The operator reconciler would create and delete these bindings in sync with the ArgoCD instance's `spec.sourceNamespaces` and `spec.applicationSet.sourceNamespaces` configuration, granting the controller access only to the namespaces it legitimately manages.
