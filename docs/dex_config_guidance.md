# Dex Config Guidance

The scope of this document is to describe the steps to Install, Configure(Setup Login with OpenShift) and Uninstall the Dex with OpenShift GitOps Operator.

## Table of Contents

1. [Install](#install)
2. [Login with OpenShift](#login-with-openshift)
3. [Restrict login to only a set of Groups](#restrict-login-to-only-a-set-of-groups)
4. [Argo CD RBAC Policies for Dex](#argo-cd-rbac-policies-for-dex)
5. [Dex Resource requests/limits](#dex-resource-requestslimits)
6. [Uninstall](#uninstall)

## Install

> **Note**  
`DISABLE_DEX` environment variable & `.spec.dex` fields are no longer supported in OpenShift GitOps v1.10 onwards. Dex can be enabled/disabled by setting `.spec.sso.provider: dex` in ArgoCD CR.

To enable dex, set `.spec.sso.provider` to `dex` & add dex configs under `.spec.sso.dex` fields in ArgoCD CR.  

```yaml
apiVersion: argoproj.io/v1beta1
kind: ArgoCD
metadata:
  name: <argocd-instance-name> 
spec:
  sso:
    provider: dex
    dex:
      openShiftOAuth: true
```
or 
```bash
oc -n <namespace> patch argocd <argocd-instance-name> --type='merge' --patch='{ "spec": { "sso": { "provider": "dex", "dex": {"openShiftOAuth": true}}}}
```

Operator has built-in support for OpenShift Dex connector and can be enabled by setting `.spec.sso.dex.openShiftOAuth` to `true`. This will automatically configure the dex to use OpenShift users to log in into Argo CD. Any additional connector configurations can be made using `.spec.sso.dex.config` field.

> **Note**  
There is known issue due to which default OpenShift connector configurations are overridden when `.spec.sso.dex.config` is set. Fix is tracked [here](https://issues.redhat.com/browse/GITOPS-3600).

> **Important**  
Dex resource creation will not be triggered, unless there is valid Dex configuration expressed through `.spec.sso.dex`. This could either be using the default OpenShift configuration via `.spec.sso.dex.openShiftOAuth` or it could be custom Dex configuration provided by the user via `.spec.sso.dex.config`. Absence of either will result in an error due to failing health checks on Dex.

Addition configurations such as a different dex image, version, resource limits, etc can be provided. Refer [dex-options](https://argocd-operator.readthedocs.io/en/latest/reference/argocd/#dex-options) section in argocd-operator documentation for more details.

## Login with OpenShift

- Go to the `OpenShift Console -> Networking -> Routes`.

- Select your Argo CD instance namespace under `Project` dropdown.

- Click on the `<argocd-instance>-server` route url to access the Argo CD UI.

- You will be redirected to Argo CD Login Page.

- You can see an option to **LOG IN VIA OPENSHIFT** apart from the usual Argo CD login. Click on the button. (Please clear the site cache or use incognito window if facing issues).

- You will be redirected to the OpenShift Login Page.

- Provide the OpenShift login credentials to get redirected to Argo CD. 

- Upon successful login in Argo CD, you can look at the user details by clicking on the User Info Tab in Argo CD UI.

## Restrict login to only a set of Groups

Dex allows the admin to restrict login to optional list of groups. Which means, only the users who are part of any of the these groups will be able to login into Argo CD.

To enable it, use `.spec.sso.dex.groups` field in ArgoCD CR. 

For example, below will give allow only users of `foo` & `bar` group to login into Argo CD via Dex.

```yaml
apiVersion: argoproj.io/v1beta1
kind: ArgoCD
metadata:
  name: <argocd-instance-name>
spec:
  sso:
    provider: dex
    dex:
      openShiftOAuth: true
      groups:
        - foo
        - bar
```

## Argo CD RBAC Policies for Dex

#### Default access

For versions upto and not including v1.10, 

- any user (except `kube:admin`) logged into Argo CD using Dex will be a **read-only** user by default.

  `policy.default: role:readonly`

For versions starting v1.10 and above,

- any user (except `kube:admin`) logged into the default Argo CD instance `openshift-gitops` in namespace `openshift-gitops` will have **no access** by default.

  `policy.default: ''`

- any user logged into user managed custom Argo CD instance will have **read-only** access by default.

  `policy.default: 'role:readonly'`

This default behavior can be modified by updating the `.spec.rbac.defaultyPolicy` in ArgoCD CR.

```yaml
apiVersion: argoproj.io/v1beta1
kind: ArgoCD
metadata:
  name: <argocd-instance-name>
spec:
  rbac:
    defaultyPolicy: 'role:readonly'
```

A detailed information on basic role policies can be found [here](https://argo-cd.readthedocs.io/en/stable/operator-manual/rbac/#basic-built-in-roles).

#### Group Level Access

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

#### User Level Access

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

## Dex Resource requests/limits

Dex container by default gets created with following  resource requests and limits.

|Resource|Requests|Limits
|:-:|:-:|:-:|
|CPU|250m|500m|
|Memory|128 Mi|256 Mi|

Admin can modify the Dex resource requests/limits by updating `.spec.sso.dex.resources` field in ArgoCD CR. 

```yaml
apiVersion: argoproj.io/v1beta1
kind: ArgoCD
metadata:
  name: <argocd-instance-name>
spec:
  sso:
    provider: dex
    dex:
      resources:
        requests:
          cpu: 512m
          memory: 512Mi
        limits:
          cpu: 1024m
          memory: 1024Mi
```

## Uninstall

**NOTE:** `DISABLE_DEX` environment variable & `.spec.dex` fields are no longer supported in OpenShift GitOps v1.10 onwards. Please use `.spec.sso.provider` to enable/disable Dex.  

Dex can be uninstalled either by removing `.spec.sso` from the Argo CD CR, or switching to a different SSO provider.  

```bash
oc -n <namespace> patch argocd <argocd-instance-name> --type json   -p='[{"op": "remove", "path": "/spec/sso"}]'
```

Or 

```yaml
apiVersion: argoproj.io/v1beta1
kind: ArgoCD
metadata:
  name: <argocd-instance-name>
spec:
  sso: {}
```