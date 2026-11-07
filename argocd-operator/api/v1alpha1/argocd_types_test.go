package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/redhat-developer/gitops-operator/argocd-operator/common"
)

func Test_ArgoCD_ApplicationInstanceLabelKey(t *testing.T) {
	cr := &ArgoCD{}
	cr.Spec.ApplicationInstanceLabelKey = "my.corp/instance"
	assert.Equal(t, cr.ApplicationInstanceLabelKey(), "my.corp/instance")
	cr = &ArgoCD{}
	assert.Equal(t, cr.ApplicationInstanceLabelKey(), common.ArgoCDDefaultApplicationInstanceLabelKey)
}

func Test_ResourceTrackingMethodToString(t *testing.T) {
	testdata := []struct {
		rtm ResourceTrackingMethod
		str string
	}{
		{ResourceTrackingMethodLabel, stringResourceTrackingMethodLabel},
		{ResourceTrackingMethodAnnotation, stringResourceTrackingMethodAnnotation},
		{ResourceTrackingMethodAnnotationAndLabel, stringResourceTrackingMethodAnnotationAndLabel},
		{20, stringResourceTrackingMethodAnnotation},
	}
	for _, tt := range testdata {
		rtm := tt.rtm
		assert.Equal(t, tt.str, rtm.String())
	}
}

func Test_ParseResourceTrackingMethod(t *testing.T) {
	testdata := []struct {
		rtm ResourceTrackingMethod
		str string
	}{
		{ResourceTrackingMethodLabel, stringResourceTrackingMethodLabel},
		{ResourceTrackingMethodAnnotation, stringResourceTrackingMethodAnnotation},
		{ResourceTrackingMethodAnnotationAndLabel, stringResourceTrackingMethodAnnotationAndLabel},
		{ResourceTrackingMethodInvalid, "invalid"},
		{ResourceTrackingMethodAnnotation, ""},
	}
	for _, tt := range testdata {
		assert.Equal(t, tt.rtm, ParseResourceTrackingMethod(tt.str))
	}
}

func Test_ArgoCDNotifications_SourceNamespaces(t *testing.T) {
	notifications := ArgoCDNotifications{
		SourceNamespaces: []string{"ns1", "ns2"},
	}
	assert.Equal(t, []string{"ns1", "ns2"}, notifications.SourceNamespaces)

	// Test omitempty (zero value)
	emptyNotifications := ArgoCDNotifications{}
	assert.Nil(t, emptyNotifications.SourceNamespaces)
}

func Test_PrincipalHASpec_Fields(t *testing.T) {
	haEnabled := true
	adminPort := int32(8405)

	ha := PrincipalHASpec{
		Enabled:                      &haEnabled,
		PreferredRole:                "primary",
		PeerAddress:                  "argocd-agent-principal-replica:8443",
		AllowedReplicationClients:    []string{"replica-identity"},
		AdminPort:                    &adminPort,
		AdminAuth:                    "mtls:subject:CN=ha-admin",
		ReplicationInitialAckTimeout: "5m",
	}

	assert.True(t, *ha.Enabled)
	assert.Equal(t, "primary", ha.PreferredRole)
	assert.Equal(t, "argocd-agent-principal-replica:8443", ha.PeerAddress)
	assert.Equal(t, []string{"replica-identity"}, ha.AllowedReplicationClients)
	assert.Equal(t, int32(8405), *ha.AdminPort)
	assert.Equal(t, "mtls:subject:CN=ha-admin", ha.AdminAuth)
	assert.Equal(t, "5m", ha.ReplicationInitialAckTimeout)
}

func Test_PrincipalSpec_HA_OmitEmpty(t *testing.T) {
	// When HA is not set, the field should be nil
	principal := PrincipalSpec{}
	assert.Nil(t, principal.HA)

	// When HA is set, it should be accessible
	haEnabled := false
	principal.HA = &PrincipalHASpec{
		Enabled:       &haEnabled,
		PreferredRole: "replica",
	}
	assert.NotNil(t, principal.HA)
	assert.False(t, *principal.HA.Enabled)
	assert.Equal(t, "replica", principal.HA.PreferredRole)
}
