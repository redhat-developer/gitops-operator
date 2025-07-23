#!/usr/bin/env bash

# fail if some commands fails
set -e
# show commands
set -x

export PATH=$PATH:$GOPATH/bin

go env
go mod vendor
if [[ $(go fmt `go list ./... | grep -v vendor`) ]]; then
    echo "not well formatted sources are found"
    exit 1
fi
go mod tidy
if [[ ! -z $(git status -s) ]]
then
    echo "Go mod state is not clean."
    exit 1
fi

# without vendoring upgrade-rollouts-manager, make manifests runs into an error
cd hack/upgrade-rollouts-manager
go mod vendor
cd ../..

# Run unit
make test
