# Integrate GitOps Operator with Secrets Store CSI driver for Secrets Management

The introduction of the Secrets Store CSI Driver, particularly in the OCP v4.14 release, marks a significant step in externalizing secrets management. This driver facilitates the secure attachment of secrets into application pods, ensuring that sensitive information is accessed securely and efficiently.

In this documentation, we aim to guide GitOps users through the integration of the SSCSI Driver with the GitOps operator, enhancing the security and efficiency of your GitOps workflows. This integration represents a strategic advancement in our commitment to secure, compliant, and efficient operations within the GitOps framework.

## Steps to integrate GitOps with Secrets Store CSI driver
**NOTE:** The Secrets Store CSI driver Operator will only be available on OCP versions v4.14+.
1. [Install the Secrets Store CSI driver Operator(SSCSID)](https://docs.openshift.com/container-platform/4.14/nodes/pods/nodes-pods-secrets-store.html#persistent-storage-csi-secrets-store-driver-install_nodes-pods-secrets-store)
2. Install the GitOps Operator
3. Store secrets management related resources in the Git repository
4. Configure SSCSID to mount secrets from an external secrets store to a CSI volume
5. Configure GitOps managed resources to use the mounted secrets

There are 3 providers supported by the SSCSID Operator, refer the below links for more information:
* [AWS Secrets Manager](https://docs.openshift.com/container-platform/4.14/nodes/pods/nodes-pods-secrets-store.html#secrets-store-aws_nodes-pods-secrets-store)
* [AWS Systems Manager Parameter Store](https://docs.openshift.com/container-platform/4.14/nodes/pods/nodes-pods-secrets-store.html#secrets-store-aws_nodes-pods-secrets-store-parameter-store)
* [Azure Key Vault](https://docs.openshift.com/container-platform/4.14/nodes/pods/nodes-pods-secrets-store.html#secrets-store-azure_nodes-pods-secrets-store)

## Integration guidance with an example using AWS Secrets Manager

### Prerequisites
* [Your cluster is installed on AWS and uses AWS Security Token Service (STS)](#configure-your-aws-cluster-to-use-aws-security-token-service-sts).
* [You have configured AWS Secrets Manager to store the required secrets](https://docs.aws.amazon.com/secretsmanager/latest/userguide/create_secret.html).
* [You have extracted and prepared the `ccoctl` binary](#obtain-the-ccoctl-tool).
* You have installed the `jq` CLI tool.
* You have access to the cluster as a user with the `cluster-admin` role.
* You have installed GitOps Operator and have a GitOps repository ready to use the secrets.

### Install the SSCSID
To install the Secrets Store CSI driver:
1. Install the Secrets Store CSI Driver Operator:
    
    a. Log in to the web console.
    
    b. Click **Operators** → **OperatorHub**.

    c. Locate the Secrets Store CSI Driver Operator by typing "Secrets Store CSI" in the filter box.

    d. Click the **Secrets Store CSI Driver Operator** button.

    e. On the **Secrets Store CSI Driver Operator** page, click **Install**.

    f. On the Install Operator page, ensure that:

    * **All namespaces on the cluster (default)** is selected.

    * **Installed Namespace** is set to **openshift-cluster-csi-drivers**.

    g. Click **Install**.

    After the installation finishes, the Secrets Store CSI Driver Operator is listed in the **Installed Operators** section of the web console.

2. Create the ClusterCSIDriver instance for the driver (`secrets-store.csi.k8s.io`):

    a. Click **Administration** → **CustomResourceDefinitions** → **ClusterCSIDriver**.

    b. On the **Instances** tab, click **Create ClusterCSIDriver**.
    Use the following YAML file:

    ```
    apiVersion: operator.openshift.io/v1
    kind: ClusterCSIDriver
    metadata:
      name: secrets-store.csi.k8s.io
    spec:
      managementState: Managed
    ```

    c. Click **Create**.

### Store resources of AWS Secrets Manager in the GitOps repository
1. Add resources for AWS Secrets Manager provider

    a. In your GitOps repository, create a directory and add `aws-provider.yaml` file in it to deploy resources for AWS Secrets Manager.

    *Example `aws-provider.yaml` file*
    ```
    apiVersion: v1
    kind: ServiceAccount
    metadata:
      name: csi-secrets-store-provider-aws
      namespace: openshift-cluster-csi-drivers
    ---
    apiVersion: rbac.authorization.k8s.io/v1
    kind: ClusterRole
    metadata:
      name: csi-secrets-store-provider-aws-cluster-role
    rules:
    - apiGroups: [""]
      resources: ["serviceaccounts/token"]
      verbs: ["create"]
    - apiGroups: [""]
      resources: ["serviceaccounts"]
      verbs: ["get"]
    - apiGroups: [""]
      resources: ["pods"]
      verbs: ["get"]
    - apiGroups: [""]
      resources: ["nodes"]
      verbs: ["get"]
    ---
    apiVersion: rbac.authorization.k8s.io/v1
    kind: ClusterRoleBinding
    metadata:
      name: csi-secrets-store-provider-aws-cluster-rolebinding
    roleRef:
      apiGroup: rbac.authorization.k8s.io
      kind: ClusterRole
      name: csi-secrets-store-provider-aws-cluster-role
    subjects:
    - kind: ServiceAccount
      name: csi-secrets-store-provider-aws
      namespace: openshift-cluster-csi-drivers
    ---
    apiVersion: apps/v1
    kind: DaemonSet
    metadata:
      namespace: openshift-cluster-csi-drivers
      name: csi-secrets-store-provider-aws
      labels:
        app: csi-secrets-store-provider-aws
    spec:
      updateStrategy:
        type: RollingUpdate
      selector:
        matchLabels:
          app: csi-secrets-store-provider-aws
      template:
        metadata:
          labels:
            app: csi-secrets-store-provider-aws
        spec:
          serviceAccountName: csi-secrets-store-provider-aws
          hostNetwork: false
          containers:
            - name: provider-aws-installer
              image: public.ecr.aws/aws-secrets-manager/secrets-store-csi-driver-provider-aws:1.0.r2-50-g5b4aca1-2023.06.09.21.19
              imagePullPolicy: Always
              args:
                - --provider-volume=/etc/kubernetes/secrets-store-csi-providers
              resources:
                requests:
                  cpu: 50m
                  memory: 100Mi
                limits:
                  cpu: 50m
                  memory: 100Mi
              securityContext:
                privileged: true
              volumeMounts:
                - mountPath: "/etc/kubernetes/secrets-store-csi-providers"
                  name: providervol
                - name: mountpoint-dir
                  mountPath: /var/lib/kubelet/pods
                  mountPropagation: HostToContainer
          tolerations:
            - operator: Exists
          volumes:
            - name: providervol
              hostPath:
                path: "/etc/kubernetes/secrets-store-csi-providers"
            - name: mountpoint-dir
              hostPath:
                path: /var/lib/kubelet/pods
                type: DirectoryOrCreate
          nodeSelector:
            kubernetes.io/os: linux
    ```

    b. Add `secret-provider-app.yaml` in your GitOps repository to create an application for resources of AWS Secrets Manager.

    *Example `secret-provider-app.yaml` file*
    ```
    apiVersion: argoproj.io/v1alpha1
    kind: Application
    metadata:
      name: secret-provider-app
      namespace: openshift-gitops
    spec:
      destination:
        namespace: openshift-cluster-csi-drivers
        server: https://kubernetes.default.svc
      project: default
      source:
        path: path/to/aws-provider/resources
        repoURL: https://github.com/<your-domain>/gitops.git
      syncPolicy:
        automated:
        prune: true
        selfHeal: true
    ```

2. Sync up resources with default Argo CD to deploy them in the cluster

Add `argocd.argoproj.io/managed-by: openshift-gitops` label to `openshift-cluster-csi-drivers` namespace. Apply resources managed by GitOps in your cluster. Now you can observe the `csi-secrets-store-provider-aws` daemonset keeps progressing/syncing. We will resolve this issue in the next step.

### Configure SSCSID to mount secrets from AWS Secrets Manager
1. Grant privileged access to the *csi-secrets-store-provider-aws* service account by running the following command:

```
oc adm policy add-scc-to-user privileged -z csi-secrets-store-provider-aws -n openshift-cluster-csi-drivers
```

2. Grant permission to allow the service account to read the AWS secret object:

    a. Create a `credentialsrequest-dir-aws` folder under a namespace specific directory in your GitOps repository as the credentials request is namespaced. In this documentation we assume you want to mount a secret to a deployment under `dev` namespace which is in the `/environments/dev/` directory.

    b. Create a YAML file with the following configuration for the credentials request in `/environments/dev/credentialsrequest-dir-aws/`:

    In this documentation, we are going to mount a secret to the deployment pod in `dev` namespace.

    *Example `credentialsrequest.yaml` file*
    ```
    apiVersion: cloudcredential.openshift.io/v1
    kind: CredentialsRequest
    metadata:
      name: aws-provider-test
      namespace: openshift-cloud-credential-operator
    spec:
      providerSpec:
        apiVersion: cloudcredential.openshift.io/v1
        kind: AWSProviderSpec
        statementEntries:
        - action:
          - "secretsmanager:GetSecretValue"
          - "secretsmanager:DescribeSecret"
          effect: Allow
          resource: "<aws_secret_arn>"
      secretRef:
        name: aws-creds
        namespace: dev
      serviceAccountNames:
      - default
    ```
    
    **NOTE**  
    The <aws_region> of <aws_secret_arn> has to match the cluster region. If it doesn't match, you could create a replication of your secret in the region where your cluster is on. Run the below command to find your cluster region.
    ```
    oc get infrastructure cluster -o jsonpath='{.status.platformStatus.aws.region}'
    ```
    *Example output*
    ```
    us-east-2
    ```

    c. Retrieve the OIDC provider by running the following command:
    ```
    oc get --raw=/.well-known/openid-configuration | jq -r '.issuer'
    ```
    *Example output*
    ```
    https://<oidc_provider_name>
    ```
    Copy the OIDC provider name <oidc_provider_name> from the output to use in the next step.

    d. Use the `ccoctl` tool to process the credentials request by running the following command:
    ```
    ccoctl aws create-iam-roles --name gitops-role --region=<aws_region> --credentials-requests-dir=<path-to-credentialsrequest> --identity-provider-arn arn:aws:iam::<aws_account>:oidc-provider/<oidc_provider_name> --output-dir=credrequests-ccoctl-output
    ```
    *Example output*
    ```
    2023/05/15 18:10:34 Role arn:aws:iam::<aws_account_id>:role/gitops-role-dev-aws-creds created
    2023/05/15 18:10:34 Saved credentials configuration to: credrequests-ccoctl-output/manifests/dev-aws-creds-credentials.yaml
    2023/05/15 18:10:35 Updated Role policy for Role gitops-role-dev-aws-creds
    ```

    e. Check the role policy on AWS. The **Resource** is the Secret ARN you want to mount to your resources, and you must confirm the <aws_region> of **Resource** in role policy matches the cluster region.
    
    ```
    {
        "Version": "2012-10-17",
        "Statement": [
            {
                "Effect": "Allow",
                "Action": [
                    "secretsmanager:GetSecretValue",
                    "secretsmanager:DescribeSecret"
                ],
                "Resource": "arn:aws:secretsmanager:<aws_region>:<aws_account_id>:secret:your-secret-xxxxxx"
            }
        ]
    }
    ```

    f. Bind the service account with the role ARN by running the following command:
    ```
    oc annotate -n dev sa/default eks.amazonaws.com/role-arn="<aws_role_arn>"
    ```

3. Create a secret provider class to define your secrets store provider:

`SecretProviderClass` resource is namespaced. Create a `secret-provider-class-aws.yaml` file in the same directory where the target deployment is located in your GitOps repository.

*Example `secret-provider-class-aws.yaml` file*
```
apiVersion: secrets-store.csi.x-k8s.io/v1
kind: SecretProviderClass
metadata:
  name: my-aws-provider
  namespace: dev                          # Has to match the namespace of the resource which is going to use the secret
spec:
  provider: aws                           # Specify the provider as aws
  parameters:                             # Specify provider-specific parameters
    objects: |
      - objectName: "<your-secret-name>"
        objectType: "secretsmanager"
```
After pushing this YAML file to your GitOps repository, the namespace-scoped `SecretProviderClass` resource will be populated in the target application page in Argo CD UI. You may need to manually **Sync** the `SecretProviderClass` resource if the Sync Policy your application is not set to Auto.

### Configure the resource to use this mounted secret

1. Add volume mounts configuration in the target resource

In this documentation we will add volume mounting to a deployment and configure the container pod to use the mounted secret.

*Example deployment file to use the secret provider class*
```
apiVersion: apps/v1
kind: Deployment
metadata:
  name: taxi
  namespace: dev
spec:
  replicas: 1
  template:
    metadata:
      ...
    spec:
      containers:
        - image: nginxinc/nginx-unprivileged:latest
          imagePullPolicy: Always
          name: taxi
          ports:
            - containerPort: 8080
          volumeMounts:
            - name: secrets-store-inline
              mountPath: "/mnt/secrets-store"
              readOnly: true
          resources: {}
    serviceAccountName: default
    volumes:
      - name: secrets-store-inline
        csi:
          driver: secrets-store.csi.k8s.io
          readOnly: true
          volumeAttributes:
          secretProviderClass: "my-aws-provider"
    ...
```
Click `REFRESH` on the target application page to apply the updated deployment manifest. Then you can observe that all resources will be successfully synced up.

2. Verification
List the secrets in the pod mount:
```
oc exec <deployment-pod-name> -n dev -- ls /mnt/secrets-store/
```
*Example output*
```
<your-secret-name>
```
View a secret in the pod mount:
```
oc exec <deployment-pod-name> -n dev -- cat /mnt/secrets-store/<your-secret-name>
```
*Example output*
```
<secret_value>
```


## Additional Information
### Determining the Cloud Credential Operator mode
Refer [Determining the Cloud Credential Operator mode](https://docs.openshift.com/container-platform/4.14/authentication/managing_cloud_provider_credentials/about-cloud-credential-operator.html#cco-determine-mode_about-cloud-credential-operator)
### Configuring an AWS cluster to use short-term credentials
Refer [Configuring an AWS cluster to use short-term credentials](https://docs.openshift.com/container-platform/4.14/installing/installing_aws/installing-aws-customizations.html#installing-aws-with-short-term-creds_installing-aws-customizations)
### Configure your AWS cluster to use AWS Security Token Service (STS)
**Note**  
Migration of a non-STS cluster to use STS is not supported, this is only for development or test purpose.

Follow [Steps to in-place migrate an OpenShift Cluster to STS](https://github.com/openshift/cloud-credential-operator/blob/master/docs/sts.md#steps-to-in-place-migrate-an-openshift-cluster-to-sts)
### Obtain the `ccoctl` tool
It's possible to obtain the ccoctl tool from the [mirror of latest OCP versions](https://mirror.openshift.com/pub/openshift-v4/x86_64/clients/ocp/) (it's available for the latest versions of all the newer minor releases).

It's also possible to obtain it from the Cloud Credential Operator:
1. Find the name of the Cloud Credential Operator pod:
```
$ oc get pod -n openshift-cloud-credential-operator -l app=cloud-credential-operator
NAME                                        READY   STATUS    RESTARTS   AGE
cloud-credential-operator-xxxxxxxxxx-yyyyyy   2/2     Running   0          6h33m
```
2. Copy the `ccoctl` binary from the pod to a local directory:
```
$ oc cp -c cloud-credential-operator openshift-cloud-credential-operator/<cco_pod_name>:/usr/bin/ccoctl ./ccoctl
```
3. Change the `ccoctl` permissions to make the binary executable and check that it is possible to use it:
```
$ chmod 775 ./ccoctl
$ ./ccoctl --help
```