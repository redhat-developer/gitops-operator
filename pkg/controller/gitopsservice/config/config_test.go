package config

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	v1 "k8s.io/api/core/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestExtractPrefixes(t *testing.T) {
	t.Run("With no prefix", func(t *testing.T) {
		config := NewGitOpsConfig()

		want := []string{}
		got := config.ExtractPrefixes()

		if diff := cmp.Diff(got, want); diff != "" {
			t.Fatalf("extractPrefixes failed: %s", diff)
		}
	})

	t.Run("With multiple prefixes", func(t *testing.T) {
		cm := v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gitops-config",
			},
			Data: map[string]string{
				"gitops-repo-1_prefix": "",
				"gitops-repo-2_prefix": "alpha",
				"gitops-repo-3_prefix": "beta",
				"gitops-repo-4_prefix": "gamma",
			},
		}
		config := Config{cm}

		want := []string{"", "alpha", "beta", "gamma"}
		got := config.ExtractPrefixes()

		if diff := cmp.Diff(got, want); diff != "" {
			t.Fatalf("extractPrefixes failed: %s", diff)
		}
	})
}
