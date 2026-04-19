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

# Upload coverage to codecov.io - failures here should not fail the build
(
  set +e
  CODECOV_TOKEN_FILE="/var/run/codecov-token/CODECOV_TOKEN"
  if [[ ! -f "${CODECOV_TOKEN_FILE}" ]]; then
    echo "Codecov token not found at ${CODECOV_TOKEN_FILE}, skipping upload"
    exit 0
  fi
  curl -OSs --fail-with-body https://cli.codecov.io/latest/linux/codecov
  chmod +x codecov
  CODECOV_TOKEN="$(cat "${CODECOV_TOKEN_FILE}")" ./codecov upload-process --flag unit-tests --file cover.out
) || echo "Coverage upload to codecov.io failed, continuing"
