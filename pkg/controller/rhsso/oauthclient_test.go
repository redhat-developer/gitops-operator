package rhsso

import (
	"fmt"
	"testing"

	"gotest.tools/assert"
)

var (
	testRedirectURI = fmt.Sprintf("https://%s/auth/realms/%s/broker/openshift-v4/endpoint",
		"keycloak.com", "openshift-gitops")
)

func TestOAuthClientCreation(t *testing.T) {
	testOAuthClient := NewOAuthClient("openshift-gitops", "keycloak.com")
	assert.Equal(t, testOAuthClient.Name, "oauthclient-openshift-gitops")
	assert.Equal(t, testOAuthClient.Namespace, "openshift-gitops")
	assert.Equal(t, testOAuthClient.Secret, "admin")
	assert.DeepEqual(t, testOAuthClient.RedirectURIs, []string{testRedirectURI})
}
