- [Overview](#overview)
- [Installing & Configuring Dex](#installing--configuring-dex)
- [Dex OpenShift OAuth Connector](#dex-openshift-oauth-connector)
    - [Role Mappings](#role-mappings)
- [Dex GitHub Connector](#dex-github-connector)
- [Uninstalling Dex](#uninstalling-dex)

## Overview

Dex can be used to delegate authentication to external identity providers like GitHub, SAML and others. SSO configuration of Argo CD requires updating the Argo CD CR with [Dex connector](https://dexidp.io/docs/connectors/) settings.

## Installing & Configuring Dex

Dex can be enabled by setting `.spec.sso.provider` to `dex` in the `ArgoCD` CR.

!!! note
    It is now mandatory to specify `.spec.sso.dex` either with OpenShift configuration through `openShiftOAuth: true` or valid custom configuration supplied through `.spec.sso.dex.config`. Absence of either will result in an error due to failing health checks on Dex.

!!! note
    Specifying `.spec.sso.dex` without setting dex as the provider will result in an error.

!!! note
    `.spec.dex` is no longer supported since OpenShift GitOps v1.10 onwards, use `.spec.sso.dex` instead.

An example of correctly configured dex would look as follows:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: ArgoCD
metadata:
  name: example-argocd
spec:
  sso:
    provider: dex
    dex:
      openShiftOAuth: true
```

## Dex OpenShift OAuth Connector

The below section describes how to configure Argo CD SSO using OpenShift connector as an example. Dex makes use of the users and groups defined within OpenShift by querying the platform provided OAuth server.

The `openShiftOAuth` property can be used to trigger the operator to auto configure the built-in OpenShift OAuth server. The `groups` property is used to mandate users to be part of one or all the groups in the groups list. The RBAC `Policy` property is used to give the admin role in the Argo CD cluster to users in the OpenShift `cluster-admins` group.

``` yaml
apiVersion: argoproj.io/v1alpha1
kind: ArgoCD
metadata:
  name: example-argocd
  labels:
    example: openshift-oauth
spec:
  sso:
    provider: dex
    dex:
      openShiftOAuth: true
      groups:
      - default
  rbac:
    defaultPolicy: 'role:readonly'
    policy: |
      g, cluster-admins, role:admin
    scopes: '[groups]'
```

### Login with OpenShift

- Go to the `OpenShift Console -> Networking -> Routes`.
- Select your Argo CD instance namespace under `Project` dropdown.
- Click on the `<argocd-instance>-server` route url to access the Argo CD UI.
- You will be redirected to Argo CD Login Page.
- You can see an option to **LOG IN VIA OPENSHIFT** apart from the usual Argo CD login. Click on the button. (Please clear the site cache or use incognito window if facing issues).
- You will be redirected to the OpenShift Login Page.
- Provide the OpenShift login credentials to get redirected to Argo CD. 
- Upon successful login in Argo CD, you can look at the user details by clicking on the User Info Tab in Argo CD UI.

#### Role Mappings

To have a specific user be properly attributed with the `role:admin` upon SSO through Openshift, the user needs to be in a **group** with the `cluster-admin` role added. If the user only has a direct `ClusterRoleBinding` to the Openshift role for `cluster-admin`, the Argo CD role will not map.

A quick fix will be to create a group named `cluster-admins` group, add the user to the group and then apply the `cluster-admin` ClusterRole to the group.

```txt
oc adm groups new cluster-admins
oc adm groups add-users cluster-admins USER
oc adm policy add-cluster-role-to-group cluster-admin cluster-admins
```

### Group Level Access

Dex reads the group information of OpenShift users. This allows admin to configure rbac at group level using group name. `.spec.rbac.policy` in ArgoCD CR can be used to add group level rbac policies. 

For example, below will give admin level access to all the users from `foo-admins` OpenShift group.

```yaml
apiVersion: argoproj.io/v1beta1
kind: ArgoCD
metadata:
  name: <argocd-instance-name>
spec:
  rbac:
    policy.csv: |
      g, foo-admins, role:admin
```

More information regarding Argo CD RBAC can be found [here](https://argo-cd.readthedocs.io/en/stable/operator-manual/rbac/).

### User Level Access

Admin can control access at individual user level by adding rbac configurations under `.spec.rbac.policy` in ArgoCD CR.

> **Important**  
It is not recommended to use user level RBAC with OpenShift login as it poses security risk. Refer [this](https://github.com/argoproj/argo-cd/discussions/8160#discussioncomment-1975554) discussion for more details. 

For example, below will give admin level access to a user with email `foo@example.com` & user with username `bar`

```yaml
apiVersion: argoproj.io/v1beta1
kind: ArgoCD
metadata:
  name: <argocd-instance-name>
spec:
  rbac:
    policy.csv: |
      g, foo@example.com, role:admin
      g, bar, role:admin
    scopes: '[groups,name,email]'
```

More information regarding Argo CD RBAC can be found [here](https://argo-cd.readthedocs.io/en/stable/operator-manual/rbac/).

## Dex GitHub Connector

The below section describes how to configure Argo CD SSO using GitHub (OAuth2) as an example, but the steps should be similar for other identity providers.

1. Register the application in the identity provider as explained [here](https://argoproj.github.io/argo-cd/operator-manual/user-management/#1-register-the-application-in-the-identity-provider).

2. Update the Argo CD CR.

In the `sso.dex.config` key, add the github connector to the connectors sub field. See the Dex [GitHub connector documentation](https://github.com/dexidp/website/blob/main/content/docs/connectors/github.md) for explanation of the fields. A minimal config should populate the `clientID`, `clientSecret` generated in Step 1.
You will very likely want to restrict logins to one or more GitHub organization. In the
`connectors.config.orgs` list, add one or more GitHub organizations. Any member of the org will then be able to login to Argo CD to perform management tasks.

``` yaml
apiVersion: argoproj.io/v1alpha1
kind: ArgoCD
metadata:
  name: example-argocd
  labels:
    example: openshift-oauth
spec:
  sso:
    provider: dex
    dex:
      config: |
        connectors:
          # GitHub example
          - type: github
            id: github
            name: GitHub
            config:
              clientID: xxxxxxxxxxxxxx
              clientSecret: $dex.github.clientSecret # Alternatively $<some_K8S_secret>:dex.github.clientSecret
              orgs:
              - name: dummy-org
```

## Use ArgoCD's Dex for Argo Workflows authentication

The below section describes how to configure Argo CD's Dex to accept authentication requests from Argo Workflows.

1. Register the application in the identity provider as explained [here](https://argoproj.github.io/argo-cd/operator-manual/user-management/#1-register-the-application-in-the-identity-provider).

2. Update the Argo CD CR.

In the `sso.dex.env` key, add the environment variable as shown in the [example manifests for authenticating against Argo CD's Dex](https://argoproj.github.io/argo-workflows/argo-server-sso-argocd/#example-manifests-for-authenticating-against-argo-cds-dex-kustomize).

``` yaml
apiVersion: argoproj.io/v1alpha1
kind: ArgoCD
metadata:
  name: example-argocd
spec:
  sso:
    provider: dex
    dex:
      config: |
        connectors:
          # GitHub example
          - type: github
            id: github
            name: GitHub
            config:
              clientID: xxxxxxxxxxxxxx
              clientSecret: $dex.github.clientSecret # Alternatively $<some_K8S_secret>:dex.github.clientSecret
              orgs:
              - name: dummy-org
      env:
        - name: ARGO_WORKFLOWS_SSO_CLIENT_SECRET
          valueFrom:
            secretKeyRef:
              name: argo-workflows-sso
              key: client-secret
```

## Additional Volume Mounts for Dex Container

You can optionally inject additional volumes and volume mounts into the Dex container managed by the Argo CD Operator. This allows for advanced configurations such as providing custom certificates, identity provider connectors, or other external resources required by Dex.

```yaml
apiVersion: argoproj.io/v1beta1
kind: ArgoCD
metadata:
  name: example-argocd
spec:
  sso:
    provider: dex
    dex: 
      volumeMounts:
        - name: custom-cert
          mountPath: /etc/dex/ssl
          readOnly: true
      volumes:
        - name: custom-cert
          secret:
            secretName: dex-tls-secret
```

- `spec.sso.dex.volumeMounts` and `spec.sso.dex.volumes` follow the standard Kubernetes `volumeMounts` and `volumes` schema.
- Ensure the volume name matches between the `volumes` and `volumeMounts` sections.

Addition configurations such as a different dex image, version, resource limits, etc., can be provided. Refer [dex-options](https://gitops-operator.readthedocs.io/en/latest/reference/argocd/#dex-options) section in argocd-operator documentation for more details.

## Uninstalling Dex

Dex can be uninstalled either by removing `.spec.sso` from the Argo CD CR, or switching to a different SSO provider.
