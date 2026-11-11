# Agent Self-Registration

## Overview

By default, onboarding an Argo CD Agent requires creating a cluster secret on the principal with `argocd-agentctl agent create`. Self-registration automates that step: when enabled, the principal creates the cluster secret the first time an agent connects with valid credentials.

Agents still need valid authentication credentials (for example, an mTLS client certificate). Self-registration only removes the manual `argocd-agentctl agent create` step.

## Create the shared client certificate

Self-registered cluster secrets use a shared TLS client certificate. The Secret must contain `tls.crt`, `tls.key`, and `ca.crt`, and the certificate must be trusted by the resource proxy CA.

Issue one with `argocd-agentctl`:

```bash
argocd-agentctl pki issue shared-client \
  --principal-context <control-plane-context> \
  --principal-namespace <argocd-namespace> \
  --upsert
```

## Enable self-registration

Here is an example to enable self-registration:

```yaml
apiVersion: argoproj.io/v1beta1
kind: ArgoCD
metadata:
  name: openshift-gitops
spec:
  argoCDAgent:
    principal:
      enabled: true
      selfRegistration:
        enabled: true
        clientCertSecretName: argocd-agent-shared-client-cert
        clusterSecretLabels:
          env: production
          team: platform
```

| Field | Description |
|-------|-------------|
| `enabled` | Enables automatic cluster secret creation when an authenticated agent connects. Defaults to disabled. |
| `clientCertSecretName` | Name of the shared client certificate Secret (`tls.crt`, `tls.key`, `ca.crt`). Required when enabled. |
| `clusterSecretLabels` | Optional labels applied to cluster Secrets created for self-registered agents. Useful for organizing or selecting those Secrets (for example, `env: production`). |

Now deploy agents as usual, but skip `argocd-agentctl agent create`. The principal creates the cluster secret on first connection.

## Verification

List self-registered cluster secrets:

```bash
kubectl get secrets -n <argocd-namespace> \
  -l argocd-agent.argoproj-labs.io/self-registered-cluster=true
```

You should see a `cluster-<agent-name>` Secret for each self-registered agent.

Check principal logs for registration messages such as:

```text
INFO Creating self-registered cluster secret for agent  agent=my-agent
INFO Successfully created self-registered cluster secret with shared client cert and bearer token
```

## Behavior notes

- **Manual secrets take precedence.** Secrets created with `argocd-agentctl agent create` are not modified or replaced. You can mix manual and self-registered agents.
- **Token refresh.** Self-registered secrets include a JWT bearer token for the resource proxy. If the JWT signing key is rotated, the principal refreshes the token on the next agent connection.
- **Labels.** Self-registered secrets are always labeled `argocd-agent.argoproj-labs.io/self-registered-cluster: "true"`. Additional labels from `clusterSecretLabels` are applied on creation when configured.

## Security considerations

- Self-registration does not bypass authentication. Only agents with valid credentials can connect and register.
- All self-registered agents share one client certificate for resource proxy mTLS; agent identity comes from the JWT bearer token. Protect that Secret with RBAC.
- Any authenticated agent can register when the feature is enabled. Limit who can obtain trusted credentials.
