### Non OLM based operator e2e kuttl test

`run-non-olm-kuttl-test.sh` is a bash script utility, that can be used to run the end to end test for Openshift GitOps Operator without using the `Operator Lifecycle Manager (OLM)`. 

### Usage

The `run-non-olm-kuttl-test.sh` script needs to be run with argument specifying the test suite to be run with .

run-non-olm-kuttl-test.sh [ -t sequential|parallel|all ] 

Example 

`./run-non-olm-kuttl-test.sh -t parallel` will run the entire parallel test suite. By default it will run all the tests.

The  directories that are not needed for the nightly operator are excluded before running the tests.
If you want to add more excluded tests, you can do so by using an environment variable called `EXCLUDED_TESTS` like so,

`export EXCLUDED_TESTS="1-031_validate_toolchain 1-085_validate_dynamic_plugin_installation 1-038_validate_productized_images"`