module github.com/redhat-developer/gitops-operator

go 1.16

require (
	github.com/argoproj-labs/argocd-operator v0.0.15-0.20210405194250-a5171dbe9363
	github.com/coreos/prometheus-operator v0.40.0
	github.com/go-logr/logr v0.1.0
	github.com/google/go-cmp v0.4.0
	github.com/openshift/api v3.9.1-0.20190916204813-cdbe64fb0c91+incompatible
	github.com/operator-framework/api v0.3.18
	github.com/operator-framework/operator-sdk v0.18.2
	github.com/rakyll/statik v0.1.7
	github.com/spf13/pflag v1.0.5
	gotest.tools v2.2.0+incompatible
	k8s.io/api v0.18.3
	k8s.io/apimachinery v0.18.3
	k8s.io/client-go v12.0.0+incompatible
	sigs.k8s.io/controller-runtime v0.6.0
	sigs.k8s.io/yaml v1.2.0
)

replace (
	github.com/Azure/go-autorest => github.com/Azure/go-autorest v13.3.2+incompatible // Required by OLM
	k8s.io/client-go => k8s.io/client-go v0.18.2
)
