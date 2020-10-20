package dependency

import (
	v1 "github.com/operator-framework/api/pkg/operators/v1"
	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

type operatorResource struct {
	namespace     string
	subscription  string
	operatorGroup string
}

func (o *operatorResource) GetSubscription() *v1alpha1.Subscription {
	return newSubscription(o.namespace, o.subscription)
}

func (o *operatorResource) GetOperatorGroup() *v1.OperatorGroup {
	return newOperatorGroup(o.namespace, o.operatorGroup)
}

func (o *operatorResource) GetNamespace() *corev1.Namespace {
	return newNamespace(o.namespace)
}

func newArgoCDOperator(prefix string) operatorResource {
	return operatorResource{
		namespace:     addPrefixIfNecessary(prefix, "argocd"),
		subscription:  "argocd-operator",
		operatorGroup: "argocd-operator-group",
	}
}

func newSealedSecretsOperator(prefix string) operatorResource {
	return operatorResource{
		namespace:     addPrefixIfNecessary(prefix, "cicd"),
		subscription:  "sealed-secrets-operator-helm",
		operatorGroup: "sealed-secrets-operator-group",
	}
}
