FROM openshift/origin-release:golang-1.16 AS builder

ENV LANG=en_US.utf8
ENV GIT_COMMITTER_NAME devtools
ENV GIT_COMMITTER_EMAIL devtools@redhat.com
LABEL com.redhat.delivery.appregistry=true

WORKDIR /go/src/github.com/redhat-developer/gitops-operator

COPY . .

ARG VERBOSE=2
RUN GIT_COMMIT=$(git rev-list -1 HEAD) && \
  go build -ldflags "-X main.GitCommit=$GIT_COMMIT" -o bin/gitops-operator cmd/manager/main.go


FROM registry.access.redhat.com/ubi8/ubi-minimal

LABEL com.redhat.delivery.appregistry=true
LABEL maintainer "Devtools <devtools@redhat.com>"
LABEL author "Shoubhik Bose <shbose@redhat.com>"
ENV LANG=en_US.utf8

COPY --from=builder /go/src/github.com/redhat-developer/gitops-operator/bin/gitops-operator /usr/local/bin/gitops-operator

# install redis artifacts
COPY build/redis /var/lib/redis

USER 10001

ENTRYPOINT [ "/usr/local/bin/gitops-operator" ]
