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
  CODECOV_TOKEN="$(cat "${CODECOV_TOKEN_FILE}")"
  COMMIT="$(git rev-parse HEAD)"
  BRANCH="$(git rev-parse --abbrev-ref HEAD)"
  QUERY="token=${CODECOV_TOKEN}&commit=${COMMIT}&branch=${BRANCH}&flags=unit-tests"

  # Step 1: request an upload slot; response is two lines: report URL, S3 URL.
  RESPONSE=$(curl -sX POST -H 'Accept: text/plain' "https://codecov.io/upload/v4?${QUERY}")
  S3_URL=$(echo "${RESPONSE}" | sed -n 2p)
  if [[ -z "${S3_URL}" ]]; then
    echo "Codecov did not return an upload URL, aborting"
    exit 1
  fi

  # Step 2: PUT the coverage file to GCS (Codecov uses GCS, not AWS S3;
  # x-amz-storage-class is not supported and causes a 400).
  curl -fiX PUT --data-binary @cover.out \
    -H 'Content-Type: text/plain' \
    "${S3_URL}"
) || echo "Coverage upload to codecov.io failed, continuing"
