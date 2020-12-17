E2E_TEST_DIR=test/e2e

default: test

.PHONY: test
test:
	go test `go list ./... | grep -v ${E2E_TEST_DIR}`

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
	operator-sdk run --local --watch-namespace ""
