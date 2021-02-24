package rhsso

import (
	"fmt"

	keycloakv1alpha1 "github.com/keycloak/keycloak-operator/pkg/apis/keycloak/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// KeycloakNamespace defines the namespace where keycloak instance will be created.
	KeycloakNamespace = "openshift-gitops"
	// EnableExternalAccess enables external access for keycloak instance.
	EnableExternalAccess = true
	// KeycloakInstanceCount defines the instance count.
	KeycloakInstanceCount = 1
	// KeycloakArgoClient defines the keycloak client for openshift gitops.
	KeycloakArgoClient = "openshift-gitops"
	// ArgoBaseURL is the ArgoCD Base URL.
	ArgoBaseURL = "/applications"
	// ClientProtocol used by keycloak
	ClientProtocol = "openid-connect"
)

var log = logf.Log.WithName("cmd")

// NewKeycloakCR returns a keycloak reference optimized for use in OpenShift
func NewKeycloakCR(name string) *keycloakv1alpha1.Keycloak {
	l := make(map[string]string)
	l["app"] = fmt.Sprintf("keycloak-%s", name)

	return &keycloakv1alpha1.Keycloak{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Keycloak",
			APIVersion: "keycloak.org/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "keycloak-" + name,
			Namespace: KeycloakNamespace,
			Labels:    l,
		},
		Spec: keycloakv1alpha1.KeycloakSpec{
			Instances: KeycloakInstanceCount,
			ExternalAccess: keycloakv1alpha1.KeycloakExternalAccess{
				Enabled: EnableExternalAccess,
			},
		},
	}
}

// NewKeycloakRealmCR returns a keycloak realm reference optimized for use in OpenShift
func NewKeycloakRealmCR(name string) *keycloakv1alpha1.KeycloakRealm {
	s := map[string]string{
		"app": "keycloak-" + name,
	}
	l := map[string]string{
		"app": "keycloakrealm-" + name,
	}
	config := map[string]string{
		"clientId":     "oauthclient-" + name,
		"clientSecret": "admin",
		"baseUrl":      getBaseURL(),
		"defaultScope": "user:full",
	}

	return &keycloakv1alpha1.KeycloakRealm{
		TypeMeta: metav1.TypeMeta{
			Kind:       "KeycloakRealm",
			APIVersion: "keycloak.org/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "keycloakrealm-" + name,
			Namespace: KeycloakNamespace,
			Labels:    l,
		},
		Spec: keycloakv1alpha1.KeycloakRealmSpec{
			Realm: &keycloakv1alpha1.KeycloakAPIRealm{
				Realm:       name,
				Enabled:     true,
				DisplayName: name,
				IdentityProviders: []*keycloakv1alpha1.KeycloakIdentityProvider{
					&keycloakv1alpha1.KeycloakIdentityProvider{
						Alias:                     "openshift-v4",
						ProviderID:                "openshift-v4",
						DisplayName:               "Login with Openshift",
						InternalID:                "keycloak-broker",
						AddReadTokenRoleOnCreate:  true,
						FirstBrokerLoginFlowAlias: "first broker login",
						Config:                    config,
					},
				},
			},
			InstanceSelector: &metav1.LabelSelector{
				MatchLabels: s,
			},
		},
	}
}

// NewKeycloakClientCR returns a keycloak client reference optimized for use in OpenShift
func NewKeycloakClientCR(name string, argoRouteHost string) *keycloakv1alpha1.KeycloakClient {
	s := map[string]string{
		"app": "keycloakrealm-" + name,
	}
	l := map[string]string{
		"app": "keycloakclient-" + name,
	}

	return &keycloakv1alpha1.KeycloakClient{
		TypeMeta: metav1.TypeMeta{
			Kind:       "KeycloakClient",
			APIVersion: "keycloak.org/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "keycloakclient-" + name,
			Namespace: KeycloakNamespace,
			Labels:    l,
		},
		Spec: keycloakv1alpha1.KeycloakClientSpec{
			Client: &keycloakv1alpha1.KeycloakAPIClient{
				ClientID:                  KeycloakArgoClient,
				Secret:                    KeycloakArgoClient,
				Protocol:                  ClientProtocol,
				RootURL:                   fmt.Sprintf("https://%s", argoRouteHost),
				BaseURL:                   ArgoBaseURL,
				StandardFlowEnabled:       true,
				DirectAccessGrantsEnabled: true,
				PublicClient:              true,
			},
			RealmSelector: &metav1.LabelSelector{
				MatchLabels: s,
			},
		},
	}
}

func getBaseURL() string {
	cfg, err := config.GetConfig()
	if err != nil {
		log.Error(err, "")
	}
	return cfg.Host
}
