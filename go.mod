module github.com/redhat-developer/gitops-operator

go 1.13

require (
	github.com/argoproj-labs/argocd-operator v0.0.15-0.20210301162004-70d7ccdfb761
	github.com/coreos/prometheus-operator v0.40.0
	github.com/go-logr/logr v0.1.0
	github.com/golang/protobuf v1.4.3 // indirect
	github.com/google/go-cmp v0.5.2
	github.com/keycloak/keycloak-operator v0.0.0-20210217134400-410337e825cc
	github.com/openshift/api v3.9.1-0.20190916204813-cdbe64fb0c91+incompatible
	github.com/operator-framework/api v0.3.18
	github.com/operator-framework/operator-sdk v0.18.2
	github.com/rakyll/statik v0.1.7
	github.com/spf13/pflag v1.0.5
	golang.org/x/net v0.0.0-20201110031124-69a78807bb2b // indirect
	golang.org/x/sys v0.0.0-20201112073958-5cba982894dd // indirect
	golang.org/x/text v0.3.4 // indirect
	golang.org/x/xerrors v0.0.0-20200804184101-5ec99f83aff1 // indirect
	google.golang.org/genproto v0.0.0-20201110150050-8816d57aaa9a // indirect
	google.golang.org/protobuf v1.25.0 // indirect
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
