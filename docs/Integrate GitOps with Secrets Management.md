# Integrate GitOps with Secrets Management

## Steps to integrate GitOps with Secrets Store CSI driver
1. Install the Secrets Store CSI driver Operator(SSCSID)
2. Install the GitOps Operator
3. Store secrets management related resources in the Git repo
4. Configure SSCSID to mount secrets from an external secrets store to a CSI volume
4. Configure GitOps managed resources to use the mounted secrets

## Integration guidance with an example using AWS Secrets Manager

### Prerequisites
* Your cluster is installed on AWS and uses AWS Security Token Service (STS).
* You have configured AWS Secrets Manager to store the required secrets.
* You have extracted and prepared the `ccoctl` binary.
* You have installed the `jq` CLI tool.
* You have access to the cluster as a user with the `cluster-admin` role.

Follow

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

### Install GitOps
1. Install the GitOps Operator
Follow the steps on [Installing OpenShift GitOps](OpenShift%20GitOps%20Usage%20Guide.md#installing-openshift-gitops).

2. Bootstrap a GitOps repository
Follow the steps on [Getting started with GitOps Application Manager (kam)](OpenShift%20GitOps%20Usage%20Guide.md#getting-started-with-gitops-application-manager-kam).


### Store resources of AWS Secrets Manager in the GitOps repository
1. Add resources for AWS Secrets Manager provider
In your KAM generated GitOps repository, add `secret-provider-app.yaml` for resources of AWS Secrets Manager to `/config/argocd` directory.

    ***Example*** `secret-provider-app.yaml` ***file***
    ```
    apiVersion: argoproj.io/v1alpha1
    kind: Application
    metadata:
    creationTimestamp: null
    name: secret-provider-app
    namespace: openshift-gitops
    spec:
    destination:
        namespace: openshift-cluster-csi-drivers
        server: https://kubernetes.default.svc
    ignoreDifferences:
    - group: argoproj.io
        jsonPointers:
        - /status
        kind: Application
    - group: triggers.tekton.dev
        jsonPointers:
        - /status
        kind: EventListener
    - group: triggers.tekton.dev
        jsonPointers:
        - /status
        kind: TriggerTemplate
    - group: triggers.tekton.dev
        jsonPointers:
        - /status
        kind: TriggerBinding
    - group: route.openshift.io
        jsonPointers:
        - /spec/host
        kind: Route
    project: default
    source:
        path: config/sscsid
        repoURL: https://github.com/your-domain/gitops.git
    syncPolicy:
        automated:
        prune: true
        selfHeal: true
    status:
    health: {}
    summary: {}
    sync:
        comparedTo:
        destination: {}
        source:
            repoURL: ""
        status: ""
    ```

    Create `/config/sscsid/` directory and add `aws-provider.yaml` file in it to deploy resources for AWS Secrets Manager.

    ***Example*** `aws-provider.yaml` ***file***
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

2. Sync up resources with default Argo CD to deploy them in the cluster

    Add `argocd.argoproj.io/managed-by: openshift-gitops` label to `openshift-cluster-csi-drivers` namespace. In your local GitOps repository directory, run `oc apply -k config/argocd` to deploy resources. Now you can observe the `csi-secrets-store-provider-aws` daemonset keeps progressing. 

### Configure SSCSID to mount secrets from AWS Secrets Manager
1. Grant privileged access to the csi-secrets-store-provider-aws service account by running the following command:

    ```
    oc adm policy add-scc-to-user privileged -z csi-secrets-store-provider-aws -n openshift-cluster-csi-drivers
    ```

2. Grant permission to allow the service account to read the AWS secret object:

    a. Create a directory to contain the credentials request by running the following command:
    ```
    mkdir credentialsrequest-dir-aws
    ```
    You can also create a `credentialsrequest-dir-aws` folder under `/environments/dev/` as the credentials request is namespaced.

    b. Create a YAML file with the following configuration for the credentials request:

    KAM generated GitOps repository has two environments in their own namespaces. In this documentation, we are going to mount the secret to the deployment pod in `environments/dev` environment which is under `dev` namespace.

    ***Example*** `credentialsrequest.yaml` ***file***
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
            resource: "arn:aws:secretsmanager:us-east-2:<your_IAM_account>:secret:your-secret-FR8xZP"
    secretRef:
        name: aws-creds
        namespace: dev
    serviceAccountNames:
        - default
    ```
    c. Retrieve the OIDC provider by running the following command:
    ```
    oc get --raw=/.well-known/openid-configuration | jq -r '.issuer'
    ```
    ***Example output***
    ```
    https://<oidc_provider_name>
    ```
    Copy the OIDC provider name <oidc_provider_name> from the output to use in the next step.

    d. Use the `ccoctl` tool to process the credentials request by running the following command:
    ```
    ccoctl aws create-iam-roles --name my-role --region=<aws_region> --credentials-requests-dir=<path-to-credentialsrequest> --identity-provider-arn arn:aws:iam::<aws_account>:oidc-provider/<oidc_provider_name> --output-dir=credrequests-ccoctl-output
    ```
    ***Example output***
    ```
    2023/05/15 18:10:34 Role arn:aws:iam::<aws_account_id>:role/my-role-dev-aws-creds created
    2023/05/15 18:10:34 Saved credentials configuration to: credrequests-ccoctl-output/manifests/dev-aws-creds-credentials.yaml
    2023/05/15 18:10:35 Updated Role policy for Role my-role-dev-aws-creds
    ```
    **NOTE** When create the IAM role, the <aws_region> has to match the cluster region. You can run `oc get infrastructure cluster -o jsonpath='{.status.platformStatus.aws.region}'` to find out your cluster region.

    e. Bind the service account with the role ARN by running the following command:
    ```
    oc annotate -n dev sa/aws-provider eks.amazonaws.com/role-arn="<aws_role_arn>"
    ```

3. Create a secret provider class to define your secrets store provider:

    `SecretProviderClass` resource is namespaced. Create a `secret-provider-class-aws.yaml` file in `/environments/dev/apps/app-taxi/services/taxi/base/config` directory.
    ***Example*** `secret-provider-app.yaml` ***file***
    ```
    apiVersion: secrets-store.csi.x-k8s.io/v1
    kind: SecretProviderClass
    metadata:
    name: my-aws-provider
    namespace: dev  \\ Has to match the namespace of the resource which is going to use the secret
    spec:
    provider: aws  \\ Specify the provider as aws
    parameters:  \\ Provider-specific parameters
        objects: |
        - objectName: "gitops-secret"  \\ This is the secret name you created in AWS
            objectType: "secretsmanager"
    ```
    In this `SecretProviderClass` you have to specify the provider-specific configuration parameters. Here's an example of parameters specific for `azure` provider.
    ```
    ...
    spec:
    provider: azure                         
    parameters:                             
        usePodIdentity: "false"
        useVMManagedIdentity: "false"
        userAssignedIdentityID: ""
        keyvaultName: "kvname"
        objects: |
        array:
            - |
            objectName: secret1
            objectType: secret
        tenantId: "tid"
    ```

### Configure the resource to use this mounted secret

1. Add volume mounts configuration in the target resource

    In this documentation we will add volume mounting to deployment of app-taxi and configure the container pod to use the mounted secret.

    ***Example updated `/environments/dev/apps/app-taxi/services/taxi/base/config/100-deployment.yaml` file to use the secret provider class***
    ```
    apiVersion: apps/v1
    kind: Deployment
    metadata:
    creationTimestamp: null
    labels:
        app.kubernetes.io/name: taxi
        app.kubernetes.io/part-of: app-taxi
    name: taxi
    namespace: dev
    spec:
    replicas: 1
    selector:
        matchLabels:
        app.kubernetes.io/name: taxi
        app.kubernetes.io/part-of: app-taxi
    strategy: {}
    template:
        metadata:
        creationTimestamp: null
        labels:
            app.kubernetes.io/name: taxi
            app.kubernetes.io/part-of: app-taxi
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
    status: {}
    ```

2. Resync resources in Argo CD UI

3. Verification
List the secrets in the pod mount:
```
oc exec taxi-<hash> -n dev -- ls /mnt/secrets-store/
```
***Example output***
```
gitops-secret
```
View a secret in the pod mount:
```
oc exec taxi-<hash> -n dev -- cat /mnt/secrets-store/gitops-secret
```
***Example output***
```
<secret_value>
```

## Additional Information
### Configure your AWS cluster to use AWS Security Token Service (STS)
Follow [Steps to in-place migrate an OpenShift Cluster to STS](https://github.com/openshift/cloud-credential-operator/blob/master/docs/sts.md#steps-to-in-place-migrate-an-openshift-cluster-to-sts)

