package argoutil

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	argoproj "github.com/argoproj-labs/gitops-operator/argocd-operator/api/v1beta1"
	"github.com/argoproj-labs/gitops-operator/argocd-operator/pkg/tlsprofile"
)

// TestGetRedisHAProxyConfigRenderedTLSValues verifies that TLS minVersion, ciphers,
// and curve preferences are correctly rendered in the final HAProxy configuration.
func TestGetRedisHAProxyConfigRenderedTLSValues(t *testing.T) {
	wd, err := os.Getwd()
	require.NoError(t, err)
	t.Setenv("REDIS_CONFIG_PATH", filepath.Join(wd, "../../build", "redis"))
	tests := []struct {
		name                    string
		useTLS                  bool
		centralTLSConfigProfile tlsprofile.TLSConfigProfile
		expectedInOutput        []string
		notExpectedInOutput     []string
		validatePattern         *regexp.Regexp
	}{
		{
			name:   "TLS 1.2 with two cipher suites",
			useTLS: true,
			centralTLSConfigProfile: tlsprofile.TLSConfigProfile{
				MinVersion: configv1.VersionTLS12,
				Ciphers: []string{
					"ECDHE-RSA-AES256-GCM-SHA384",
					"ECDHE-RSA-AES128-GCM-SHA256",
				},
			},
			expectedInOutput: []string{
				"ssl-default-bind-options ssl-min-ver TLSv1.2",
				"ssl-default-server-options ssl-min-ver TLSv1.2",
				"ssl-default-bind-ciphers ECDHE-RSA-AES256-GCM-SHA384:ECDHE-RSA-AES128-GCM-SHA256",
				"ssl-default-server-ciphers ECDHE-RSA-AES256-GCM-SHA384:ECDHE-RSA-AES128-GCM-SHA256",
				"ssl-default-bind-ciphersuites ECDHE-RSA-AES256-GCM-SHA384:ECDHE-RSA-AES128-GCM-SHA256",
				"ssl-default-server-ciphersuites ECDHE-RSA-AES256-GCM-SHA384:ECDHE-RSA-AES128-GCM-SHA256",
			},
			notExpectedInOutput: []string{
				"ssl-default-bind-curves",
				"ssl-default-server-curves",
			},
			validatePattern: regexp.MustCompile(`ssl-min-ver\s+TLSv1\.2`),
		},
		{
			name:   "TLS 1.3 with modern cipher suites",
			useTLS: true,
			centralTLSConfigProfile: tlsprofile.TLSConfigProfile{
				MinVersion: configv1.VersionTLS13,
				Ciphers: []string{
					"TLS_AES_256_GCM_SHA384",
					"TLS_CHACHA20_POLY1305_SHA256",
					"TLS_AES_128_GCM_SHA256",
				},
			},
			expectedInOutput: []string{
				"ssl-default-bind-options ssl-min-ver TLSv1.3",
				"ssl-default-server-options ssl-min-ver TLSv1.3",
				"ssl-default-bind-ciphersuites TLS_AES_256_GCM_SHA384:TLS_CHACHA20_POLY1305_SHA256:TLS_AES_128_GCM_SHA256",
				"ssl-default-server-ciphersuites TLS_AES_256_GCM_SHA384:TLS_CHACHA20_POLY1305_SHA256:TLS_AES_128_GCM_SHA256",
			},
			notExpectedInOutput: []string{
				"ssl-default-bind-ciphers ",
				"ssl-default-server-ciphers ",
			},
			validatePattern: regexp.MustCompile(`ssl-min-ver\s+TLSv1\.3`),
		},
		{
			name:   "TLS enabled with minimum version only",
			useTLS: true,
			centralTLSConfigProfile: tlsprofile.TLSConfigProfile{
				MinVersion: configv1.VersionTLS13,
			},
			expectedInOutput: []string{
				"ssl-default-bind-options ssl-min-ver TLSv1.3",
				"ssl-default-server-options ssl-min-ver TLSv1.3",
			},
			notExpectedInOutput: []string{
				"ssl-default-bind-ciphers ",
				"ssl-default-server-ciphers ",
				"ssl-default-bind-ciphersuites",
				"ssl-default-server-ciphersuites",
				"ssl-default-bind-curves",
				"ssl-default-server-curves",
			},
		},
		{
			name:   "TLS with curve preferences maps OpenShift groups to HAProxy names",
			useTLS: true,
			centralTLSConfigProfile: tlsprofile.TLSConfigProfile{
				MinVersion: configv1.VersionTLS12,
				CurvePreferences: []string{
					"X25519MLKEM768",
					"X25519",
					"secp256r1",
					"secp384r1",
				},
			},
			expectedInOutput: []string{
				"ssl-default-bind-options ssl-min-ver TLSv1.2",
				"ssl-default-server-options ssl-min-ver TLSv1.2",
				"ssl-default-bind-curves X25519MLKEM768:X25519:P-256:P-384",
				"ssl-default-server-curves X25519MLKEM768:X25519:P-256:P-384",
			},
			notExpectedInOutput: []string{
				"ssl-default-bind-ciphers ",
				"ssl-default-server-ciphers ",
				"ssl-default-bind-ciphersuites",
				"ssl-default-server-ciphersuites",
				"secp256r1",
				"secp384r1",
			},
		},
		{
			name:   "TLS 1.3 with ciphers and curve preferences",
			useTLS: true,
			centralTLSConfigProfile: tlsprofile.TLSConfigProfile{
				MinVersion: configv1.VersionTLS13,
				Ciphers: []string{
					"TLS_AES_128_GCM_SHA256",
					"TLS_AES_256_GCM_SHA384",
				},
				CurvePreferences: []string{
					"X25519",
					"secp256r1",
				},
			},
			expectedInOutput: []string{
				"ssl-default-bind-options ssl-min-ver TLSv1.3",
				"ssl-default-server-options ssl-min-ver TLSv1.3",
				"ssl-default-bind-ciphersuites TLS_AES_128_GCM_SHA256:TLS_AES_256_GCM_SHA384",
				"ssl-default-server-ciphersuites TLS_AES_128_GCM_SHA256:TLS_AES_256_GCM_SHA384",
				"ssl-default-bind-curves X25519:P-256",
				"ssl-default-server-curves X25519:P-256",
			},
			notExpectedInOutput: []string{
				"ssl-default-bind-ciphers ",
				"ssl-default-server-ciphers ",
			},
		},
		{
			name:                    "TLS disabled",
			useTLS:                  false,
			centralTLSConfigProfile: tlsprofile.TLSConfigProfile{},
			notExpectedInOutput: []string{
				"ca-base",
				"ssl-default-bind-options",
				"ssl-default-server-options",
				"ssl-default-bind-ciphers",
				"ssl-default-server-ciphers",
				"ssl-default-bind-ciphersuites",
				"ssl-default-server-ciphersuites",
				"ssl-default-bind-curves",
				"ssl-default-server-curves",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cr := &argoproj.ArgoCD{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argocd",
					Namespace: "argocd",
				},
			}

			var capturedVars map[string]string
			original := loadTemplateFile
			defer func() {
				loadTemplateFile = original
			}()
			loadTemplateFile = func(path string, vars map[string]string) (string, error) {
				capturedVars = maps.Clone(vars)
				return original(path, vars)
			}
			result := GetRedisHAProxyConfig(cr, tt.useTLS, tt.centralTLSConfigProfile)
			t.Logf("Rendered HAProxy config:\n%s", result)
			for _, expected := range tt.expectedInOutput {
				assert.Contains(t, result, expected)
			}
			for _, unexpected := range tt.notExpectedInOutput {
				assert.NotContains(t, result, unexpected)
			}
			if tt.validatePattern != nil {
				assert.Regexp(t, tt.validatePattern, result)
			}
			if tt.useTLS {
				expectedVersion := TLSProtocolVersionString(tt.centralTLSConfigProfile.MinVersion)
				if expectedVersion != "" {
					assert.Equal(t, expectedVersion, capturedVars["TLSMinVersion"])
				}
				if len(tt.centralTLSConfigProfile.Ciphers) > 0 {
					assert.Equal(
						t,
						strings.Join(tt.centralTLSConfigProfile.Ciphers, ":"),
						capturedVars["TLSCiphers"],
					)
				} else {
					assert.Empty(t, capturedVars["TLSCiphers"])
				}
				expectedCurves := MapCurvePreferencesToHAProxyCurves(tt.centralTLSConfigProfile.CurvePreferences)
				if expectedCurves != "" {
					assert.Equal(t, expectedCurves, capturedVars["TLSCurves"])
				} else {
					assert.Empty(t, capturedVars["TLSCurves"])
				}
			}
		})
	}
}

func TestMapCurvePreferencesToHAProxyCurves(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected string
	}{
		{
			name:     "empty",
			input:    nil,
			expected: "",
		},
		{
			name:     "classical OpenShift groups",
			input:    []string{"X25519", "secp256r1", "secp384r1", "secp521r1"},
			expected: "X25519:P-256:P-384:P-521",
		},
		{
			name:     "maps OpenShift groups including PQC",
			input:    []string{"X25519MLKEM768", "X25519", "SecP256r1MLKEM768", "secp256r1"},
			expected: "X25519MLKEM768:X25519:SecP256r1MLKEM768:P-256",
		},
		{
			name:     "skips unknown groups",
			input:    []string{"UnknownGroup", "X25519"},
			expected: "X25519",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, MapCurvePreferencesToHAProxyCurves(tt.input))
		})
	}
}
