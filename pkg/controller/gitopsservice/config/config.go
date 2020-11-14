package config

import (
	"context"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	gitopsPrefix   = "gitops-prefixes"
	timeoutKey     = "timeout"
	defaultTimeout = "2m"
)

// Config represents a GitOps config
type Config struct {
	corev1.ConfigMap
}

// NewGitOpsConfig returns a new Config for GitOps
func NewGitOpsConfig() Config {
	c := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gitops-config",
			Namespace: "openshift-operators",
		},
		Data: map[string]string{
			timeoutKey: defaultTimeout,
		},
	}
	return Config{c}
}

// ExtractPrefixes will extract the prefixes list from config
func (c *Config) ExtractPrefixes() []string {
	prefixes := []string{}
	if prefixString, ok := c.Data[gitopsPrefix]; ok {
		return strings.Split(prefixString, ",")
	}
	return []string{}
}

// GetTimeout will return the timeout threshold for operator
func (c *Config) GetTimeout() (time.Duration, error) {
	if value, ok := c.Data[timeoutKey]; ok {
		return time.ParseDuration(value)
	}
	return 2 * time.Minute, nil
}

// Create the gitops-config map
func (c *Config) Create(ctx context.Context, client client.Client) error {
	return client.Create(ctx, &c.ConfigMap)
}

// GetLatest returns the latest gitops-config from the cluster
func (c *Config) GetLatest(ctx context.Context, client client.Client) error {
	return client.Get(ctx, types.NamespacedName{Name: c.Name, Namespace: c.Namespace}, &c.ConfigMap)
}
