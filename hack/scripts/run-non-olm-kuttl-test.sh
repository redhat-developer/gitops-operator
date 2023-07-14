#!/usr/bin/env bash

export NON_OLM="true"

set -x

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
sequential_suite=$ROOT/../../test/openshift/e2e/sequential/
parallel_suite=$ROOT/../../test/openshift/e2e/parallel/

testsuite="all"

# Get test suite argument
while getopts ":t:" opt; do
  case ${opt} in
    t) testsuite=$OPTARG;;
    \?) echo "Please provide options sequential/parallel/all -$OPTARG" >&2;;
  esac
done

# these tests will be removed while running non-olm operator test
# 1-031_validate_toolchain
# 1-085_validate_dynamic_plugin_installation
# 1-036_validate_keycloak_resource_reqs
# 1-038_validate_productized_images
# 1-051-validate_csv_permissions
# 1-073_validate_rhsso
# 1-077_validate_disable_dex_removed
# 1-090_validate_permissions

filenames="1-031_validate_toolchain 1-085_validate_dynamic_plugin_installation 1-036_validate_keycloak_resource_reqs 1-038_validate_productized_images 1-051-validate_csv_permissions 1-073_validate_rhsso 1-077_validate_disable_dex_removed 1-090_validate_permissions"

if [ -n "$EXCLUDED_TESTS" ]; then
  filenames="${filenames} ${EXCLUDED_TESTS}"
fi

temp_dir=$(mktemp -d "${TMPDIR:-"/tmp"}/kuttl-non-olm-tests-XXXXXXX")

cp -R "$sequential_suite" "$temp_dir"

cp -R "$parallel_suite" "$temp_dir"

for dir in $filenames ; do
  if [ -d "$temp_dir/sequential/$dir" ]; then
    echo "Deleting directory $dir"
    rm -rf "$temp_dir/sequential/$dir"
  elif [ -d "$temp_dir/parallel/$dir" ]; then
    echo "Deleting directory $dir"
    rm -rf "$temp_dir/parallel/$dir"  
  else
    echo "Directory $dir does not exist"
  fi
done

#replace the namespace for assert in test file

sed -i 's/openshift-operators/gitops-operator-system/g' $temp_dir/sequential/1-018_validate_disable_default_instance/02-assert.yaml \
  $temp_dir/sequential/1-035_validate_argocd_secret_repopulate/04-check_controller_pod_status.yaml

cleanup() {
  rm -rf "$temp_dir"
  echo "Deleted temp test directory $temp_dir"
}

trap cleanup EXIT INT

script="$ROOT/../../scripts/run-kuttl-tests.sh"

# Check if the file exists before executing it
if [ -e "$script" ]; then
    chmod +x "$script"   
else
    echo "ERROR: Script file '$script' not found."
fi

export TEST_BASE_DIR=$temp_dir

# Run the specific test suite
case "$testsuite" in
"parallel")
    source "$script" parallel
	;;
"sequential")
    source "$script" sequential
	;;
"all")
    source "$script" all
	;;  
*)
	echo "USAGE: $0 (parallel|sequential|all)" >&2
	exit 1
esac