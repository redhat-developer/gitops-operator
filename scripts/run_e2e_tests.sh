#!/bin/bash

# show commands
set -x

E2E_TEST_DIR=./test/e2e
NON_DEFAULT_E2E_TEST_DIR=./test/nondefaulte2e

echo "Running e2e tests"
go test $E2E_TEST_DIR -coverprofile cover.out -ginkgo.v
Teststatus=$?
if [ $Teststatus_e2e -ne "0" ]; then
    exit $Teststatus_e2e
fi

echo "Running e2e tests (DISABLE_DEFAULT_ARGOCD_INSTANCE=true)"
go test $NON_DEFAULT_E2E_TEST_DIR -coverprofile cover.out -ginkgo.v
Teststatus_e2e_nondefault=$?
if [ $Teststatus_e2e_nondefault -ne "0" ]; then
    exit $Teststatus_e2e_nondefault
fi
