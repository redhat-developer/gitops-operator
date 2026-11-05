## Overview

[Argo CD Image Updater](https://argocd-image-updater.readthedocs.io/) controller is now available as an optional workload that can be configured through the Argo CD operator. 

## Installation

Argo CD Image Updater controller can be enabled/disabled using a new toggle within the Argo CD CR with default specs as follows:

``` yaml
apiVersion: argoproj.io/v1beta1
kind: ArgoCD
metadata:
  name: example-argocd
spec:
  imageUpdater:
    enabled: true
```

Users may also specify advanced configuration such as the resource requirements or environment variables for the image updater controller. The full list of available settings can be found in the [API spec](../reference/argocd.md#image-updater-controller-options).

### Watch scope

The Image Updater controller's watch scope determines which namespaces it monitors for `ImageUpdater` CRs and Argo CD `Applications`. It is controlled by the `IMAGE_UPDATER_WATCH_NAMESPACES` environment variable, set via `.spec.imageUpdater.env`.

The operator supports two installation modes:

#### Install into the Argo CD namespace (recommended, default)

The controller runs in the same namespace as the Argo CD instance and watches only that namespace. This is the default behavior when `IMAGE_UPDATER_WATCH_NAMESPACES` is not set or is empty.

If you use Argo CD's [Applications in any namespace](https://gitops-operator.readthedocs.io/en/latest/usage/apps-in-any-namespace/) feature and have `Application` resources in additional namespaces, you can specify a comma-separated list of namespaces to watch. Each entry can be an exact name, a glob-style wildcard, or a regular expression:

```yaml
spec:
  imageUpdater:
    enabled: true
    env:
      - name: IMAGE_UPDATER_WATCH_NAMESPACES
        # exact names
        value: "app-ns1,app-ns2"
```

```yaml
      - name: IMAGE_UPDATER_WATCH_NAMESPACES
        # glob: matches team-a-argocd, team-b-argocd, ...
        value: "*-argocd"
```

```yaml
      - name: IMAGE_UPDATER_WATCH_NAMESPACES
        # regex (must be wrapped in /.../)
        value: "/^team-[0-9]+$/"
```

The operator resolves the patterns to concrete namespaces at reconcile time and creates a `Role` and `RoleBinding` in each matching namespace. When the namespace list changes (e.g. a new tenant namespace is created that matches the pattern), the operator automatically provisions the required RBAC on the next reconcile.

!!! note
    Regular expression patterns must be wrapped in forward slashes (`/pattern/`). Patterns without slashes are treated as glob patterns. For example, `team-*` is a glob (matches `team-1`, `team-frontend`, etc.), while `/^team-[0-9]+$/` is a regex (matches `team-1` but not `team-frontend`).

#### Cluster-scoped installation

For environments where the controller must watch `ImageUpdater` CRs in all namespaces, set `IMAGE_UPDATER_WATCH_NAMESPACES` to `"*"`:

``` yaml
spec:
  imageUpdater:
    enabled: true
    env:
      - name: IMAGE_UPDATER_WATCH_NAMESPACES
        value: "*"
```

This mode requires the ArgoCD instance to be installed in a cluster configuration namespace. The operator will create a `ClusterRole` and `ClusterRoleBinding` instead of namespace-scoped RBAC.

!!! note
    Installing Image Updater into a namespace separate from the Argo CD namespace (upstream Option 2) is **not supported** by this operator. Refer to the [upstream documentation](https://argocd-image-updater.readthedocs.io/) for details on that installation pattern.

### Resources created

The resources created depend on the selected watch scope:

**namespace-scoped (default or comma-separated namespaces):**

*  `<instance-name>-argocd-image-updater-controller` deployment
*  `<instance-name>-argocd-image-updater-controller` serviceAccount
*  `<instance-name>-argocd-image-updater-controller` role (in the ArgoCD namespace)
*  `<instance-name>-argocd-image-updater-controller` roleBinding (in the ArgoCD namespace)
*  `<instance-name>_<namespace>` role + roleBinding (one per extra namespace, when a comma-separated list is provided)
*  `argocd-image-updater-config` configmap
*  `argocd-image-updater-ssh-config` configmap
*  `argocd-image-updater-secret` secret

**cluster-scoped (`IMAGE_UPDATER_WATCH_NAMESPACES="*"`):**

*  `<instance-name>-argocd-image-updater-controller` deployment
*  `<instance-name>-argocd-image-updater-controller` serviceAccount
*  `<instance-name>-argocd-image-updater-controller` role (in the ArgoCD namespace)
*  `<instance-name>-argocd-image-updater-controller` roleBinding (in the ArgoCD namespace)
*  `<instance-name>-<instance-namespace>-argocd-image-updater-controller` clusterRole
*  `<instance-name>-<instance-namespace>-argocd-image-updater-controller` clusterRoleBinding
*  `argocd-image-updater-config` configmap
*  `argocd-image-updater-ssh-config` configmap
*  `argocd-image-updater-secret` secret

The operator creates the `argocd-image-updater-config` and `argocd-image-updater-ssh-config` configmaps that are editable to users, and will not be reconciled/overwritten by the operator. The `argocd-image-updater-secret` is an empty secret that can be used to configure credentials for the supported image updater services.

Instructions for appropriate configuration of these resources can be found within [upstream documentation](https://argocd-image-updater.readthedocs.io/).

## Webhook

By default, Image Updater polls registries for new images. It can also run a [webhook server](https://argocd-image-updater.readthedocs.io/en/stable/configuration/webhook/) that registries call when an image is pushed, so updates are picked up immediately.

The webhook server is exposed by enabling a Route (OpenShift) or an Ingress under `.spec.imageUpdater.webhookServer`:

```yaml
apiVersion: argoproj.io/v1beta1
kind: ArgoCD
metadata:
  name: example-argocd
spec:
  imageUpdater:
    enabled: true
    webhookServer:
      route:
        enabled: true
      secrets:
        quay:
          name: image-updater-webhook-secrets
          key: quay
```

When a Route or an Ingress is enabled, the operator:

* sets `ENABLE_WEBHOOK=true` on the Image Updater container
* creates a `<instance-name>-image-updater-webhook` Service on port `8082`
* creates the `<instance-name>-image-updater-webhook` Route and/or Ingress, exposing only the `/webhook` path

The Service, Route and Ingress are deleted again when both the Route and the Ingress are disabled.

The webhook port is fixed at `8082` because the Image Updater NetworkPolicy only allows traffic on that port. Don't set `WEBHOOK_PORT` in `.spec.imageUpdater.env` while the webhook is exposed.

### Webhook URL

All registries use the same endpoint and are told apart by the `type` query parameter:

```
https://<webhook-host>/webhook?type=<registry>
```

Supported types are `docker.io`, `ghcr.io`, `quay.io`, `harbor`, `acr`, `aliyun-acr` and `cloudevents`. Registries that cannot send the secret in a header pass it as a query parameter, for example `https://<webhook-host>/webhook?type=quay.io&secret=<secret>`. Refer to the [upstream documentation](https://argocd-image-updater.readthedocs.io/en/stable/configuration/webhook/) for registry-specific setup.

!!! warning
    The `secret` query parameter travels in the request URL, so it can be retained by Ingress, proxy, API gateway, access and monitoring logs, and by anything else that stores the URL. When the registry supports sending the secret in a header, prefer that. Docker Hub, Quay and Aliyun ACR only accept the query parameter. If you use it, URL-encode the secret because the webhook server decodes the query value, never reuse a secret configured for another service, and configure your Ingress controller, proxies and monitoring to omit or redact query strings.

The host depends on the exposure type and can be changed with `.spec.imageUpdater.webhookServer.host`:

* **Route**: defaults to `<instance-name>-image-updater-webhook-<namespace>.<apps-domain>`, where `<apps-domain>` is the cluster apps domain, for example `oc get ingress.config cluster -o jsonpath='{.spec.domain}'`. The effective host is also shown by `oc get route <instance-name>-image-updater-webhook -o jsonpath='{.spec.host}'`.
* **Ingress**: defaults to `<instance-name>`; likewise shown in `oc get ingress <instance-name>-image-updater-webhook -o jsonpath='{.spec.rules[0].host}'`. The host must resolve to the Ingress controller, for example through a DNS record you create.

### Webhook secrets

By default, the webhook server rejects requests from any registry that has no secret configured. Create a Secret in the Argo CD namespace and reference it under `.spec.imageUpdater.webhookServer.secrets`:

```bash
oc create secret generic image-updater-webhook-secrets -n <argocd-namespace> \
  --from-literal=quay=<quay-webhook-secret>
```

| Field         | Environment variable set on the container |
|---------------|-------------------------------------------|
| `docker`      | `DOCKER_WEBHOOK_SECRET`                   |
| `ghcr`        | `GHCR_WEBHOOK_SECRET`                     |
| `quay`        | `QUAY_WEBHOOK_SECRET`                     |
| `harbor`      | `HARBOR_WEBHOOK_SECRET`                   |
| `acr`         | `ACR_WEBHOOK_SECRET`                      |
| `aliyunACR`   | `ALIYUN_ACR_WEBHOOK_SECRET`               |
| `cloudEvents` | `CLOUDEVENTS_WEBHOOK_SECRET`              |

The operator does not copy the secret. The environment variable references the user's Secret directly. If the referenced Secret or key does not exist, Image Updater still starts and keeps polling registries, but rejects webhooks from that registry; the operator reports the missing reference in the `ImageUpdaterWebhookConfigurationError` condition, and restarts the Image Updater pod once the Secret is created or fixed.

For each registry, the operator uses the first of these that is set:

1. A reference under `.spec.imageUpdater.webhookServer.secrets`.
2. An environment variable of the same name in `.spec.imageUpdater.env`.
3. The `webhook.<registry>-secret` key in `argocd-image-updater-secret` (for example `webhook.quay-secret`), as described in the upstream documentation. This reference is optional.

Image Updater reads webhook secrets only at startup. The operator rolls the Image Updater pod automatically when a secret referenced under `.spec.imageUpdater.webhookServer.secrets`, or a `webhook.<registry>-secret` key in `argocd-image-updater-secret`, changes. Secrets you wire in yourself through `.spec.imageUpdater.env` are not tracked.

!!! note
    `argocd-image-updater-secret` is deleted when Image Updater is disabled. Keep webhook secrets in a Secret you manage and reference it under `.spec.imageUpdater.webhookServer.secrets`.

To accept webhooks from registries that have no secret configured, set `WEBHOOK_REQUIRE_SECRET` to `false`:

```yaml
spec:
  imageUpdater:
    enabled: true
    env:
      - name: WEBHOOK_REQUIRE_SECRET
        value: "false"
```

!!! warning
    With `WEBHOOK_REQUIRE_SECRET` set to `false`, anyone who can reach the webhook Route or Ingress can send webhook requests for registries without a secret. Only use it in network-isolated environments.

### TLS

By default, the webhook server serves TLS using an OpenShift [service serving certificate](https://docs.openshift.com/container-platform/latest/security/certificates/service-serving-certificate.html) stored in the `<instance-name>-image-updater-webhook-tls` Secret. The Route defaults to `reencrypt` termination with `Redirect` for insecure traffic. The operator rolls the Image Updater pod when the certificate is issued or rotated.

Set `insecure: true` to serve plain HTTP instead. The Route then defaults to `edge` termination:

```yaml
spec:
  imageUpdater:
    enabled: true
    webhookServer:
      insecure: true
      route:
        enabled: true
```

Route TLS settings can be overridden through `.spec.imageUpdater.webhookServer.route.tls`, the same way as for other operator-managed Routes. To present your own certificate to registries, reference a `kubernetes.io/tls` Secret with `route.tls.externalCertificate`; the operator updates the Route when that Secret is renewed. The termination must match the `insecure` setting: `edge` for an insecure webhook server, `reencrypt` or `passthrough` otherwise.

As with the Argo CD server Route, when webhook TLS is in use the operator requests a service serving certificate unless the Route is enabled with `passthrough` termination. This includes a Route whose `tls` block is unset, which defaults to `reencrypt`, and the case where only an Ingress is enabled and no Route exists. With `passthrough`, registries see the webhook server's own certificate, so provide one they trust in a `kubernetes.io/tls` Secret named `<instance-name>-image-updater-webhook-tls`. If you switch an existing Route from `reencrypt` to `passthrough`, the operator stops requesting a certificate, but the Secret still holds the previously issued one: replace its contents with your certificate. If you later switch back to `reencrypt` while the Secret holds your own certificate, the operator does not replace it; either delete the Secret so the service CA issues a new one, or set `destinationCACertificate` in the Route TLS settings so the router trusts your certificate.

The TLS protocol versions and ciphers follow the cluster-wide TLS security profile, like the rest of Image Updater.

!!! warning
    Image Updater refuses to start when the certificate in `<instance-name>-image-updater-webhook-tls` cannot be used: for example, when it is malformed, does not match its private key, has expired, or is not yet valid. This stops the whole Image Updater controller, including registry polling, until the certificate is fixed. A running pod keeps serving the certificate it loaded at startup, so the problem only appears the next time the pod restarts. Certificates issued by the OpenShift service CA are rotated automatically; if you provide your own certificate, make sure it is renewed before it expires.

On non-OpenShift clusters there is no service serving certificate authority. Either set `insecure: true`, or provide your own certificate in a `kubernetes.io/tls` Secret named `<instance-name>-image-updater-webhook-tls` (for example, issued by cert-manager). Without a certificate, the webhook server generates a self-signed certificate at startup. The Ingress defaults the `nginx.ingress.kubernetes.io/backend-protocol` annotation to `HTTPS`, or to `HTTP` in insecure mode.

### Configuration warnings

When the webhook configuration cannot work as intended, the operator sets an `ImageUpdaterWebhookConfigurationError` condition on the ArgoCD CR. This happens when:

* no registry webhook secret is configured and `WEBHOOK_REQUIRE_SECRET` is not set to `false` in `.spec.imageUpdater.env`
* the Route TLS termination contradicts the `insecure` setting
* a Secret or key referenced under `webhookServer.secrets` does not exist or is empty
* a Service, Route or Ingress named `<instance-name>-image-updater-webhook` already exists and is controlled by another owner, or is managed by an Argo CD Application; the operator leaves it untouched
* an environment variable in `.spec.imageUpdater.env` is overridden with a different value by a `webhookServer` setting: `ENABLE_WEBHOOK` (always `true` while the webhook is exposed), `DISABLE_TLS` (follows `insecure`), or a `<REGISTRY>_WEBHOOK_SECRET` that is also declared under `webhookServer.secrets`
* `WEBHOOK_PORT` is set in `.spec.imageUpdater.env` to a port other than `8082`
* `TLS_CERT_PATH` or `TLS_KEY_PATH` is set in `.spec.imageUpdater.env` while `insecure` is `false`
* `.spec.imageUpdater.image` is an upstream Image Updater image older than v1.4.0 while `insecure` is `false`

### Migrating from a manually configured webhook

Before `webhookServer` was available, the webhook server could only be enabled through `.spec.imageUpdater.env`, with a Service and Route created by hand. Such installations keep working unchanged after upgrading the operator: as long as neither `webhookServer.route.enabled` nor `webhookServer.ingress.enabled` is set, the operator does not touch the webhook environment variables, does not mount a certificate, and does not create, change or delete any Service, Route or Ingress for the webhook. Upgrading the operator does not restart the Image Updater pod for this feature.

!!! note
    With Image Updater v1.2 and v1.3, the operator could not provide a certificate to the webhook server, so it always used a self-signed certificate. Manually configured webhooks therefore often run with `DISABLE_TLS=true` behind an `edge` Route.

To switch to the operator-managed configuration:

1. Decide on the TLS mode. If your existing Route uses `edge` termination with `DISABLE_TLS=true`, either set `webhookServer.insecure: true` to keep plain HTTP, or leave it `false` to let the operator provide a certificate and use `reencrypt`.
2. Remove the webhook settings the operator now manages from `.spec.imageUpdater.env`: `ENABLE_WEBHOOK`, `DISABLE_TLS`, `WEBHOOK_PORT`, `TLS_CERT_PATH` and `TLS_KEY_PATH`. While the webhook is exposed, `webhookServer` settings take precedence over these environment variables, because the operator derives the Service, Route and certificate configuration from the same settings. Any value that is overridden or would break the webhook is reported in the `ImageUpdaterWebhookConfigurationError` condition.
3. Optionally move webhook secrets to `webhookServer.secrets`. Existing `*_WEBHOOK_SECRET` entries in `.spec.imageUpdater.env` keep working and take precedence over the `argocd-image-updater-secret` fallback.
4. Enable `webhookServer.route` (or `webhookServer.ingress`).
5. Update the webhook URL in your registries to the new Route host, then delete the Service, Route or Ingress you created by hand. If yours were named `<instance-name>-image-updater-webhook`, the operator adopts and reconfigures them instead, and deletes them when the webhook is disabled again. Objects with that name that are controlled by another owner, such as another operator, are never adopted or deleted. Neither are objects carrying Argo CD tracking metadata (the `argocd.argoproj.io/tracking-id` annotation, or the `app.kubernetes.io/instance` or custom application instance label), because the operator and Argo CD would keep reverting each other's changes. If you manage your hand-made objects with Argo CD, remove them from the Application before enabling `webhookServer`.
6. Remove any other workarounds you put in place for the webhook certificate once the new configuration works.

## Uninstallation

Argo CD Image Updater controller can be disabled by setting `.spec.imageUpdater.enabled` to `false` :

``` yaml
apiVersion: argoproj.io/v1beta1
kind: ArgoCD
metadata:
  name: example-argocd
spec:
  imageUpdater:
    enabled: false
```
This will clean up all the aforementioned image updater controller resources that were created by the operator.
