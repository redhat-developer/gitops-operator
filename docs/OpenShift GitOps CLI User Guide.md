# OpenShift GitOps CLI User Guide

## Installing OpenShift GitOps CLI (argocd)
Use the CLI tool to manage Red Hat OpenShift GitOps from a terminal. You can install the CLI tool on different platforms.

### Installing the Red Hat OpenShift GitOps CLI on Linux using an RPM
For Red Hat Enterprise Linux (RHEL) version 8, you can install the Red Hat OpenShift GitOps CLI as an RPM.
#### Prerequisites
You have an active OpenShift Container Platform subscription on your Red Hat account.
You have root or sudo privileges on your local system.
#### Procedure
- Register with Red Hat Subscription Manager:
```
# subscription-manager register
```
- Pull the latest subscription data:
```
# subscription-manager refresh
```
- List the available subscriptions:
```
# subscription-manager list --available --matches '*openshift-gitops*'
```
- In the output for the previous command, find the pool ID for your OpenShift Container Platform subscription and attach the subscription to the registered system:
```
# subscription-manager attach --pool=<pool_id>
```
- Enable the repositories required by Red Hat OpenShift GitOps:
    - Linux (x86_64, amd64)
        ```
        # subscription-manager repos --enable="openshift-gitops-1.12-for-rhel-8-x86_64-rpms"
        ```
    - Linux on IBM zSystems and IBM® LinuxONE (s390x)
        ```
        # subscription-manager repos --enable="openshift-gitops-1.12-for-rhel-8-s390x-rpms"
        ```
    - Linux on IBM Power (ppc64le)
        ```
        # subscription-manager repos --enable="openshift-gitops-1.12-for-rhel-8-ppc64le-rpms"
        ```
    - Linux on ARM (aarch64, arm64)
        ```
        # subscription-manager repos --enable="openshift-gitops-1.12-for-rhel-8-aarch64-rpms"
        ```
- Install the `openshift-gitops-argocd-cli` package:
```
# yum install openshift-gitops-argocd-cli
```
#### Verification
Run the following command to validate that the installation has succeeded.
```
argocd version --client
```
Sample output:
```
argocd: v2.9.2+c5ea5c4
  BuildDate: 2023-12-18T12:35:23Z
  GitCommit: c5ea5c4df52943a6fff6c0be181fde5358970304
  GitTreeState: clean
  GoVersion: go1.20.10
  Compiler: gc
  Platform: linux/amd64
  ExtraBuildInfo: openshift-gitops-version: 1.11.0, release: 0718122023
```
**Note:** The above output is just for reference. The actual details might be different based on the version of OpenShift GitOps argocd CLI client installed.

### Installing the Red Hat OpenShift GitOps CLI on Windows
#### Prerequisites
- ZIP file tools like winrar, 7zip etc
#### Procedure
- Download the [CLI tool](https://mirror.openshift.com/pub/openshift-v4/clients/argocd-cli/1.12.0/argocd-cli-windows-amd64.zip).
- Extract the archive with a ZIP program.
- Add the location of your `argocd` files to your `PATH` environment variable.
#### Verification
Run the following command to validate that the installation has succeeded.
```
argocd version --client
```
Sample output:
```
argocd: v2.9.2+c5ea5c4
  BuildDate: 2023-12-18T12:35:23Z
  GitCommit: c5ea5c4df52943a6fff6c0be181fde5358970304
  GitTreeState: clean
  GoVersion: go1.20.10
  Compiler: gc
  Platform: linux/amd64
  ExtraBuildInfo: openshift-gitops-version: 1.11.0, release: 0718122023
```
**Note:** The above output is just for reference. The actual details might be different based on the version of OpenShift GitOps argocd CLI client installed.

### Installing the Red Hat OpenShift GitOps CLI on macOS
#### Prerequisites
- tar
#### Procedure
- Download the CLI tool for the appropriate processor architecture
    - [macOS on Intel](https://mirror.openshift.com/pub/openshift-v4/clients/argocd-cli/1.12.0/argocd-cli-darwin-amd64.tar.gz)
    - [macOS on ARM](https://mirror.openshift.com/pub/openshift-v4/clients/argocd-cli/1.12.0/argocd-cli-darwin-arm64.tar.gz)

- Extract the archive with a ZIP program.
- Add the location of your `argocd` files to your `PATH` environment variable.
#### Verification
Run the following command to validate that the installation has succeeded.
```
argocd version --client
```
Sample output:
```
argocd: v2.9.2+c5ea5c4
  BuildDate: 2023-12-18T12:35:23Z
  GitCommit: c5ea5c4df52943a6fff6c0be181fde5358970304
  GitTreeState: clean
  GoVersion: go1.20.10
  Compiler: gc
  Platform: linux/amd64
  ExtraBuildInfo: openshift-gitops-version: 1.11.0, release: 0718122023
```
**Note:** The above output is just for reference. The actual details might be different based on the version of OpenShift GitOps argocd CLI client installed.

## Configuring OpenShift GitOps CLI (argocd)

Configure the Red Hat OpenShift GitOps `argocd` CLI to enable tab completion.

### Enabling tab completion

After you install the `argocd` CLI, you can enable tab completion to automatically complete `argocd` commands or suggest options when you press Tab.

#### Prerequisites
- You must have the `argocd` CLI tool installed.
- You must have bash-completion installed on your local system.

####  Procedure
The following procedure enables tab completion for Bash.

1. Save the Bash completion code to a file:
  ```
  $ argocd completion bash > argocd_bash_completion
  ```
2. Copy the file to /etc/bash_completion.d/:
  ```
  $ sudo cp argocd_bash_completion /etc/bash_completion.d/
  ```
  Alternatively, you can save the file to a local directory and source it from your `.bashrc` file instead.

Tab completion is enabled when you open a new terminal.

## OpenShift GitOps argocd reference

This section lists the basic `argocd` CLI commands.
**Note** MicroShift based installation do not host an ArgoCD server and supports only the `core` mode of execution. 
In the `core` mode (`--core` argument specified), the CLI talks directly to the Kubernetes API server set as per the `KUBECONFIG` environment variable or the default file `$HOME/.kube/config`. There is no need for users to login into the ArgoCD server for executing commands.

### Basic syntax

#### Normal mode

In the normal mode, users have to login to the ArgoCD server component using the login component before executing the commands.

  1. Get the admin password for the ArgoCD server
      ```
      ADMIN_PASSWD=$(kubectl get secret argocd-initial-admin-secret -n argocd -o jsonpath='{.data.password}' | base64 -d)
      ```
  2. Login to the ArgoCD server using the login command
      ```
      argocd login --username admin --password ${ADMIN_PASSWD} <server url>
      #eg:
      argocd login --username admin --password ${ADMIN_PASSWD} openshift-gitops.openshift-gitops.apps-crc.testing
      ```
  3. Execute the argocd commands
      ```
      argocd [command or options] [arguments…​]
      ```

#### Core mode

In the `core` mode (`--core` argument specified), the CLI talks directly to the Kubernetes API server set as per the `KUBECONFIG` environment variable or the default file `$HOME/.kube/config`. There is no need for users to login into the ArgoCD server for executing commands. The commands would be run as user configured in the kubeconfig file. 

  1. With the default context in kubeconfig file
    ```
    KUBECONFIG=~/.kube/config argocd --core [command or options] [arguments…​]
    ```
  2. With a custom context in kubeconfig file
    ```
    KUBECONFIG=~/.kube/config argocd --core --kube-context [context] [command or options] [arguments…​]
    ```

### Global options
Global options are options applicable to all sub-commands of `argocd`.

| Option| Argument Type | Description|
| ----- | ------------- |----------- |
| --auth-token | string | Authentication token    |
| --client-crt | string | Client certificate file
| --client-crt-key | string | Client certificate key file |
| --config | string | Path to Argo CD config (default "/home/user/.config/argocd/config") |
| --controller-name | string | Name of the Argo CD Application controller; set this or the ARGOCD_APPLICATION_CONTROLLER_NAME environment variable when the controller's name label differs from the default, for example when installing via the Helm chart (default "argocd-application-controller") |
| --core | | If set to true then CLI talks directly to Kubernetes instead of talking to Argo CD API server |
| --grpc-web | | Enables gRPC-web protocol. Useful if Argo CD server is behind proxy which does not support HTTP2. |
| --grpc-web-root-path | string | Enables gRPC-web protocol. Useful if Argo CD server is behind proxy which does not support HTTP2. Set web root.|
| -H, --header | strings | Sets additional header to all requests made by Argo CD CLI. (Can be repeated multiple times to add multiple headers, also supports comma separated headers) |
| -h, --help | | help for argocd |
| --http-retry-max | int | Maximum number of retries to establish http connection to Argo CD server |
| --insecure | | Skip server certificate and domain verification |
| --kube-context | string | Directs the command to the given kube-context |
| --logformat | string | Set the logging format. One of: text|json (default "text") |
| --loglevel | string | Set the logging level. One of: debug|info|warn|error (default "info") |
| --plaintext | | Disable TLS |
| --port-forward | | Connect to a random argocd-server port using port forwarding |
| --port-forward-namespace | string | Namespace name which should be used for port forwarding |
| --redis-haproxy-name | string | Name of the Redis HA Proxy; set this or the ARGOCD_REDIS_HAPROXY_NAME environment variable when the HA Proxy's name label differs from the default, for example when installing via the Helm chart (default "argocd-redis-ha-haproxy") |
| --redis-name | string | Name of the Redis deployment; set this or the ARGOCD_REDIS_NAME environment variable when the Redis's name label differs from the default, for example when installing via the Helm chart (default "argocd-redis") |


### Utility Commands

#### version
Print version information

##### Usage:
```
argocd version [flags]
```
##### Examples
- Print the full version of client and server to stdout
```
argocd version
```
- Print only full version of the client - no connection to server will be made
```
argocd version --client
```
- Print only full version of the server
```
argocd version --server openshift-gitops.openshift-gitops.crc.local
```
- Print the full version of client and server in JSON format
```
argocd version -o json
```
- Print only client and server core version strings in YAML format
```
argocd version --short -o yaml
```

##### help 
Prints the help message about any command

##### Usage:
```
argocd version [sub-command]
```

##### Examples:
- To get the help text for all the available commands, run the following command
```
argocd help admin
```

- To get the help text for `admin` sub command run the following command
```
argocd help admin
```

##### completion
Write bash or zsh shell completion code to standard output.

For bash, ensure you have bash completions installed and enabled.
To access completions in your current shell, run the following command
```
$ source <(argocd completion bash)
```
Alternatively, write it to a file and source in .bash_profile

For zsh, add the following to your ~/.zshrc file:
```
source <(argocd completion zsh)
compdef _argocd argocd
```
Optionally, also add the following, in case you are getting errors involving compdef & compinit such as command not found: 
```
compdef:
autoload -Uz compinit
compinit
```

##### Usage:
  argocd completion SHELL [flags]

###### Flags:
  -h, --help   help for completion

### Login related Commands
* [argocd login](./cli/argocd_login.md)   - Log in to an Argo CD server
* [argocd logout](./cli/argocd_logout.md) - Log out from Argo CD
* [argocd relogin](./cli/argocd_relogin.md)   - Refresh an expired authenticate token

### Administrative Commands
* [argocd admin](./cli/argocd_admin.md)	 - Contains a set of commands useful for Argo CD administrators and requires direct Kubernetes access
* [argocd admin export](./cli/argocd_admin_export.md) - Export all Argo CD data to stdout (default) or a file
* [argocd admin import](./cli/argocd_admin_import.md) - Import Argo CD data from stdin (specify `-') or a file
* [argocd admin app](./cli/argocd_admin_app.md)	 - Manage applications configuration
* [argocd admin cluster](./cli/argocd_admin_cluster.md)	 - Manage clusters configuration
* [argocd admin dashboard](./cli/argocd_admin_dashboard.md)	 - Starts Argo CD Web UI locally
* [argocd admin export](./cli/argocd_admin_export.md)	 - Export all Argo CD data to stdout (default) or a file
* [argocd admin import](./cli/argocd_admin_import.md)	 - Import Argo CD data from stdin (specify `-') or a file
* [argocd admin initial-password](./cli/argocd_admin_initial-password.md)	 - Prints initial password to log in to Argo CD for the first time
* [argocd admin notifications](./cli/argocd_admin_notifications.md)	 - Set of CLI commands that helps manage notifications settings
* [argocd admin proj](./cli/argocd_admin_proj.md)	 - Manage projects configuration
* [argocd admin repo](./cli/argocd_admin_repo.md)	 - Manage repositories configuration
* [argocd admin settings](./cli/argocd_admin_settings.md)	 - Provides set of commands for settings validation and troubleshooting
* [argocd admin app diff-reconcile-results](./cli/argocd_admin_app_diff-reconcile-results.md)	 - Compare results of two reconciliations and print diff.
* [argocd admin app generate-spec](./cli/argocd_admin_app_generate-spec.md)	 - Generate declarative config for an application
* [argocd admin app get-reconcile-results](./cli/argocd_admin_app_get-reconcile-results.md)	 - Reconcile all applications and stores reconciliation summary in the specified file.
* [argocd admin cluster generate-spec](./cli/argocd_admin_cluster_generate-spec.md)	 - Generate declarative config for a cluster
* [argocd admin cluster kubeconfig](./cli/argocd_admin_cluster_kubeconfig.md)	 - Generates kubeconfig for the specified cluster
* [argocd admin cluster namespaces](./cli/argocd_admin_cluster_namespaces.md)	 - Print information namespaces which Argo CD manages in each cluster.
* [argocd admin cluster shards](./cli/argocd_admin_cluster_shards.md)	 - Print information about each controller shard and the estimated portion of Kubernetes resources it is responsible for.
* [argocd admin cluster stats](./cli/argocd_admin_cluster_stats.md)	 - Prints information cluster statistics and inferred shard number
* [argocd admin notifications template](./cli/argocd_admin_notifications_template.md)	 - Notification templates related commands
* [argocd admin notifications trigger](./cli/argocd_admin_notifications_trigger.md)	 - Notification triggers related commands
* [argocd admin proj generate-allow-list](./cli/argocd_admin_proj_generate-allow-list.md)	 - Generates project allow list from the specified clusterRole file
* [argocd admin proj generate-spec](./cli/argocd_admin_proj_generate-spec.md)	 - Generate declarative config for a project
* [argocd admin proj update-role-policy](./cli/argocd_admin_proj_update-role-policy.md)	 - Implement bulk project role update. Useful to back-fill existing project policies or remove obsolete actions.
* [argocd admin repo generate-spec](./cli/argocd_admin_repo_generate-spec.md)	 - Generate declarative config for a repo
* [argocd admin settings rbac](./cli/argocd_admin_settings_rbac.md)	 - Validate and test RBAC configuration
* [argocd admin settings resource-overrides](./cli/argocd_admin_settings_resource-overrides.md)	 - Troubleshoot resource overrides
* [argocd admin settings validate](./cli/argocd_admin_settings_validate.md)	 - Validate settings

### Account management commands
* [argocd account](./cli/argocd_account.md)	 - Manage argo accounts
* [argocd account bcrypt](./cli/argocd_account_bcrypt.md)	 - Generate bcrypt hash for any password
* [argocd account can-i](./cli/argocd_account_can-i.md)	 - Can I
* [argocd account delete-token](./cli/argocd_account_delete-token.md)	 - Deletes account token
* [argocd account generate-token](./cli/argocd_account_generate-token.md)	 - Generate account token
* [argocd account get](./cli/argocd_account_get.md)	 - Get account details
* [argocd account get-user-info](./cli/argocd_account_get-user-info.md)	 - Get user info
* [argocd account list](./cli/argocd_account_list.md)	 - List accounts
* [argocd account update-password](./cli/argocd_account_update-password.md)	 - Update an account's password

### GPG key management Commands
* [argocd gpg add](./cli/argocd_gpg_add.md)	 - Adds a GPG public key to the server's keyring
* [argocd gpg get](./cli/argocd_gpg_get.md)	 - Get the GPG public key with ID <KEYID> from the server
* [argocd gpg list](./cli/argocd_gpg_list.md)	 - List configured GPG public keys
* [argocd gpg rm](./cli/argocd_gpg_rm.md)	 - Removes a GPG public key from the server's keyring

### Project management commands
* [argocd proj](./cli/argocd_proj.md)	 - Manage projects
* [argocd proj add-destination](./cli/argocd_proj_add-destination.md)	 - Add project destination
* [argocd proj add-orphaned-ignore](./cli/argocd_proj_add-orphaned-ignore.md)	 - Add a resource to orphaned ignore list
* [argocd proj add-signature-key](./cli/argocd_proj_add-signature-key.md)	 - Add GnuPG signature key to project
* [argocd proj add-source](./cli/argocd_proj_add-source.md)	 - Add project source repository
* [argocd proj allow-cluster-resource](./cli/argocd_proj_allow-cluster-resource.md)	 - Adds a cluster-scoped API resource to the allow list and removes it from deny list
* [argocd proj allow-namespace-resource](./cli/argocd_proj_allow-namespace-resource.md)	 - Removes a namespaced API resource from the deny list or add a namespaced API resource to the allow list
* [argocd proj create](./cli/argocd_proj_create.md)	 - Create a project
* [argocd proj delete](./cli/argocd_proj_delete.md)	 - Delete project
* [argocd proj deny-cluster-resource](./cli/argocd_proj_deny-cluster-resource.md)	 - Removes a cluster-scoped API resource from the allow list and adds it to deny list
* [argocd proj deny-namespace-resource](./cli/argocd_proj_deny-namespace-resource.md)	 - Adds a namespaced API resource to the deny list or removes a namespaced API resource from the allow list
* [argocd proj edit](./cli/argocd_proj_edit.md)	 - Edit project
* [argocd proj get](./cli/argocd_proj_get.md)	 - Get project details
* [argocd proj list](./cli/argocd_proj_list.md)	 - List projects
* [argocd proj remove-destination](./cli/argocd_proj_remove-destination.md)	 - Remove project destination
* [argocd proj remove-orphaned-ignore](./cli/argocd_proj_remove-orphaned-ignore.md)	 - Remove a resource from orphaned ignore list
* [argocd proj remove-signature-key](./cli/argocd_proj_remove-signature-key.md)	 - Remove GnuPG signature key from project
* [argocd proj remove-source](./cli/argocd_proj_remove-source.md)	 - Remove project source repository
* [argocd proj role](./cli/argocd_proj_role.md)	 - Manage a project's roles
* [argocd proj set](./cli/argocd_proj_set.md)	 - Set project parameters
* [argocd proj windows](./cli/argocd_proj_windows.md)	 - Manage a project's sync windows

### Application management commands
* [argocd app](./cli/argocd_app.md)	 - Manage argo Applications
* [argocd app actions](./cli/argocd_app_actions.md)	 - Manage Resource actions
* [argocd app create](./cli/argocd_app_create.md)	 - Create an application
* [argocd app delete](./cli/argocd_app_delete.md)	 - Delete an application
* [argocd app delete-resource](./cli/argocd_app_delete-resource.md)	 - Delete resource in an application
* [argocd app diff](./cli/argocd_app_diff.md)	 - Perform a diff against the target and live state.
* [argocd app edit](./cli/argocd_app_edit.md)	 - Edit application
* [argocd app get](./cli/argocd_app_get.md)	 - Get application details
* [argocd app history](./cli/argocd_app_history.md)	 - Show application deployment history
* [argocd app list](./cli/argocd_app_list.md)	 - List applications
* [argocd app logs](./cli/argocd_app_logs.md)	 - Get logs of application pods
* [argocd app manifests](./cli/argocd_app_manifests.md)	 - Print manifests of an application
* [argocd app patch](./cli/argocd_app_patch.md)	 - Patch application
* [argocd app patch-resource](./cli/argocd_app_patch-resource.md)	 - Patch resource in an application
* [argocd app resources](./cli/argocd_app_resources.md)	 - List resource of application
* [argocd app rollback](./cli/argocd_app_rollback.md)	 - Rollback application to a previous deployed version by History ID, omitted will Rollback to the previous version
* [argocd app set](./cli/argocd_app_set.md)	 - Set application parameters
* [argocd app sync](./cli/argocd_app_sync.md)	 - Sync an application to its target state
* [argocd app terminate-op](./cli/argocd_app_terminate-op.md)	 - Terminate running operation of an application
* [argocd app unset](./cli/argocd_app_unset.md)	 - Unset application parameters
* [argocd app wait](./cli/argocd_app_wait.md)	 - Wait for an application to reach a synced and healthy state
* [argocd app actions](./cli/argocd_app_actions.md)	 - Manage Resource actions
* [argocd app create](./cli/argocd_app_create.md)	 - Create an application
* [argocd app delete](./cli/argocd_app_delete.md)	 - Delete an application
* [argocd app delete-resource](./cli/argocd_app_delete-resource.md)	 - Delete resource in an application
* [argocd app diff](./cli/argocd_app_diff.md)	 - Perform a diff against the target and live state.
* [argocd app edit](./cli/argocd_app_edit.md)	 - Edit application
* [argocd app get](./cli/argocd_app_get.md)	 - Get application details
* [argocd app history](./cli/argocd_app_history.md)	 - Show application deployment history
* [argocd app list](./cli/argocd_app_list.md)	 - List applications
* [argocd app logs](./cli/argocd_app_logs.md)	 - Get logs of application pods
* [argocd app manifests](./cli/argocd_app_manifests.md)	 - Print manifests of an application
* [argocd app patch](./cli/argocd_app_patch.md)	 - Patch application
* [argocd app patch-resource](./cli/argocd_app_patch-resource.md)	 - Patch resource in an application
* [argocd app resources](./cli/argocd_app_resources.md)	 - List resource of application
* [argocd app rollback](./cli/argocd_app_rollback.md)	 - Rollback application to a previous deployed version by History ID, omitted will Rollback to the previous version
* [argocd app set](./cli/argocd_app_set.md)	 - Set application parameters
* [argocd app sync](./cli/argocd_app_sync.md)	 - Sync an application to its target state
* [argocd app terminate-op](./cli/argocd_app_terminate-op.md)	 - Terminate running operation of an application
* [argocd app unset](./cli/argocd_app_unset.md)	 - Unset application parameters
* [argocd app wait](./cli/argocd_app_wait.md)	 - Wait for an application to reach a synced and healthy state

### Application Set management commands
* [argocd appset](./cli/argocd_appset.md)	 - argocd controls a Argo CD server
* [argocd appset create](./cli/argocd_appset_create.md)	 - Create one or more ApplicationSets
* [argocd appset delete](./cli/argocd_appset_delete.md)	 - Delete one or more ApplicationSets
* [argocd appset get](./cli/argocd_appset_get.md)	 - Get ApplicationSet details
* [argocd appset list](./cli/argocd_appset_list.md)	 - List ApplicationSets
* [argocd appset update](./cli/argocd_appset_update.md)	 - Updates the given ApplicationSet(s)

### Repository management commands
* [argocd repo add](./cli/argocd_repo_add.md)	 - Add git repository connection parameters
* [argocd repo get](./cli/argocd_repo_get.md)	 - Get a configured repository by URL
* [argocd repo list](./cli/argocd_repo_list.md)	 - List configured repositories
* [argocd repo rm](./cli/argocd_repo_rm.md)	 - Remove repository credentials
* [argocd repocreds](./cli/argocd_repocreds.md) - Repository credentials command
* [argocd repocreds add](./cli/argocd_repocreds_add.md)	 - Add git repository connection parameters
* [argocd repocreds list](./cli/argocd_repocreds_list.md)	 - List configured repository credentials
* [argocd repocreds rm](./cli/argocd_repocreds_rm.md)	 - Remove repository credentials


## Creating an application by using OpenShift GitOps argocd CLI

### Create an application in Normal mode

#### Prerequisites

- OpenShift CLI (oc)
- OpenShift GitOps CLI (argocd)

#### Procedure
  1. Get the admin password for the ArgoCD server
      ```
      ADMIN_PASSWD=$(kubectl get secret argocd-initial-admin-secret -n argocd -o jsonpath='{.data.password}' | base64 -d)
      ```
  2. Login to the ArgoCD server using the login command
      ```
      argocd login --username admin --password ${ADMIN_PASSWD} <server url>
      #eg:
      argocd login --username admin --password ${ADMIN_PASSWD} openshift-gitops.openshift-gitops.apps-crc.testing
      ```
  3. Validate that you are able to run `argocd` commands in normal mode by executing the following command to list all Applications. 
    ```
    # argocd app list
    ```
  If the configuration is correct, then existing Applications will be listed with header as below
    ```
    NAME CLUSTER NAMESPACE  PROJECT  STATUS  HEALTH   SYNCPOLICY  CONDITIONS  REPO PATH TARGET
    ```
  4. Let's create an application in normal mode
    ```
    # argocd app create app-spring-petclinic \
        --repo https://github.com/redhat-developer/openshift-gitops-getting-started.git \
        --path app \
        --revision main \
        --dest-server  https://kubernetes.default.svc \
        --dest-namespace spring-petclinic \
        --directory-recurse \
        --sync-policy automated \
        --self-heal \
        --sync-option Prune=true \
        --sync-option CreateNamespace=true \
        --annotations "argocd.argoproj.io/managed-by=openshift-gitops"
    ```
  5. List the application to confirm that the application is created successfully and repeat the command till the application reaches the state `Synced` and `Healthy`
    ```
    # argocd app list
    ```

### Create an application in Core mode

#### Prerequisites

- OpenShift CLI (oc)
- OpenShift GitOps CLI (argocd)

#### Procedure

  1. Login to the OpenShift Cluster using the `oc` CLI tool
    ```
    # oc login -u [username] -p [password] [server_url]
    eg:
    # oc login -u kubeadmin -p ${ADMIN_PASSWD} https://api.crc.testing:6443
    ```
  2. Check if the context is set correctly in the kubeconfig file
    ```
    # oc config current-context
    ```
  3. Set the default namespace of the current context to `openshift-gitops`
    ```
    # oc config set-context --current --namespace openshift-gitops
    ```
  4. Validate that you are able to run `argocd` commands in core mode by executing the following command to list all Applications. 
    ```
    # argocd app list --core
    ```
  If the configuration is correct, then existing Applications will be listed with header as below
    ```
    NAME CLUSTER NAMESPACE  PROJECT  STATUS  HEALTH   SYNCPOLICY  CONDITIONS  REPO PATH TARGET
    ```
  5. Let's create an application in core mode
    ```
    # argocd app create app-spring-petclinic --core \
        --repo https://github.com/redhat-developer/openshift-gitops-getting-started.git \
        --path app \
        --revision main \
        --dest-server  https://kubernetes.default.svc \
        --dest-namespace spring-petclinic \
        --directory-recurse \
        --sync-policy automated \
        --self-heal \
        --sync-option Prune=true \
        --sync-option CreateNamespace=true \
        --annotations "argocd.argoproj.io/managed-by=openshift-gitops"
    ```
  6. List the application to confirm that the application is created successfully and repeat the command till the application reaches the state `Synced` and `Healthy`
    ```
    # argocd app list --core
    ```


## Syncing an application by using OpenShift GitOps argocd CLI

### Syncing an application in normal mode
#### Prerequisites

- OpenShift CLI (oc)
- OpenShift GitOps CLI (argocd)
#### Procedure

  1. Get the admin password for the ArgoCD server
      ```
      ADMIN_PASSWD=$(kubectl get secret argocd-initial-admin-secret -n argocd -o jsonpath='{.data.password}' | base64 -d)
      ```
  2. Login to the ArgoCD server using the login command
      ```
      argocd login --username admin --password ${ADMIN_PASSWD} <server url>
      #eg:
      argocd login --username admin --password ${ADMIN_PASSWD} openshift-gitops.openshift-gitops.apps-crc.testing
      ```
  3. If the argo application is created with manual sync policy, then the user has to trigger the sync operation manually. This can be done by using the `argocd` CLI in normal mode as below
    ```
    argocd app sync --core openshift-gitops/app-spring-petclinic
    ```
### Syncing an application in core mode
#### Prerequisites

- OpenShift CLI (oc)
- OpenShift GitOps CLI (argocd)

#### Procedure

  1. Login to the OpenShift Cluster using the `oc` CLI tool
    ```
    # oc login -u [username] -p [password] [server_url]
    eg:
    # oc login -u kubeadmin -p ${ADMIN_PASSWD} https://api.crc.testing:6443
    ```
  2. Check if the context is set correctly in the kubeconfig file
    ```
    # oc config current-context
    ```
  3. Set the default namespace of the current context to `openshift-gitops`
    ```
    # oc config set-context --current --namespace openshift-gitops
    ```

  4. If the argo application is created with manual sync policy, then the user has to trigger the sync operation manually. This can be done by using the `argocd` CLI in core mode as below
    ```
    # argocd app sync --core openshift-gitops/app-spring-petclinic
    ```