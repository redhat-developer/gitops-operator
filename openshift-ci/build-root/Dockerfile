# Dockerfile to bootstrap build and test in openshift-ci

FROM registry.ci.openshift.org/openshift/release:golang-1.19

ARG KUTTL_VERSION=0.15.0
ARG OPERATOR_SDK_VERSION=1.11.0
ARG OS_TYPE="linux"
ARG ARCH_X86="x86_64"
ARG ARCH_AMD="amd64"

RUN yum -y install dnf httpd-tools podman docker

# Install kubectl tool which is used in e2e-tests
RUN curl -sSL -o /usr/local/bin/kubectl "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl" && \
     chmod +x /usr/local/bin/kubectl

# Install kubectl-kuttl tool which is used in e2e-tests
RUN curl -sSL -o /usr/local/bin/kubectl-kuttl https://github.com/kudobuilder/kuttl/releases/download/v${KUTTL_VERSION}/kubectl-kuttl_${KUTTL_VERSION}_${OS_TYPE}_${ARCH_X86} && \
     chmod +x /usr/local/bin/kubectl-kuttl

# Install argocd cli tool which is used in e2e-tests
RUN curl -sSL -o /usr/local/bin/argocd https://github.com/argoproj/argo-cd/releases/latest/download/argocd-${OS_TYPE}-${ARCH_AMD} && \
     chmod +x /usr/local/bin/argocd

# Install Kustomize
RUN wget https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh && \
    bash install_kustomize.sh /usr/local/bin && rm install_kustomize.sh

# Install operator-sdk
RUN curl -L -o /usr/local/bin/operator-sdk https://github.com/operator-framework/operator-sdk/releases/download/v${OPERATOR_SDK_VERSION}/operator-sdk_${OS_TYPE}_${ARCH_AMD} && \
    chmod +x /usr/local/bin/operator-sdk
