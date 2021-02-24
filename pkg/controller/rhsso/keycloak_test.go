package rhsso

import (
	"testing"

	keycloakv1alpha1 "github.com/keycloak/keycloak-operator/pkg/apis/keycloak/v1alpha1"
	"gotest.tools/assert"
)

var (
	externalAccess = keycloakv1alpha1.KeycloakExternalAccess{
		Enabled: true,
	}

	testConfig = map[string]string{
		"clientId":     "oauthclient-openshift-gitops",
		"clientSecret": "admin",
		"baseUrl":      getBaseURL(),
		"defaultScope": "user:full",
	}

	dummyRealmData = &keycloakv1alpha1.KeycloakAPIRealm{
		Realm:       "openshift-gitops",
		Enabled:     true,
		DisplayName: "openshift-gitops",
		IdentityProviders: []*keycloakv1alpha1.KeycloakIdentityProvider{
			&keycloakv1alpha1.KeycloakIdentityProvider{
				Alias:                     "openshift-v4",
				ProviderID:                "openshift-v4",
				DisplayName:               "Login with Openshift",
				InternalID:                "keycloak-broker",
				AddReadTokenRoleOnCreate:  true,
				FirstBrokerLoginFlowAlias: "first broker login",
				Config:                    testConfig,
			},
		},
	}

	dummyClientData = &keycloakv1alpha1.KeycloakAPIClient{
		ClientID:                  "openshift-gitops",
		Secret:                    "openshift-gitops",
		Protocol:                  "openid-connect",
		RootURL:                   "https://argocd.com",
		BaseURL:                   "/applications",
		StandardFlowEnabled:       true,
		DirectAccessGrantsEnabled: true,
		PublicClient:              true,
	}
)

func TestKeycloakInstanceCreation(t *testing.T) {
	testKeycloakInstance := NewKeycloakCR("openshift-gitops")
	assert.Equal(t, testKeycloakInstance.Name, "keycloak-openshift-gitops")
	assert.Equal(t, testKeycloakInstance.Namespace, "openshift-gitops")
	assert.Equal(t, testKeycloakInstance.Spec.Instances, 1)
	assert.Equal(t, testKeycloakInstance.Spec.ExternalAccess, externalAccess)
}

func TestKeycloakRealmCreation(t *testing.T) {
	testKeycloakRealm := NewKeycloakRealmCR("openshift-gitops")
	assert.Equal(t, testKeycloakRealm.Name, "keycloakrealm-openshift-gitops")
	assert.Equal(t, testKeycloakRealm.Namespace, "openshift-gitops")
	assert.DeepEqual(t, testKeycloakRealm.Spec.Realm, dummyRealmData)
}

func TestKeycloakClientCreation(t *testing.T) {
	testKeycloakClient := NewKeycloakClientCR("openshift-gitops", "argocd.com")
	assert.Equal(t, testKeycloakClient.Name, "keycloakclient-openshift-gitops")
	assert.Equal(t, testKeycloakClient.Namespace, "openshift-gitops")
	assert.DeepEqual(t, testKeycloakClient.Spec.Client, dummyClientData)
}
