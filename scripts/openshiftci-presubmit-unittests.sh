#!/bin/sh

# fail if some commands fails
set -e
# show commands
set -x

export ARTIFACTS_DIR="/tmp/artifacts"
export CUSTOM_HOMEDIR=$ARTIFACTS_DIR
export PATH=$PATH:$GOPATH/bin

go env
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

# Run unit
make test
