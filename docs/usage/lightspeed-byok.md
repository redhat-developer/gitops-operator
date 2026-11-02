# Enhancing OpenShift Lightspeed with Custom Knowledge

## Overview
This guide explains how to extend the intelligence of OpenShift Lightspeed by integrating specialized knowledge for Argo CD, the Argo CD Operator, and the GitOps Operator. By configuring a custom Retrieval-Augmented Generation (RAG) database, you ensure the service provides precise, context-aware assistance for your GitOps queries.

The OpenShift Lightspeed service leverages Large Language Models (LLMs) to provide intelligent, context-aware responses. To ensure the model has deep expertise in your specific environment, you can use the Bring Your Own (BYO) Knowledge tool to integrate a Retrieval-Augmented Generation (RAG) database.

By connecting this database, you bridge the gap between general AI knowledge and specific technical documentation, ensuring more accurate troubleshooting and configuration advice.


## Pre-packaged Knowledge for GitOps
We have curated and optimized specialized knowledge sets for the following components:

* ArgoCD
* ArgoCD Operator
* Red Hat OpenShift GitOps Operator
* ArgoCD Agent

This knowledge is packaged into a container [image](https://quay.io/devtools_gitops/argocd_lightspeed_byok:v0.0.4) and can be configured in Lightspeed using the instructions below.

## Prerequisites

* You are logged in to the OpenShift Container Platform web console as a user account that has permission to create a cluster-scoped custom resource (CR) file, such as a user with the cluster-admin role.
* You have an LLM provider available for use with the OpenShift Lightspeed Service.
* You have installed and configured the [OpenShift Lightspeed Operator](https://docs.redhat.com/en/documentation/red_hat_openshift_lightspeed/1.0/html/configure/ols-configuring-openshift-lightspeed).

Modify the OLSconfig CR to deploy the pre-packaged RAG database alongside the existing one:

* In the OpenShift Container Platform web console, click Operators  >> Installed Operators.
* Select All Projects in the Project dropdown at the top of the screen.
* Click OpenShift Lightspeed Operator.
* Click OLSConfig, then click the cluster configuration instance in the list.
* Click the YAML tab.
* Insert the spec.ols.rag yaml code:
  Example OLSconfig CR file

```bash
apiVersion: ols.openshift.io/v1alpha1
kind: OLSConfig
metadata:
  name: cluster
spec:
  ols:
    rag:
      - image: quay.io/devtools_gitops/argocd_lightspeed_byok:v0.0.4
```

Note: Where image specifies the tag for the image that is present in the image registry so that the OpenShift Lightspeed Operator can access the custom content.
* Click Save.
