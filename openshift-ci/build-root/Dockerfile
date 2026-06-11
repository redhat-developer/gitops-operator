# Dockerfile to bootstrap build and test in openshift-ci

FROM quay.io/devtools_gitops/go-toolset:1.26.2
USER root

ARG OPERATOR_SDK_VERSION=1.35.0
ARG KUSTOMIZE_VERSION=v5.8.1

# Install kubectl tool which is used in e2e-tests
RUN curl -sSL -o /usr/local/bin/kubectl "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl" && \
     chmod +x /usr/local/bin/kubectl

# Install argocd cli tool which is used in e2e-tests
RUN curl -sSL -o /usr/local/bin/argocd https://github.com/argoproj/argo-cd/releases/latest/download/argocd-linux-amd64 && \
     chmod +x /usr/local/bin/argocd

# Install Kustomize
RUN curl -sSL https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize%2F${KUSTOMIZE_VERSION}/kustomize_${KUSTOMIZE_VERSION}_linux_amd64.tar.gz | tar -xz -C /usr/local/bin

# Install operator-sdk
RUN curl -L -o /usr/local/bin/operator-sdk https://github.com/operator-framework/operator-sdk/releases/download/v${OPERATOR_SDK_VERSION}/operator-sdk_linux_amd64 && \
    chmod +x /usr/local/bin/operator-sdk