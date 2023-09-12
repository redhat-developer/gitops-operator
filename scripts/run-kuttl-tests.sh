#!/usr/bin/env bash

# fail if some commands fails
set -e

# Do not show token in CI log
set +x

# show commands
set -x
DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source $DIR/e2e-common.sh

testsuite="$1"
report=${report:-"json"}
current_time=${current_time:-$(date "+%Y.%m.%d-%H.%M.%S")}

# deletes the temp directory
cleanup() {      
  if test -f $WORK_DIR/results/kuttl-test.$report; then
	echo "Copying results"
	mv $WORK_DIR/results/kuttl-test.$report $DIR/results/$testsuite-results-$current_time.$report
	mv $WORK_DIR/results/$testsuite.log $DIR/results/$testsuite-results-$current_time.log
  fi

  rm -rf "$WORK_DIR"
  echo "Deleted temp working directory $WORK_DIR"
}

# Simple wrapper to run the acceptance tests for GitOps Operator


TEST_BASE_DIR=${TEST_BASE_DIR:-"$DIR/../test/openshift/e2e"}

run_parallel() {
	if test -f $WORK_DIR/results/kuttl-test.$report; then
		rm -f $WORK_DIR/results/kuttl-test.$report
	fi

	echo "Running parallel test suite"
	kubectl kuttl test $TEST_BASE_DIR/parallel --artifacts-dir $WORK_DIR/results --config $DIR/../test/openshift/e2e/parallel/kuttl-test.yaml --report $report 2>&1 | tee $WORK_DIR/results/$testsuite.log 
	if [ ${PIPESTATUS[0]} != 0 ]; then
	   failed=1
	fi	
}

run_sequential() {
	if test -f $WORK_DIR/results/kuttl-test.$report; then
		rm -f $WORK_DIR/results/kuttl-test.$report
	fi

	echo "Running sequential test suite"
    kubectl kuttl test $TEST_BASE_DIR/sequential --artifacts-dir $WORK_DIR/results --config $DIR/../test/openshift/e2e/sequential/kuttl-test.yaml --report $report 2>&1 | tee $WORK_DIR/results/$testsuite.log
	if [ ${PIPESTATUS[0]} != 0 ]; then
	   failed=1
	fi
}

run_cmd_silent() {
	$* >/dev/null 2>&1
	return $?
}

check_prereqs() {
	if ! run_cmd_silent jq --version; then
		echo "jq not found" >&2
		return 1
	fi
	if ! run_cmd_silent curl --version; then
		echo "curl not found" >&2
		return 1
	fi
	if ! run_cmd_silent oc version --client; then
		echo "oc not found" >&2
		return 1
	fi
	if ! run_cmd_silent kubectl version --client; then
		echo "kubectl not found" >&2
		return 1
	fi
	if ! run_cmd_silent oc project openshift-gitops; then
		echo "OpenShift GitOps seems not to be installed in your cluster" >&2
		echo "No openshift-gitops namespace found in your cluster, or cluster down." >&2
		return 1
	fi
	return 0
}

failed=0

if ! check_prereqs; then
	echo "Pre-requisites not met. Exiting."
	exit 1
fi

# the temp directory used, within $DIR
# omit the -p parameter to create a temporal directory in the default location
WORK_DIR=`mktemp -d -p "$DIR"`

# check if tmp dir was created
if [[ ! "$WORK_DIR" || ! -d "$WORK_DIR" ]]; then
  echo "Could not create temp dir"
  exit 1
fi

# register the cleanup function to be called on the EXIT signal
trap cleanup EXIT

# Handle ctrl+c
trap unexpectedError INT

mkdir -p $WORK_DIR/results || exit 1
mkdir -p $DIR/results || exit 1

case "$testsuite" in
"parallel")
    header "Running $testsuite tests"
	run_parallel $2
	;;
"sequential")
    header "Running $testsuite tests"
	run_sequential $2
	;;
"all")
    header "Running $testsuite tests"
	run_parallel
	run_sequential
	;;
*)
	echo "USAGE: $0 (parallel|sequential|all)" >&2
	exit 1
esac

(( failed )) && fail_test "$testsuite tests failed"
success $testsuite
