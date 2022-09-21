# Dockerfile to bootstrap build and test in openshift-ci

FROM registry.ci.openshift.org/openshift/release:golang-1.18

RUN yum -y install dnf httpd-tools podman docker

# Install kubectl tool which is used in e2e-tests
RUN curl -sSL -o /usr/local/bin/kubectl "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl" && \
     chmod +x /usr/local/bin/kubectl

# Install kubectl-kuttl tool which is used in e2e-tests
ARG KUBECTL_KUTTL_VERSION=0.12.1
RUN curl -sSL -o /usr/local/bin/kubectl-kuttl https://github.com/kudobuilder/kuttl/releases/download/v${KUBECTL_KUTTL_VERSION}/kubectl-kuttl_${KUBECTL_KUTTL_VERSION}_linux_x86_64 && \
     chmod +x /usr/local/bin/kubectl-kuttl


# Install argocd cli tool which is used in e2e-tests
RUN curl -sSL -o /usr/local/bin/argocd https://github.com/argoproj/argo-cd/releases/latest/download/argocd-linux-amd64 && \
     chmod +x /usr/local/bin/argocd

# Install Kustomize
ARG KUSTOMIZE_VERSION=4.4.1
RUN curl -L -o /usr/local/bin/kustomize https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize/v{$KUSTOMIZE_VERSION}/kustomize_kustomize.v${KUSTOMIZE_VERSION}_linux_amd64 && \
     chmod +x /usr/local/bin/kustomize

# Install operator-sdk
ARG OPERATOR_SDK_VERSION=1.22.2
RUN curl -L -o /usr/local/bin/operator-sdk https://github.com/operator-framework/operator-sdk/releases/download/v${OPERATOR_SDK_VERSION}/operator-sdk_linux_amd64 && \
    chmod +x /usr/local/bin/operator-sdk

