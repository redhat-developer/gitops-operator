# Dockerfile to bootstrap build and test in openshift-ci

FROM registry.svc.ci.openshift.org/openshift/release:golang-1.13

RUN yum -y install dnf httpd-tools

RUN dnf -y install 'dnf-command(config-manager)'

RUN dnf -y config-manager --add-repo https://cli.github.com/packages/rpm/gh-cli.repo

RUN dnf -y install gh
