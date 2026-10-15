package argoutil

import (
	"reflect"
	"strings"
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
		name      string
		groups    []configv1.TLSGroup
		expected  string
		wantErr   bool
		errSubstr string
	}{
		{
			name:     "empty groups",
			groups:   nil,
			expected: "",
		},
		{
			name: "supported groups joined with colon",
			groups: []configv1.TLSGroup{
				configv1.TLSGroupX25519,
				configv1.TLSGroupSecP256r1,
				configv1.TLSGroupSecP384r1,
				configv1.TLSGroupSecP521r1,
				configv1.TLSGroupX25519MLKEM768,
			},
			expected: "X25519:CurveP256:CurveP384:CurveP521:X25519MLKEM768",
		},
		{
			name: "unsupported groups skipped when others remain",
			groups: []configv1.TLSGroup{
				configv1.TLSGroupSecP256r1MLKEM768,
				configv1.TLSGroupX25519,
			},
			expected: "X25519",
		},
		{
			name: "all unsupported groups returns error",
			groups: []configv1.TLSGroup{
				configv1.TLSGroupSecP256r1MLKEM768,
			},
			wantErr:   true,
			errSubstr: "none of the configured groups are supported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MapOpenShiftTLSGroupsToGoCurves(tt.groups, "test-component")
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Fatalf("expected error containing %q, got %v", tt.errSubstr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.expected {
				t.Fatalf("expected %q got %q", tt.expected, got)
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
