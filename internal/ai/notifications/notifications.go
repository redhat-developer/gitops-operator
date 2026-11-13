// Package notifications provides AI-specific Argo CD notification configuration.
// It manages the trigger, template, service, and subscription entries injected into
// the default NotificationsConfiguration CR, as well as propagation of the SA token
// and (optionally) the TLS serving CA into the Argo CD notification secrets.
package notifications

const (
	TriggerKey          = "trigger.on-gitops-ai-diagnose"
	TemplateKey         = "template.gitops-ai-diagnose"
	ServiceKey          = "service.webhook.gitops-ai-controller"
	SubscriptionKey     = "subscriptions"
	ConfigurationName   = "default-notifications-configuration"
	TokenSecretName     = "gitops-ai-controller-notifications-token"
	NotificationsSecret = "argocd-notifications-secret"
	TriggerTokenKey     = "gitops-ai-controller-token"
	CAConfigMap         = "gitops-ai-controller-serving-ca"
	ServiceName         = "gitops-ai-controller"
)
