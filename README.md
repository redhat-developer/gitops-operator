# OpenShift Gitops Service Operator

The Gitops Service Operator delivers and manages the backend which powers the OpenShift GitOps UI.
The service would be available at a well-known UI location.

# Getting started

## Making the operator available on the in-cluster OperatorHub

1. Add the following resource to your cluster

```
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: Gitops-service-source
  namespace: openshift-marketplace
spec:
  displayName: 'Gitops Service by Red Hat'
  image: 'quay.io/redhat-developer/helm-chart-repository-configuration-api-index:0.0.1'
  publisher: 'Red Hat Developer'
  sourceType: grpc
```

2. Go the OperatorHub on OpenShift Webconsole and look for the "Gitops Service" operator.



![a relative link](docs/assets/operatorhub-listing.png)

3. Install the operator using the defaults in the wizard, and wait for it to show up in the list of "Installed Operators". I it doesn't go check on it's status in the "Installed Operators" in the `openshift-operators` namespace.

![a relative link](docs/assets/installed-operator.png)

4. To validate if the installation was successful, look for the route named `cluster` in the `openshift-app-gitops` namespace. Note, the namespace doesn't have to exist in advance, the operator creates it for you.

That's it, your API `route` should be created for you. You don't need to expliclty create any operand/CR.

## Contribute


1. Clone the repository.
2. Login to a cluster on your command-line.
3. `OPERATOR_NAME=gitops-operator operator-sdk run local --watch-openshift-app-gitops`


## Re-build and Deploy

This operator currently deploys the following payload.

```
quay.io/redhat-developer/gitops-backend:v0.0.1
```

If that's all what you are changing, the following steps are not needed in development
mode. You could update your image "payload" and re-install the operator.

* Build the operator image.

```
docker build -t quay.io/redhat-developer/gitops-backend-operator-bundle
docker push quay.io/redhat-developer/gitops-backend-operator:v0.0.1
```


2. Build the Bundle image ( operator + OLM manifests )

```
operator-sdk bundle create quay.io/redhat-developer/gitops-backend-operator-bundle:v0.0.3
docker push quay.io/redhat-developer/gitops-backend-operator-bundle:v0.0.3
```

3. Build the Index image

```
opm index add --bundles quay.io/redhat-developer/gitops-backend-operator-bundle:v0.0.3  --tag quay.io/redhat-developer/gitops-backend-operator-index:v0.0.3 --build-tool=docker
docker push quay.io/redhat-developer/gitops-backend-operator-index:v0.0.3
```

The Index image powers the listing of the Operator on OperatorHub.