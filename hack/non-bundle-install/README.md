### Non OLM based operator installation

The purpose of this script is to install and uninstall the GitOps Operator without using OLM

### Run the script

#### Options

```
-i -> for installing the operator
-u -> for uninstalling the operator

```

#### Run locally

```
./install-gitops-operator.sh -i

```


#### Run it remotely

```
curl -L https://raw.githubusercontent.com/saumeya/gitops-operator/wget-rbac/hack/non-bundle-install/install-gitops-operator.sh | bash -s -- -i

```
