### Non OLM based operator installation

The purpose of this script is to install and uninstall the Openshift GitOps Operator without using OLM. It uses latest version of the kustomize manifests available in the repository for creating the required k8s resources.

### Usage

The `install-gitops-operator.sh` script supports two methods of installation.
1. Using operator and component images from environment variables (default method)
2. Derive the operator and component images from the ClusterServiceVersion manifest present in the operator bundle (Note: This method requires podman binary to be available in the PATH)


### Known issues and work arounds

1. Missing RBAC access to update CRs in `argoproj.io` domain 

Issue: 

https://github.com/redhat-developer/gitops-operator/issues/148

Workaround:

Run the following script to create the required `ClusterRole` and `ClusterRoleBinding`

```
${KUBECTL} apply -f https://raw.githubusercontent.com/redhat-developer/gitops-operator/master/hack/non-bundle-install/rbac-patch.yaml
```
### Prerequisites
- kustomize (v4.57 or later)
- kubectl (v1.26.0 or later)
- yq (v4.31.2 or later)
- bash (v5.0 or later)
- git (v2.39.1 or later)
- podman (v4.4.4 or later) or docker (Note: Required only if operator and component images need to be derived from a bundle image)

### Environment Variables
The following environment variables can be set to configure various options for the installation/uninstallation process.

#### Variables for Operator image and related manifests
|Environment|Description|Default Value|
|:----------|:---------:|:-----------:|
|**NAMESPACE_PREFIX**|Namespace prefix to be used in the kustomization.yaml file when running kustomize|gitops-operator-|
|**GIT_REVISION**|The revision of the kustomize manifest to be used.|master|
|**OPERATOR_REGISTRY**|Registry server for downloading the container images|registry.redhat.io|
|**GITOPS_OPERATOR_VER**|Version of the gitops operator version to use|1.8.1-1|
|**OPERATOR_IMG**|Operator image to be used for the installation|${OPERATOR_REGISTRY}/rh-osbs/openshift-gitops-1-gitops-rhel8-operator:${GITOPS_OPERATOR_VER}|
|**USE_BUNDLE_IMG**|If the operator image and other component image needs to be derived from a bundle image, set this flag to true.|false|
|**BUNDLE_IMG**|used only when USE_BUNDLE_IMG is set to true|${OPERATOR_REGISTRY}/openshift-gitops-1/gitops-operator-bundle:${GITOPS_OPERATOR_VER}|
|**DOCKER**|used only when USE_BUNDLE_IMG is set to true. CLI binary to be used for extracting ClusterServiceVersion manifest from the Bundle Image|podman|

#### Variables for 3rd party tools used in the script
|Environment|Description|Default Value|
|:----------|:---------:|:-----------:|
|**KUSTOMIZE_VERSION**|Version of kustomize binary to be installed if not found in PATH|v4.5.7|
|**KUBECTL_VERSION**|Version of the kubectl client binary to be installed if not found in PATH|v1.26.0|
|**YQ_VERSION**|Version of the yq binary to be installed if not found in PATH|v4.31.2|

#### Variables for Component Image Overrides
|Environment|Description|Default Value|
|:----------|:---------:|:-----------:|
|**ARGOCD_DEX_IMAGE**|Image override for Argo CD DEX component|${OPERATOR_REGISTRY}/rh-osbs/openshift-gitops-1-dex-rhel8:${GITOPS_OPERATOR_VER}|
|**ARGOCD_IMAGE**|Image override for Argo CD component|${OPERATOR_REGISTRY}/rh-osbs/openshift-gitops-1-argocd-rhel8:${GITOPS_OPERATOR_VER}|
|**BACKEND_IMAGE**|Image override for Backend component|${OPERATOR_REGISTRY}/rh-osbs/openshift-gitops-1-gitops-rhel8:${GITOPS_OPERATOR_VER}|
|**GITOPS_CONSOLE_PLUGIN_IMAGE**|Image override for console plugin component|${OPERATOR_REGISTRY}/rh-osbs/openshift-gitops-1-kam-delivery-rhel8:${GITOPS_OPERATOR_VER}|
|**KAM_IMAGE**|Image override for KAM component|${OPERATOR_REGISTRY}/rh-osbs/openshift-gitops-1-kam-delivery-rhel8:${GITOPS_OPERATOR_VER}|
|**ARGOCD_KEYCLOAK_IMAGE**|Image override for Keycloak component|registry.redhat.io/rh-sso-7/sso7-rhel8-operator:7.6-8|
|**ARGOCD_REDIS_IMAGE**|Image override for Redis component|registry.redhat.io/rhel8/redis-6:1-110|
|**ARGOCD_REDIS_HA_PROXY_IMAGE**|Image override for Redis HA proxy component|registry.redhat.io/openshift4/ose-haproxy-router:v4.12.0-202302280915.p0.g3065f65.assembly.stream|

#### Variables for Operator parameters
|Environment|Description|Default Value|
|:----------|:---------:|:-----------:|
|**DISABLE_DEFAULT_ARGOCD_INSTANCE**|When set to `true`, this will disable the default 'ready-to-use' installation of Argo CD in the `openshift-gitops` namespace.|false|
|**DISABLE_DEX**|Flag to control if Dex needs to be disabled|false|
|**ARGOCD_CLUSTER_CONFIG_NAMESPACES**|OpenShift GitOps instances in the identified namespaces are granted limited additional permissions to manage specific cluster-scoped resources, which include platform operators, optional OLM operators, user management, etc.
Multiple namespaces can be specified via a comma delimited list.|openshift-gitops|
|**WATCH_NAMESPACE**|namespaces in which Argo applications can be created|None|
|**CONTROLLER_CLUSTER_ROLE**|This environment variable enables administrators to configure a common cluster role to use across all managed namespaces in the role bindings the operator creates for the Argo CD application controller.|None|
|**SERVER_CLUSTER_ROLE**|This environment variable enables administrators to configure a common cluster role to use across all of the managed namespaces in the role bindings the operator creates for the Argo CD server.|None|
### Running the script

#### Usage

```
install-gitops-operator.sh -i|-u

```

|Option|Description|
|:----------|:---------:|
-i |installs the openshift-gitops-operator|
-u |uninstalls the openshift-gitops-operator |


#### Local Run
##### Installation
```
./install-gitops-operator.sh -i

```
##### Uninstallation
```
./install-gitops-operator.sh -u

```


#### Running it from a remote URL

```
curl -L https://raw.githubusercontent.com/saumeya/gitops-operator/wget-rbac/hack/non-bundle-install/install-gitops-operator.sh | bash -s -- -i

```

#### Running install with custom Operator image

```
OPERATOR_REGISTRY=brew.registry.redhat.io GITOPS_OPERATOR_VER=v99.9.0-70 ./install-gitops-operator.sh -i
```
