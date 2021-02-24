package rhsso

import (
	"fmt"

	oauthv1 "github.com/openshift/api/oauth/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NewOAuthClient returns a openshift OAuthClient reference
func NewOAuthClient(name string, keycloakRouteHost string) *oauthv1.OAuthClient {
	redirectURI := fmt.Sprintf("https://%s/auth/realms/%s/broker/openshift-v4/endpoint",
		keycloakRouteHost, name)
	return &oauthv1.OAuthClient{
		TypeMeta: metav1.TypeMeta{
			Kind:       "OAuthClient",
			APIVersion: "oauth.openshift.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "oauthclient-" + name,
			Namespace: KeycloakNamespace,
		},
		Secret:       "admin",
		RedirectURIs: []string{redirectURI},
		GrantMethod:  "prompt",
	}
}
