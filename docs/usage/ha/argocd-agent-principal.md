# High Availability for Argo CD Agent Principal

The GitOps Operator can configure active/passive High Availability for the
[Argo CD Agent][argocd_agent] principal through
`.spec.argoCDAgent.principal.ha` on the `ArgoCD` Custom Resource.

Two principal instances run in separate clusters. One starts as the preferred
`primary` (ACTIVE) and the other as a `replica` (SYNCING → REPLICATING).
Promotion and demotion are performed with `argocd-agentctl ha`; principals do
not promote themselves.

A replica authenticates to its peer for replication with a dedicated client
certificate (issued by `argocd-agentctl pki issue principal-peer`, stored in
`argocd-agent-ha-peer-tls` by default). This is separate from the principal's
server certificate. The operator mounts that secret and sets
`ARGOCD_PRINCIPAL_HA_PEER_TLS_CERT` / `ARGOCD_PRINCIPAL_HA_PEER_TLS_KEY`. The
connecting identity (`CN=argocd-agent-principal-peer`) must appear in the peer's
`allowedReplicationClients`.

!!! note
    Principal HA & Replication is currently in Beta upstream. See the
    [argocd-agent HA documentation][argocd_agent_ha] for concepts, DNS/GSLB
    health checks, and failover procedures.

## Prerequisites

- Two clusters, each with the GitOps Operator and an Argo CD instance
- A shared CA so both principals can verify each other's client certificates
- A dedicated replication client certificate on each principal
  (`argocd-agentctl pki issue principal-peer`)
- A DNS/GSLB endpoint (or manual DNS) for agents to reach the active principal

## Configure the preferred primary

```yaml
apiVersion: argoproj.io/v1beta1
kind: ArgoCD
metadata:
  name: example-argocd
spec:
  controller:
    enabled: false
  argoCDAgent:
    principal:
      enabled: true
      auth: "mtls:CN=([^,]+)"
      ha:
        enabled: true
        preferredRole: primary
        peerAddress: "principal.region-b.example.com:443"
        peerTLSSecretName: argocd-agent-ha-peer-tls
        allowedReplicationClients:
          - "argocd-agent-principal-peer"
        adminPort: 8405
        adminAuth: "mtls:subject:CN=ha-admin"
```

## Configure the preferred replica

```yaml
apiVersion: argoproj.io/v1beta1
kind: ArgoCD
metadata:
  name: example-argocd
spec:
  controller:
    enabled: false
  argoCDAgent:
    principal:
      enabled: true
      auth: "mtls:CN=([^,]+)"
      ha:
        enabled: true
        preferredRole: replica
        peerAddress: "principal.region-a.example.com:443"
        peerTLSSecretName: argocd-agent-ha-peer-tls
        allowedReplicationClients:
          - "argocd-agent-principal-peer"
        adminPort: 8405
        adminAuth: "mtls:subject:CN=ha-admin"
```

!!! important
    `peerAddress` is required when `preferredRole` is `replica`.
    When TLS is active, set `adminAuth`; otherwise HA admin calls are denied.
    Client certificates for `argocd-agentctl ha` are supplied on the CLI
    (`--tls-cert` / `--tls-key` / `--tls-ca`), not by the operator.
    Issue the replication client certificate with
    `argocd-agentctl pki issue principal-peer` before enabling HA, or mount the
    secret optionally so the pod can start first.

## Configuration reference

| Field | Description |
|-------|-------------|
| `enabled` | Enable active/passive HA (default: `false`) |
| `preferredRole` | Startup role: `primary` or `replica` (default: `primary`) |
| `peerAddress` | Peer gRPC address (`host:port`). Required for `replica` |
| `peerTLSSecretName` | TLS secret with the dedicated replication client cert (`tls.crt`, `tls.key`). Default: `argocd-agent-ha-peer-tls` |
| `allowedReplicationClients` | mTLS identities allowed for replication. Empty allows any authenticated peer. Use `argocd-agent-principal-peer` for the dedicated client cert |
| `adminPort` | HAAdmin gRPC port (default: `8405`) |
| `adminAuth` | Auth policy for the HA admin endpoint (for example `mtls:subject:CN=ha-admin`) |
| `replicationInitialAckTimeout` | How long the primary waits for the replica's initial snapshot acknowledgement (default: `5m`) |

The operator maps these fields to `ARGOCD_PRINCIPAL_HA_*` environment variables,
mounts the peer TLS secret at `/app/config/ha-peer-tls`, and opens the
configured `adminPort` (default `8405`) on the principal pod.
The operator does not create a Service for HA admin. You can create one
manually; use the same `adminPort` value for the Service `port`/`targetPort`
and for `argocd-agentctl --address`:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: example-argocd-agent-principal-ha-admin
  namespace: <namespace>
spec:
  selector:
    app.kubernetes.io/name: example-argocd-agent-principal
  ports:
    - name: ha-admin
      port: 8405
      targetPort: 8405
```

## Verify HA status

```bash
argocd-agentctl ha status \
  --address example-argocd-agent-principal-ha-admin.<namespace>.svc:8405 \
  --tls-cert=<admin-client.crt> \
  --tls-key=<admin-client.key> \
  --tls-ca=<ca.crt>
```

Expected: primary is `active`, replica is `replicating`.

[argocd_agent]: https://argocd-agent.readthedocs.io/latest/
[argocd_agent_ha]: https://argocd-agent.readthedocs.io/latest/configuration/ha/
