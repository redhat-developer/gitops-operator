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
	TokenSecretName     = "gitops-ai-controller-notifications-token" // #nosec G101 -- resource name, not a credential
	NotificationsSecret = "argocd-notifications-secret"              // #nosec G101 -- resource name, not a credential
	TriggerTokenKey     = "gitops-ai-controller-token"               // #nosec G101 -- Secret data key, not a credential
	CAConfigMap         = "gitops-ai-controller-serving-ca"
	ServiceName         = "gitops-ai-controller"
)
