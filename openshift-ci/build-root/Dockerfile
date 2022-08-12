# Dockerfile to bootstrap build and test in openshift-ci

FROM registry.ci.openshift.org/openshift/release:golang-1.18

RUN yum -y install dnf httpd-tools podman docker

# Install kubectl-kuttl tool which is used in e2e-tests
RUN curl -sSL -o /usr/local/bin/kubectl-kuttl https://github.com/kudobuilder/kuttl/releases/download/v0.12.1/kubectl-kuttl_0.12.1_linux_x86_64 && \
     chmod +x /usr/local/bin/kubectl-kuttl


# Install argocd cli tool which is used in e2e-tests
RUN curl -sSL -o /usr/local/bin/argocd https://github.com/argoproj/argo-cd/releases/latest/download/argocd-linux-amd64 && \
     chmod +x /usr/local/bin/argocd

# Install Kustomize
RUN curl -L -o /usr/local/bin/kustomize https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize/v4.4.1/kustomize_kustomize.v4.4.1_linux_amd64 && \
     chmod +x /usr/local/bin/kustomize

# Install operator-sdk
RUN curl -L -o /usr/local/bin/operator-sdk https://github.com/operator-framework/operator-sdk/releases/download/v1.22.2/operator-sdk-v1.22.2-x86_64-linux-gnu && \
     chmod +x /usr/local/bin/operator-sdk


