module github.com/redhat-developer/gitops-operator

go 1.16

require (
	github.com/argoproj-labs/argocd-operator v0.0.16-0.20210425113932-df1ca8d66a11
	github.com/coreos/prometheus-operator v0.40.0
	github.com/go-logr/logr v0.4.0
	github.com/google/go-cmp v0.5.2
	github.com/onsi/ginkgo v1.14.1
	github.com/onsi/gomega v1.10.2
	github.com/openshift/api v3.9.1-0.20190916204813-cdbe64fb0c91+incompatible
	github.com/operator-framework/api v0.3.7-0.20200528122852-759ca0d84007
	github.com/operator-framework/operator-sdk v0.18.0
	github.com/rakyll/statik v0.1.7
	go.etcd.io/etcd v0.0.0-20191023171146-3cf2f69b5738
	gotest.tools v2.2.0+incompatible
	k8s.io/api v0.19.2
	k8s.io/apimachinery v0.21.0
	k8s.io/client-go v12.0.0+incompatible
	sigs.k8s.io/controller-runtime v0.7.2
	sigs.k8s.io/yaml v1.2.0
)

replace (
	github.com/Azure/go-autorest => github.com/Azure/go-autorest v13.3.2+incompatible // Required by OLM
	k8s.io/client-go => k8s.io/client-go v0.19.2
	sigs.k8s.io/controller-runtime => sigs.k8s.io/controller-runtime v0.6.0
)
