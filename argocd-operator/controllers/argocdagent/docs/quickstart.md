# Argo CD Agent Setup Guide

## Overview

This guide provides step-by-step instructions for setting up and configuring the Argo CD Agent in a dev environment.

## Prerequisites
- The `argocd-agentctl` CLI tool installed.
- `oc` CLI tool installed and configured. ([OpenShift CLI](https://docs.openshift.com/container-platform/latest/cli_reference/openshift_cli/getting-started-cli.html))
- `openssl` command line tool installed. ([OpenSSL Library](https://openssl-library.org/source/))

## Steps for Principal Setup

### Step 1: Create Required Namespaces

Create a namespace for the principal in the control plane cluster.
```bash
oc create ns argocd
```
Create namespaces for agents in the control plane cluster. This should be same as the [agent namespace](https://github.com/argoproj-labs/argocd-agent/blob/main/install/kubernetes/agent/agent-params-cm.yaml#L50) in workload clusters.

```bash
oc create ns agent-autonomous
oc create ns agent-managed
```

### Step 2: Start the Operator

Install the required CRDs and start the Argo CD operator in development mode.

```bash
make install
export ARGOCD_CLUSTER_CONFIG_NAMESPACES="argocd"
make run
```

### Step 3: Deploy Argo CD Instance with Principal

Use the yaml content below to create an Argo CD instance in the `argocd` namespace.

```yaml
apiVersion: argoproj.io/v1beta1
kind: ArgoCD
metadata:
  name: argocd
spec:
  controller:
    enabled: false
  argoCDAgent:
    principal:
      enabled: true
      server:
        auth: "mtls:CN=([^,]+)"
        logLevel: "trace"
      namespace:
        allowedNamespaces:
          - "*"
      tls:
        insecureGenerate: true   
      jwt:
        insecureGenerate: true     
  sourceNamespaces:
    - "agent-managed"
    - "agent-autonomous"
```

### Step 4: Generate Agent Configurations

Run the agent configuration script to set up the necessary cluster secrets and other configurations.

```bash
./controllers/argocdagent/scripts/create-agent-config.sh
```
Use `--recreate` flag to recreate configurations. 

Now restart principal to pick up the new configurations. 

```bash
oc rollout -n argocd restart deployment argocd-agent-principal
```

### Step 5: Create Principal Metrics Route

A Service `argocd-agent-principal-metrics` will be created to expose principal metrics. You can create a Route to expose this Service.


### Step 6: Set up Workload Clusters

Follow this [doc](https://github.com/argoproj-labs/argocd-agent/blob/main/docs/getting-started/openshift/index.md#setting-up-agent-workload-cluster) to set up workload clusters.

While installing managed agent you need to provide host names of principal's redis and repo server in [argocd-cmd-params-cm](https://github.com/argoproj-labs/argocd-agent/blob/main/hack/dev-env/agent-managed/argocd-cmd-params-cm.yaml#L6) ConfigMap to allow it to communicate with control plan's redis server.

```bash
oc get svc argocd-redis -n argocd
oc get svc argocd-repo-server -n argocd
```

### Step 7: Issue Agent Certificates

For this demo, we are assuming that the `Managed agent` will be deployed in the `agent-managed` namespace with the `vcluster-agent-managed` context and the `Autonomous agent` in the `argocd` namespace with the `vcluster-agent-autonomous` context.

Now you need to issue agent certificates.

```bash
# For managed agent
argocd-agentctl pki issue agent agent-managed --agent-context vcluster-agent-managed --agent-namespace agent-managed --upsert

# For autonomous agent
argocd-agentctl pki issue agent agent-autonomous --agent-context vcluster-agent-autonomous --agent-namespace argocd --upsert
```

### Step 8: Start Agents in Workload Clusters

Now you can connect agents to principal.

Use the yaml content below to create an Argo CD instance in the workload cluster `argocd` namespace.

```yaml
kind: ArgoCD
metadata:
  name: argocd
spec:
  server:
    enabled: false
  argoCDAgent:
    agent:
      enabled: true
      client:
        principalServerAddress: "_REPLACE_ME_"
        principalServerPort: "443"
        logLevel: "trace"
        mode: "managed" # autonomous
        creds: "mtls:any"
      tls:
        insecure: true
```

### Step 9: Verification

After completing the setup, verify the installation by:

1. Check in pod logs:
When an agent is connected with the principal, you will see a log message something like this:
```
level=info msg="An agent connected to the subscription stream" client=agent-managed method=Subscribe
```

2. Verify the number of connected agents through the `agent_connected_with_principal` metric at the metrics endpoint

## Using SPIFFE/SPIRE for Authentication (Optional)

As an alternative to the mTLS/JWT setup described above, the principal and agent can authenticate
using SPIFFE identities issued by a SPIRE deployment, via the [SPIFFE CSI driver](https://github.com/spiffe/spiffe-csi).
When `spiffe` is configured, it takes precedence over any legacy `tls` and `creds` configuration.
However, `principal.auth` is **not** overridden by SPIFFE. A non-empty `principal.auth` always takes
precedence over the SPIFFE-inferred pattern. Leave it unset to let the controller infer the correct
pattern automatically, or set it to a value that matches your SPIFFE identity format.

### Prerequisites

- A SPIRE deployment (agent + server) reachable from the cluster(s) running the principal and agents,
  with the [SPIFFE CSI driver](https://github.com/spiffe/spiffe-csi) installed so workloads can access
  the SPIRE Agent's Workload API socket.
- SPIFFE IDs issued to the principal and agent workloads that match the expected SVID format, e.g.
  `spiffe://<trust-domain>/ns/<namespace>/argocd/agent/<agent-name>` for the default `jwt` auth method.

### Configuration

Add a `spiffe` block to the `principal` and `agent` sections of the `ArgoCD` CR:

```yaml
spec:
  argoCDAgent:
    principal:
      enabled: true
      # Only needed if your SPIRE setup uses a different SVID format than the default.
      auth: "spiffe-jwt:spiffe://<trust-domain>/ns/<namespace>/argocd/agent/(.+)"
      spiffe:
        # enabled must be set to true to activate SPIFFE; all other fields are optional
        enabled: true
        # "jwt" (JWT-SVIDs, for federated SPIRE) or "mtls" (X.509-SVIDs, for centralized SPIRE)
        authMethod: "jwt"
        # Name of the CSI driver that mounts the SPIFFE Workload API socket
        csiDriverName: "csi.spiffe.io"
    agent:
      enabled: true
      spiffe:
        enabled: true
        authMethod: "jwt"
        csiDriverName: "csi.spiffe.io"
```

To use SPIFFE with all defaults, only `enabled: true` is required:

```yaml
spec:
  argoCDAgent:
    principal:
      enabled: true
      spiffe:
        enabled: true
    agent:
      enabled: true
      spiffe:
        enabled: true
```

### Verifying SVID Format Compatibility

Before deploying, verify if your SPIFFE ID format will work with the default regex patterns:

```bash
# Replace with your actual SPIFFE ID
SPIFFE_ID="spiffe://example.org/ns/agent-managed/argocd/agent/agent-managed"

# Extract agent name using default pattern
AGENT_NAME=$(echo "$SPIFFE_ID" | sed -E 's|^spiffe://[^/]+/ns/[^/]+/argocd/agent/(.+)$|\1|')

# Verify agent name is extracted successfully
echo $AGENT_NAME

```

Notes:

- `spiffe.enabled` must be `true` to activate SPIFFE for a component. Omitting the `spiffe` block
  entirely, or setting `enabled: false`, leaves the component using its legacy mTLS/JWT configuration.
- `authMethod` defaults to `jwt` and `csiDriverName` defaults to `csi.spiffe.io` when omitted.
- **Default SVID Patterns**: The operator expects SVIDs to follow specific patterns. If you configure
  your SPIRE setup to issue SVIDs matching these default patterns, you do not need to set the
  `principal.auth` field explicitly. The default patterns based on `spiffe.authMethod` are:
  - `jwt`: `spiffe-jwt:spiffe://<trust-domain>/ns/<namespace>/argocd/agent/(.+)`. The agent
    identity is extracted from the SPIFFE ID in the JWT-SVID token.
  - `mtls`: `mtls:uri:spiffe://<trust-domain>/ns/<namespace>/argocd/agent/(.+)` The agent
    identity is extracted from the URI SAN of the X.509-SVID certificate (not the CN, which is what
    SPIRE-issued SVIDs use).
  
  **Custom SVID Formats**: If your SPIRE setup issues SVIDs in a different format than the defaults
  above, explicitly set `principal.auth` to a regex that matches your custom SVID format. Because
  `principal.auth` takes precedence over the SPIFFE-inferred pattern (see the note above), the
  controller will use your custom value instead of inferring one from `spiffe.authMethod`.
- **IMPORTANT**: SPIFFE must be enabled on both the principal and agent (`spiffe.enabled: true` on both sides) 
  for the authentication handshake to succeed. Enabling SPIFFE on only one side will break the connection.
- `authMethod` on the agent and principal should match, since they must negotiate the same SPIFFE
  authentication method with each other.
- **Agent-side authentication override**: Unlike the principal, which has a `principal.auth` field to override 
  the default SVID pattern, the agent does **not** have an equivalent `agent.creds` override when using SPIFFE. 
  The agent always uses the SPIFFE-inferred pattern based on `spiffe.authMethod` and cannot be overridden.
- On the agent, if `spiffe` and the legacy `tls.secretName`/`tls.rootCASecretName` fields are both
  configured, SPIFFE takes precedence for the connection to the principal; the operator logs a warning
  and the legacy TLS secret names are ignored by the agent binary at runtime.
