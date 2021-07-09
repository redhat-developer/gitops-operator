E2E_TEST_DIR=test/e2e
NON_DEFAULT_E2E_TEST_DIR=test/nondefaulte2e
OPERATOR_SDK?=operator-sdk

default: test

.PHONY: test
test:
	go test `go list ./... | grep -v ${E2E_TEST_DIR} | grep -v ${NON_DEFAULT_E2E_TEST_DIR}`

.PHONY: prepare-test-cluster
prepare-test-cluster:
	. ./scripts/prepare-test-cluster.sh

.PHONY: test-e2e
test-e2e:
	. ./scripts/run_e2e_tests.sh

.PHONY: gomod_tidy
gomod_tidy:
	go mod tidy

.PHONY: gofmt
gofmt:
	go fmt -x ./...

.PHONY: run-local
run-local:
	${OPERATOR_SDK} run --local --watch-namespace ""

# Please install GitOps operator before running this target
.PHONY: test-e2e-on-operator
test-e2e-on-operator:
	CGO_ENABLED=0 SKIP_OPERATOR_DEPLOYMENT=true operator-sdk test local ./test/e2e  --verbose --no-setup
