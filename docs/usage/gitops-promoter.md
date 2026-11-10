# GitOps Promoter

The [GitOps Promoter](https://gitops-promoter.readthedocs.io/) can be deployed as an optional workload through cluster-scoped instances reconciled by the Argo CD operator.
This allows for environment promotion for GitOps via the rendered manifests pattern. This pattern can be accomplished by using Argo CD's [Source Hydrator](https://gitops-operator.readthedocs.io/en/latest/reference/argocd/#source-hydrator-options).

## Installation

The GitOps Promoter's resources can be enabled/disabled via the `.spec.promoter` field. An example of the most basic installation:

```yaml
apiVersion: argoproj.io/v1beta1
kind: ArgoCD
metadata:
  name: example-cluster-scoped-argocd-with-promoter-enabled
  namespace: some-ns
spec:
  promoter:
    enabled: true
```

As mentioned above, the Argo CD CR **must** be cluster-scoped to use the GitOps Promoter. Instructions on how to enable a cluster-scoped Argo CD can be found on [this](../basics/#cluster-scoped-instance) docs page.

There are more configuration settings that can be found on the [API Spec](../reference/api-v1beta1/#promoterspec) page.

### Resources Created

The following resources are created when the Promoter is enabled:

**Controller Manager resources (created when promoter is enabled):**

- `<instance-name>-promoter-controller-manager` serviceAccount
- `<instance-name>-<instance-namespace>-promoter-controller-manager` clusterRole (not created if PROMOTER_CONTROLLER_CLUSTER_ROLE env is set on the operator)
- `<instance-name>-<instance-namespace>-promoter-controller-manager` clusterRoleBinding
- `promoter-controller-configuration` controllerConfiguration
- `<instance-name>-promoter-controller-manager` deployment
- `<instance-name>-promoter-controller-manager` service (only when .spec.promoter.webhook.enabled: true)

**API Server resources (created when promoter is enabled, unless .spec.promoter.apiserver.enabled: false):**

- `<instance-name>-promoter-apiserver` serviceAccount
- `<instance-name>-<instance-namespace>-promoter-apiserver` clusterRole (not created if PROMOTER_API_SERVER_CLUSTER_ROLE env is set on the operator)
- `<instance-name>-<instance-namespace>-promoter-apiserver-promotionstrategydetails-viewer` clusterRole
- `<instance-name>-<instance-namespace>-promoter-apiserver` clusterRoleBinding
- `<instance-name>-<instance-namespace>-promoter-apiserver-promotionstrategydetails-viewer` clusterRoleBinding
- `<instance-name>-<instance-namespace>-promoter-apiserver-auth-delegator` clusterRoleBinding
- `<instance-name>-<instance-namespace>-promoter-apiserver-extension-auth-reader` roleBinding (in kube-system namespace)
- `<instance-name>-promoter-apiserver` service
- `v1alpha1.view.promoter.argoproj.io` apiService
- `<instance-name>-promoter-apiserver` deployment
- `promoter-apiserver-serving-cert` secret (built-in cert rotation; omitted when `.spec.promoter.apiserver.tls.certSecretName` is set)
- `<instance-name>-<instance-namespace>-promoter-controller-manager-apiserver-cert-rotation` clusterRole/clusterRoleBinding (built-in cert rotation)

### Controller TLS settings

On OpenShift, the GitOps Promoter controller inherits the cluster TLS security profile (minimum TLS version, cipher suites, and curve preferences) and passes them to the controller as:

- `--tls-min-version`
- `--tls-cipher-suites`
- `--tls-curve-preferences`

These settings apply to the controller's metrics/webhook servers, outbound HTTPS/mTLS clients, and related Go HTTP transports. When no cluster TLS profile is available (or applying it is disabled), the controller keeps Go's default TLS behavior.

### TLS for API Server

By default, when `.spec.promoter.apiserver.tls.certSecretName` is unset, the operator enables upstream gitops-promoter built-in serving-cert rotation:

- Creates an empty `promoter-apiserver-serving-cert` Secret
- Runs the controller with `--apiserver-cert-rotation` (and related flags) so it issues/rotates the serving cert and injects the CA into the APIService
- Mounts the Secret into the API Server at `/serving-certs`

The API Server may take up to a few minutes to become Ready on first install while the controller issues the certificate.

To bring your own certificates instead, set `.spec.promoter.apiserver.tls`. Provide a CA for the APIService and a certificate generated from that CA for the API Server Deployment:

```yaml
apiVersion: argoproj.io/v1beta1
kind: ArgoCD
metadata:
  name: example-promoter-apiserver-tls
  namespace: some-ns
spec:
  promoter:
    enabled: true
    apiserver:
      tls:
        certSecretName: tls-secret
        caSecretName: ca-cert-secret
        caSecretKey: ca.crt # ca.crt is the default value and does not need to be provided. If anything else is used it must be set to that.
---
apiVersion: v1
kind: Secret
metadata:
  name: tls-secret
  namespace: some-ns
type: kubernetes.io/tls
data:
  tls.crt: <base64-encoded-tls-cert>
  tls.key: <base64-encoded-tls-key>
---
apiVersion: v1
kind: Secret
metadata:
  name: ca-cert-secret
  namespace: some-ns
type: Opaque
data:
  ca.crt: <base64-encoded-ca-cert>
```

### Argo CD UI Extension

The GitOps Promoter has an Argo CD UI Extension that allows for information on Promoter resources that are deployed by Argo CD applications to be visible in the UI.
For more information see the [upstream docs page](https://gitops-promoter.readthedocs.io/en/latest/integrating-with-argocd/).

This can be enabled with the following options set.

```yaml
apiVersion: argoproj.io/v1beta1
kind: ArgoCD
metadata:
  name: example-promoter-with-ui-extension
  namespace: some-ns
spec:
  promoter:
    enabled: true
    argoCDUIExtensionEnabled: true
```

## Uninstallation

The GitOps Promoter can be disabled by setting the `.spec.promoter.enabled` to `false`. When moving to false all resources created to run the Promoter will be deleted.

```yaml
apiVersion: argoproj.io/v1beta1
kind: ArgoCD
metadata:
  name: example-uninstall-promoter
  namespace: some-ns
spec:
  promoter:
    enabled: false
```