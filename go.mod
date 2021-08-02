module github.com/redhat-developer/gitops-operator

go 1.16

require (
	github.com/argoproj-labs/argocd-operator v0.0.16-0.20210722160114-5fe7ef0c459f
	github.com/coreos/prometheus-operator v0.40.0
	github.com/go-logr/logr v0.1.0
	github.com/google/go-cmp v0.5.4
	github.com/onsi/ginkgo v1.14.1
	github.com/onsi/gomega v1.10.2
	github.com/openshift/api v3.9.1-0.20190916204813-cdbe64fb0c91+incompatible
	github.com/operator-framework/api v0.3.18
	github.com/rakyll/statik v0.1.7
	gotest.tools v2.2.0+incompatible
	k8s.io/api v0.18.3
	k8s.io/apimachinery v0.18.3
	k8s.io/client-go v12.0.0+incompatible
	sigs.k8s.io/controller-runtime v0.6.0
	sigs.k8s.io/yaml v1.2.0
)

replace (
	github.com/Azure/go-autorest => github.com/Azure/go-autorest v13.3.2+incompatible // Required by OLM
	github.com/opencontainers/runc => github.com/opencontainers/runc v1.0.0-rc95 // fix CVE-2019-16884 on runc v1.0.0-rc9
	k8s.io/client-go => k8s.io/client-go v0.18.3
)
