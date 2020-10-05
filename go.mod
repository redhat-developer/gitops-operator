module github.com/redhat-developer/gitops-operator

go 1.13

require (
	github.com/argoproj-labs/argocd-operator v0.0.13
	github.com/go-logr/logr v0.1.0
	github.com/google/go-cmp v0.4.0
	github.com/openshift/api v3.9.1-0.20190916204813-cdbe64fb0c91+incompatible
	github.com/operator-framework/operator-sdk v0.18.2
	github.com/spf13/pflag v1.0.5
	k8s.io/api v0.18.3
	k8s.io/apimachinery v0.18.3
	k8s.io/client-go v12.0.0+incompatible
	sigs.k8s.io/controller-runtime v0.6.0
)

replace (
	github.com/Azure/go-autorest => github.com/Azure/go-autorest v13.3.2+incompatible // Required by OLM
	k8s.io/client-go => k8s.io/client-go v0.18.2 // Required by prometheus-operator
)
