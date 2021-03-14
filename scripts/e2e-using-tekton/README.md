# End-to-end tests using Tekton

## About
Run operator-sdk end-to-end tests written in ./test/e2e/ on a cluster with OpenShift GitOps installed. 

## Steps 

1. Install OpenShift GitOps on your cluster

2. Create the following namespace to run your Pipelines.

```
oc new-project gitpos-e2e-test
``` 

3. Run the tests by 

```
kubect apply -f e2e-pipeline.yaml
```

3. Sit back and watch it run,


![test-pipeline](/docs/assets/test-pipeline.png)
