# Dockerfile to bootstrap build and test in openshift-ci

FROM quay.io/devtools_gitops/go-toolset:1.26.2
USER root

ARG OPERATOR_SDK_VERSION=1.35.0
ARG KUSTOMIZE_VERSION=v5.8.1
ARG KUSTOMIZE_SHA256="029a7f0f4e1932c52a0476cf02a0fd855c0bb85694b82c338fc648dcb53a819d"

# Install kubectl tool which is used in e2e-tests
RUN curl -sSL -o /usr/local/bin/kubectl "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl" && \
     chmod +x /usr/local/bin/kubectl

# Install argocd cli tool which is used in e2e-tests
RUN curl -sSL -o /usr/local/bin/argocd https://github.com/argoproj/argo-cd/releases/latest/download/argocd-linux-amd64 && \
     chmod +x /usr/local/bin/argocd

# Install Kustomize with checksum validation
RUN curl -sSL -o kustomize.tar.gz https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize%2F${KUSTOMIZE_VERSION}/kustomize_${KUSTOMIZE_VERSION}_linux_amd64.tar.gz && \
    echo "${KUSTOMIZE_SHA256}  kustomize.tar.gz" | sha256sum -c - && \
    tar -xzf kustomize.tar.gz -C /usr/local/bin && \
    rm kustomize.tar.gz

# Install operator-sdk
RUN curl -L -o /usr/local/bin/operator-sdk https://github.com/operator-framework/operator-sdk/releases/download/v${OPERATOR_SDK_VERSION}/operator-sdk_linux_amd64 && \
    chmod +x /usr/local/bin/operator-sdk