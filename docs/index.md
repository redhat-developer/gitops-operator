# GitOps Operator

An operator for managing Argo CD clusters, for OpenShift and Kubernetes

## Overview

The GitOps Operator manages the full lifecycle for [Argo CD](https://argoproj.github.io/argo-cd/) and its
components. The operator's goal is to automate the tasks required when operating an Argo CD cluster.

Beyond installation, the operator helps to automate the process of upgrading, backing up and restoring as needed and
remove the human as much as possible. In addition, the operator aims to provide deep insights into the Argo CD
environment by configuring Prometheus to aggregate, visualize and expose the metrics already exported by
Argo CD.

## Features

The operator aims to provide the following:

* Easy configuration and installation of the Git Ops components with sane defaults to get up and running quickly.
    * The Argo CD itself
    * [Argo CD Image Updater](https://argocd-image-updater.readthedocs.io/en/stable/)
    * [Argo Rollouts](https://argoproj.github.io/rollouts/)
    * [GitOps Promoter](https://gitops-promoter.readthedocs.io/en/latest/)
* Provide seamless upgrades to the operated components.
* Aggregate and expose the metrics for Argo CD and the operator itself using Prometheus.
* Autoscale the Argo CD components as necessary to handle variability in demand.

