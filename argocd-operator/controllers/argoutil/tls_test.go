package argoutil

import (
	"reflect"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
)

func TestTLSProtocolVersionString(t *testing.T) {
	tests := []struct {
		input    configv1.TLSProtocolVersion
		expected string
	}{
		{configv1.VersionTLS10, "1.0"},
		{configv1.VersionTLS11, "1.1"},
		{configv1.VersionTLS12, "1.2"},
		{configv1.VersionTLS13, "1.3"},
		{configv1.TLSProtocolVersion("invalid"), ""},
	}

	for _, tt := range tests {
		got := TLSProtocolVersionString(tt.input)

		if got != tt.expected {
			t.Fatalf("expected %s got %s", tt.expected, got)
		}
	}
}

func TestRedisTLSProtocolVersionString(t *testing.T) {
	tests := []struct {
		input    configv1.TLSProtocolVersion
		expected string
	}{
		{configv1.VersionTLS10, "TLSv1"},
		{configv1.VersionTLS11, "TLSv1.1"},
		{configv1.VersionTLS12, "TLSv1.2"},
		{configv1.VersionTLS13, "TLSv1.3"},
		{configv1.TLSProtocolVersion("invalid"), ""},
	}

	for _, tt := range tests {
		got := RedisTLSProtocolVersionString(tt.input)

		if got != tt.expected {
			t.Fatalf("expected %s got %s", tt.expected, got)
		}
	}
}

func TestMapCipherSuites(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name: "valid mappings",
			input: []string{
				"ECDHE-RSA-AES128-GCM-SHA256",
				"TLS_AES_128_GCM_SHA256",
			},
			expected: []string{
				"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
				"TLS_AES_128_GCM_SHA256",
			},
		},
		{
			name: "invalid cipher",
			input: []string{
				"INVALID",
			},
			expected: []string{},
		},
		{
			name: "partial valid",
			input: []string{
				"INVALID",
				"ECDHE-RSA-AES256-GCM-SHA384",
			},
			expected: []string{
				"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MapCipherSuites(tt.input)

			if !reflect.DeepEqual(got, tt.expected) {
				t.Fatalf("expected %+v got %+v", tt.expected, got)
			}
		})
	}
}

func TestMapOpenShiftTLSGroupsToGoCurves(t *testing.T) {
	tests := []struct {
		name        string
		input       []configv1.TLSGroup
		expected    string
		expectError bool
	}{
		{
			name:     "empty",
			input:    nil,
			expected: "",
		},
		{
			name: "maps OpenShift classical and PQC groups to Go names",
			input: []configv1.TLSGroup{
				configv1.TLSGroupX25519MLKEM768,
				configv1.TLSGroupX25519,
				configv1.TLSGroupSecP256r1,
				configv1.TLSGroupSecP384r1,
			},
			expected: "X25519MLKEM768:X25519:CurveP256:CurveP384",
		},
		{
			name: "skips unsupported groups when some remain supported",
			input: []configv1.TLSGroup{
				configv1.TLSGroupSecP256r1MLKEM768,
				configv1.TLSGroupX25519,
				configv1.TLSGroup("UnknownGroup"),
			},
			expected: "X25519",
		},
		{
			name: "errors when all configured groups are unsupported",
			input: []configv1.TLSGroup{
				configv1.TLSGroupSecP256r1MLKEM768,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MapOpenShiftTLSGroupsToGoCurves(tt.input, "test")
			if tt.expectError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.expected {
				t.Fatalf("expected %s got %s", tt.expected, got)
			}
		})
	}
}

func TestAgentTLSProtocolVersionString(t *testing.T) {
	tests := []struct {
		input    configv1.TLSProtocolVersion
		expected string
	}{
		{configv1.VersionTLS10, ""},
		{configv1.VersionTLS11, "tls1.1"},
		{configv1.VersionTLS12, "tls1.2"},
		{configv1.VersionTLS13, "tls1.3"},
	}

	for _, tt := range tests {
		got := AgentTLSProtocolVersionString(tt.input)

		if got != tt.expected {
			t.Fatalf("expected %s got %s", tt.expected, got)
		}
	}
}

func TestMapOpenShiftTLSGroupsToOpenSSL(t *testing.T) {
	tests := []struct {
		name     string
		input    []configv1.TLSGroup
		expected string
	}{
		{
			name:     "empty",
			input:    nil,
			expected: "",
		},
		{
			name: "classical OpenShift groups",
			input: []configv1.TLSGroup{
				configv1.TLSGroupX25519,
				configv1.TLSGroupSecP256r1,
				configv1.TLSGroupSecP384r1,
				configv1.TLSGroupSecP521r1,
			},
			expected: "X25519:secp256r1:secp384r1:secp521r1",
		},
		{
			name: "OpenShift groups including PQC",
			input: []configv1.TLSGroup{
				configv1.TLSGroupX25519MLKEM768,
				configv1.TLSGroupX25519,
				configv1.TLSGroupSecP256r1MLKEM768,
				configv1.TLSGroupSecP256r1,
			},
			expected: "X25519MLKEM768:X25519:SecP256r1MLKEM768:secp256r1",
		},
		{
			name: "skips unknown groups",
			input: []configv1.TLSGroup{
				configv1.TLSGroup("UnknownGroup"),
				configv1.TLSGroupX25519,
			},
			expected: "X25519",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MapOpenShiftTLSGroupsToOpenSSL(tt.input, "test")
			if got != tt.expected {
				t.Fatalf("expected %s got %s", tt.expected, got)
			}
		})
	}
}
