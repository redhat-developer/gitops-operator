module github.com/redhat-developer/gitops-operator

go 1.13

require (
	github.com/brancz/gojsontoyaml v0.0.0-20191212081931-bf2969bbd742 // indirect
	github.com/brancz/kube-rbac-proxy v0.5.0 // indirect
	github.com/coreos/etcd v3.3.15+incompatible // indirect
	github.com/go-logr/zapr v0.1.1 // indirect
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510 // indirect
	github.com/hashicorp/go-version v1.1.0 // indirect
	github.com/iancoleman/strcase v0.0.0-20190422225806-e506e3ef7365 // indirect
	github.com/jsonnet-bundler/jsonnet-bundler v0.3.1 // indirect
	github.com/kylelemons/godebug v0.0.0-20170820004349-d65d576e9348 // indirect
	github.com/markbates/inflect v1.0.4 // indirect
	github.com/mattn/go-isatty v0.0.12 // indirect
	github.com/mitchellh/hashstructure v0.0.0-20170609045927-2bca23e0e452 // indirect
	github.com/openshift/api v3.9.0+incompatible
	github.com/openshift/prom-label-proxy v0.1.1-0.20191016113035-b8153a7f39f1 // indirect
	github.com/operator-framework/operator-sdk v0.18.2
	github.com/prometheus/client_golang v1.5.1 // indirect
	github.com/rogpeppe/go-internal v1.5.0 // indirect
	github.com/sirupsen/logrus v1.5.0 // indirect
	github.com/spf13/pflag v1.0.5
	github.com/thanos-io/thanos v0.11.0 // indirect
	go.uber.org/zap v1.14.1 // indirect
	golang.org/x/net v0.0.0-20200301022130-244492dfa37a // indirect
	gomodules.xyz/jsonpatch/v3 v3.0.1 // indirect
	gonum.org/v1/netlib v0.0.0-20190331212654-76723241ea4e // indirect
	helm.sh/helm/v3 v3.2.0 // indirect
	k8s.io/api v0.18.2
	k8s.io/apimachinery v0.18.2
	k8s.io/client-go v12.0.0+incompatible
	k8s.io/kube-state-metrics v1.7.2 // indirect
	k8s.io/kubectl v0.18.2 // indirect
	rsc.io/letsencrypt v0.0.3 // indirect
	sigs.k8s.io/controller-runtime v0.6.0
	sigs.k8s.io/kubebuilder v1.0.9-0.20200513134826-f07a0146a40b // indirect
)

replace (
	github.com/Azure/go-autorest => github.com/Azure/go-autorest v13.3.2+incompatible // Required by OLM
	k8s.io/client-go => k8s.io/client-go v0.18.2 // Required by prometheus-operator
)
