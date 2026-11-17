package argocd

import (
	"fmt"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"

	argoproj "github.com/redhat-developer/gitops-operator/argocd-operator/api/v1beta1"
	"github.com/redhat-developer/gitops-operator/argocd-operator/common"
	"github.com/redhat-developer/gitops-operator/argocd-operator/controllers/argoutil"
	tlsProfile "github.com/redhat-developer/gitops-operator/argocd-operator/pkg/tlsprofile"
)

// getDexContainerImage will return the container image for the Dex server.
//
// There are three possible options for configuring the image, and this is the
// order of preference.
//
// 1. from the Spec, the spec.sso.dex field has an image and version to use for
// generating an image reference.
// 2. from the Environment, this looks for the `ARGOCD_DEX_IMAGE` field and uses
// that if the spec is not configured.
// 3. the default is configured in common.ArgoCDDefaultDexVersion and
// common.ArgoCDDefaultDexImage.
func getDexContainerImage(cr *argoproj.ArgoCD) string {
	defaultImg, defaultTag := false, false

	img := ""
	tag := ""

	if cr.Spec.SSO != nil && cr.Spec.SSO.Dex != nil && cr.Spec.SSO.Dex.Image != "" {
		img = cr.Spec.SSO.Dex.Image
	}

	if img == "" {
		img = common.ArgoCDDefaultDexImage
		defaultImg = true
	}

	if cr.Spec.SSO != nil && cr.Spec.SSO.Dex != nil && cr.Spec.SSO.Dex.Version != "" {
		tag = cr.Spec.SSO.Dex.Version
	}

	if tag == "" {
		tag = common.ArgoCDDefaultDexVersion
		defaultTag = true
	}
	if e := os.Getenv(common.ArgoCDDexImageEnvName); e != "" && (defaultTag && defaultImg) {
		return e
	}
	return argoutil.CombineImageTag(img, tag)
}

// getDexOAuthRedirectURI will return the OAuth redirect URI for the Dex server.
func (r *ReconcileArgoCD) getDexOAuthRedirectURI(cr *argoproj.ArgoCD) (string, error) {
	uri, err := r.getArgoServerURI(cr)
	if err != nil {
		return "", err
	}
	return uri + common.ArgoCDDefaultDexOAuthRedirectPath, nil
}

// getDexOAuthClientID will return the OAuth client ID for the given ArgoCD.
func getDexOAuthClientID(cr *argoproj.ArgoCD) string {
	return fmt.Sprintf("system:serviceaccount:%s:%s", cr.Namespace, getServiceAccountName(cr.Name, common.ArgoCDDefaultDexServiceAccountName))
}

// getDexResources will return the ResourceRequirements for the Dex container.
func getDexResources(cr *argoproj.ArgoCD) corev1.ResourceRequirements {

	resources := corev1.ResourceRequirements{}

	// Allow override of resource requirements from CR
	if cr.Spec.SSO != nil && cr.Spec.SSO.Dex != nil && cr.Spec.SSO.Dex.Resources != nil {
		resources = *cr.Spec.SSO.Dex.Resources
	}

	return resources
}

func getDexConfig(cr *argoproj.ArgoCD, centralTLSConfig tlsProfile.TLSConfigProfile) string {
	config := common.ArgoCDDefaultDexConfig

	// Inject cluster TLS profile into Dex web config (tlsMinVersion, tlsCiphers, tlsCurvePreferences).
	if !centralTLSConfig.DisableClusterTLSProfile {
		if webTLS := buildDexWebTLSConfig(centralTLSConfig); webTLS != "" {
			if config != "" {
				config += "\n"
			}
			config += webTLS
		}
	}

	// Allow override of config from CR
	if cr.Spec.ExtraConfig["dex.config"] != "" {
		config = cr.Spec.ExtraConfig["dex.config"]
	} else if cr.Spec.SSO != nil && cr.Spec.SSO.Dex != nil && len(cr.Spec.SSO.Dex.Config) > 0 {
		config = cr.Spec.SSO.Dex.Config
	}
	return config
}

// buildDexWebTLSConfig builds the Dex `web:` YAML fragment from the cluster TLS profile.
// Empty string is returned when there is nothing to configure.
func buildDexWebTLSConfig(centralTLSConfig tlsProfile.TLSConfigProfile) string {
	const component = "dex"

	var b strings.Builder
	hasContent := false

	if v := argoutil.TLSProtocolVersionString(centralTLSConfig.MinVersion); v != "" {
		if !hasContent {
			b.WriteString("web:")
			hasContent = true
		}
		b.WriteString(fmt.Sprintf("\n  tlsMinVersion: %q", v))
	}

	if ciphers := argoutil.MapCipherSuites(centralTLSConfig.Ciphers); len(ciphers) > 0 {
		// Dex rejects TLS 1.3 cipher suites; they are not independently configurable.
		// Only filter them when TLS versions below 1.3 are used (same pattern as
		// BuildTLSArgsFromClusterTLSProfile). When MinVersion is TLS 1.3 the
		// tlsCiphers setting has no effect, so omit them entirely.
		if centralTLSConfig.MinVersion != "VersionTLS13" {
			tls13Ciphers := map[string]bool{
				"TLS_AES_128_GCM_SHA256":       true,
				"TLS_AES_256_GCM_SHA384":       true,
				"TLS_CHACHA20_POLY1305_SHA256": true,
			}
			filtered := make([]string, 0, len(ciphers))
			for _, cipher := range ciphers {
				if !tls13Ciphers[cipher] {
					filtered = append(filtered, cipher)
				}
			}
			ciphers = filtered
		} else {
			ciphers = nil
		}
		if len(ciphers) > 0 {
			if !hasContent {
				b.WriteString("web:")
				hasContent = true
			}
			b.WriteString("\n  tlsCiphers:")
			for _, cipher := range ciphers {
				b.WriteString(fmt.Sprintf("\n    - %q", cipher))
			}
		}
	}

	curves, err := argoutil.MapOpenShiftTLSGroupsToGoCurves(centralTLSConfig.CurvePreferences, component)
	if err != nil {
		// Do not fail reconciliation for incompatible curve preferences. Keep min
		// version/ciphers and fall back to the component's default curve selection
		// so disable/delete paths (and other reconcile work) can still proceed.
		log.Info("skipping TLS curve preferences; falling back to component defaults",
			"component", component, "error", err.Error())
	} else if curves != "" {
		if !hasContent {
			b.WriteString("web:")
			hasContent = true
		}
		b.WriteString("\n  tlsCurvePreferences:")
		for _, curve := range strings.Split(curves, ":") {
			b.WriteString(fmt.Sprintf("\n    - %q", curve))
		}
	}

	if !hasContent {
		return ""
	}
	return b.String()
}
