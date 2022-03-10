# Migrate from [Argo CD Community Operator](https://github.com/argoproj-labs/argocd-operator) to GitOps Operator

This document provides the required guidance and steps to follow to migrate from [Argo CD Community Operator](https://github.com/argoproj-labs/argocd-operator) to GitOps Operator.

Please refer to the README file of the repository to understand the differences between [Argo CD Community Operator](https://github.com/argoproj-labs/argocd-operator) to GitOps Operator.

**Note**: Installing GitOps operator will create a namespace with name `openshift-gitops` and an Argo CD instance in the same namespace. This Instance can be used for managing your OpenShift cluster configuration. It is enabled with Dex OpenShift connector by default which allows users to login with their OpenShift credentials.

The  default Argo CD instance in the `openshift-gitops` namespace can be deleted by adding an environmental variable `DISABLE_DEFAULT_ARGOCD_INSTANCE` with value `true` in the Subscription resource.

Edit the Subscription and add the following

```yaml
spec:
  config:
    env:
      - name: DISABLE_DEFAULT_ARGOCD_INSTANCE
        value: 'true'
```

Which GitOps Operator version should I migrate to ?

Please refer to the below table to understand the correct version of GitOps operator that you need to migrate from the community operator.

| GitOps Operator | Argo CD Operator | Default Argo CD Version |
| -------- | -------- | -------- |
| v1.5.z | v0.3.z | v2.3.z |
| v1.4.z | v0.2.z | v2.2.z |
| v1.3.z | v0.1.z | v2.1.z |

**Note**: If you are running <= `v0.16.0` version of Argo CD operator, Please upgrade to `v0.1.0` or above before you consider migrating to GitOps operator.

## Migration

### Uninstall Argo CD Operator

-> Go to Operators -> Installed Operators -> Argo CD -> Actions -> Uninstall Operator

![image alt text](assets/Uninstall_Community_operator.png)

### Install GitOps Operator

-> Go to Operators -> OperatorHub -> Red Hat OpenShift GitOps -> Install

![image alt text](assets/Install_GitOps_Operator.png)
