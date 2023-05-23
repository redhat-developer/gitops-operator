.PHONY: e2e-tests
e2e-tests:
	@echo "Running GitOps Operator E2E tests..."
	. ./../scripts/run-kuttl-tests.sh all

.PHONY: e2e-tests-sequential
e2e-tests-sequential:
	@echo "Running GitOps Operator sequential E2E tests..."
	. ./../scripts/run-kuttl-tests.sh nightly-sequential

.PHONY: e2e-tests-parallel
e2e-tests-parallel:
	@echo "Running GitOps Operator parallel E2E tests..."
	. ./../scripts/run-kuttl-tests.sh nightly-parallel