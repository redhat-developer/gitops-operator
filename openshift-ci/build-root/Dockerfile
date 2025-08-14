# Dockerfile to bootstrap build and test in openshift-ci

FROM registry.ci.openshift.org/ocp/builder:rhel-9-golang-1.24-openshift-4.20
USER root

ARG KUBECTL_KUTTL_VERSION=0.12.1
ARG OPERATOR_SDK_VERSION=1.35.0

# Install kubectl tool which is used in e2e-tests
RUN curl -sSL -o /usr/local/bin/kubectl "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl" && \
     chmod +x /usr/local/bin/kubectl

# Install kubectl-kuttl tool which is used in e2e-tests
RUN curl -sSL -o /usr/local/bin/kubectl-kuttl https://github.com/kudobuilder/kuttl/releases/download/v${KUBECTL_KUTTL_VERSION}/kubectl-kuttl_${KUBECTL_KUTTL_VERSION}_linux_x86_64 && \
     chmod +x /usr/local/bin/kubectl-kuttl

# Install argocd cli tool which is used in e2e-tests
RUN curl -sSL -o /usr/local/bin/argocd https://github.com/argoproj/argo-cd/releases/latest/download/argocd-linux-amd64 && \
     chmod +x /usr/local/bin/argocd

# Install Kustomize
RUN wget https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh && \
    bash install_kustomize.sh /usr/local/bin && rm install_kustomize.sh

# Install operator-sdk
RUN curl -L -o /usr/local/bin/operator-sdk https://github.com/operator-framework/operator-sdk/releases/download/v${OPERATOR_SDK_VERSION}/operator-sdk_linux_amd64 && \
    chmod +x /usr/local/bin/operator-sdk

# Install jq
RUN curl -L -o /usr/local/bin/jq https://github.com/jqlang/jq/releases/latest/download/jq-linux-amd64 && \
    chmod +x /usr/local/bin/jq