# Migrate from [Argo CD Community Operator](https://github.com/argoproj-labs/argocd-operator) to GitOps Operator

This document provides the required guidance and steps to follow to migrate from [Argo CD Community Operator](https://github.com/argoproj-labs/argocd-operator) to GitOps Operator.

For understanding the differences between [Argo CD Community Operator](https://github.com/argoproj-labs/argocd-operator) to GitOps Operator, kindly refer to the [README](https://github.com/redhat-developer/gitops-operator/blob/master/README.md#gitops-operator-vs-argo-cd-community-operator) file of the repository.

**Note**: Installing GitOps operator will create a namespace with the name `openshift-gitops` and an Argo CD instance in the same namespace. This instance can be used for managing your OpenShift cluster configuration. It is enabled with Dex OpenShift connector by default which allows users to log in with their OpenShift credentials.

The default Argo CD instance in the `openshift-gitops` namespace can be deleted by adding an environmental variable `DISABLE_DEFAULT_ARGOCD_INSTANCE` with the value `true` in the Subscription resource.

To disable the default instance, edit the Subscription and add the following:

```yaml
spec:
  config:
    env:
      - name: DISABLE_DEFAULT_ARGOCD_INSTANCE
        value: 'true'
```

## Which GitOps Operator version should I migrate to ?

Please refer to the below table to understand the correct version of GitOps operator that you need to migrate from the community operator.

| GitOps Operator | Argo CD Operator | Default Argo CD Version |
| -------- | -------- | -------- |
| v1.5.z | v0.3.z | v2.3.z |
| v1.4.z | v0.2.z | v2.2.z |
| v1.3.z | v0.1.z | v2.1.z |

**Note**: If you are running <= `v0.16.0` version of Argo CD operator, Please upgrade to `v0.1.0` or above before you consider migrating to GitOps operator.

## Migration

### Copy any environment variables added to the Subscription resource

Any environment variables added to the subscription resource of Argo CD operator has to be added back to the subscription resource of GitOps Operator post migration.

To get the list of environment variables added to the subscription resource,

In the RedHat OpenShift platform, go to the `Operators` section (located on the left side of the toolbar).

- Go to the `Installed Operators` section, where one can view all the installed operators for the particular cluster.
- Select Argo CD, click on the `Subscription` Tab, go to Actions (located on the top right corner), click on `Edit Subscription`.
- List of environment variables can be found under `.spec.config`.

An example subscription resource that is configured with environment variables to enable custom cluster roles is shown below.

```yaml
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: argocd-operator
  namespace: argocd
spec:
  config:
    env:
    - name: CONTROLLER_CLUSTER_ROLE
      value: custom-controller-role
    - name: SERVER_CLUSTER_ROLE
      value: custom-server-role
```

Post migration the above environment variables has to be copied to GitOps operator subscription resource.

**Note**:
GitOps operator supports the below additional environment variables
`DISABLE_DEFAULT_ARGOCD_INSTANCE`: Disables the installation of default instance in openshift-gitops namespace.

`ARGOCD_CLUSTER_CONFIG_NAMESPACES`: Argo CD is granted permissions to manage specific cluster-scoped resources which include
platform operators, optional OLM operators, user management, etc. Argo CD is not granted cluster-admin. You can find the complete
information [here](https://github.com/redhat-developer/gitops-operator/blob/608b8bac2350980d7dc616c88baba634c0908e1b/docs/OpenShift%20GitOps%20Usage%20Guide.md#cluster-configuration).

### Uninstall Argo CD Operator

In the RedHat OpenShift platform, go to the `Operators` section (located on the left side of the toolbar).

- Go to the `Installed Operators` section, where one can view all the installed operators for the particular cluster.
- Select Argo CD, go to Actions (located on the top right corner), and finally select Uninstall Operator.

![image alt text](assets/Uninstall_Community_operator.png)

**Note:** OLM does not delete any Argo CD instances, Applications or workloads created by this operator.

### Install GitOps Operator

-> Go to Operators -> OperatorHub -> Red Hat OpenShift GitOps -> Install

![image alt text](assets/Install_GitOps_Operator.png)

All the workloads, applications and resources created by the Argo CD Operator are preserved. OLM does not remove any configuration
created by the Argo CD Operator. You can login into the GitOPs operator with the same user credentials that were used to log into Argo CD
Operator.

**Note:**
GitOps operator is a Red Hat provider operator. Post installation, it updates the workloads(controller, repo-server, server e.t.c.,.) to Red Hat container images - `registry.redhat.io/openshift-gitops-1/argocd-rhel8`.

But if you have configured the Argo CD custom resource with `.spec.image` and `.spec.version` fields to use images
from `quay.io` or other registries, Post installation, GitOps operator does not update the workloads to `registry.redhat.io/openshift-gitops-1/argocd-rhel8` images.

This is not recommended for the following reasons.

1. They are not supported by Red Hat.
2. GitOps operator fails to install in a Disconnected or Air-gapped clusters.
