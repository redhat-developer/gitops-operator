// Copyright 2020 ArgoCD Operator Developers
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package common

import (
	tlsutil "github.com/operator-framework/operator-sdk/pkg/tls"
	corev1 "k8s.io/api/core/v1"
)

const (
	// ArgoCDKeyAdminEnabled is the configuration key for the admin enabled setting..
	ArgoCDKeyAdminEnabled = "admin.enabled"

	// ArgoCDKeyApplicationInstanceLabelKey is the configuration key for the application instance label.
	ArgoCDKeyApplicationInstanceLabelKey = "application.instanceLabelKey"

	// ArgoCDKeyAdminPassword is the admin password key for labels.
	ArgoCDKeyAdminPassword = "admin.password"

	// ArgoCDKeyAdminPasswordMTime is the admin password last modified key for labels.
	ArgoCDKeyAdminPasswordMTime = "admin.passwordMtime"

	// ArgoCDKeyBackupKey is the "backup key" key for ConfigMaps.
	ArgoCDKeyBackupKey = "backup.key"

	// ArgoCDKeyConfigManagementPlugins is the configuration key for config management plugins.
	ArgoCDKeyConfigManagementPlugins = "configManagementPlugins"

	// ArgoCDKeyComponent is the resource component key for labels.
	ArgoCDKeyComponent = "app.kubernetes.io/component"

	// ArgoCDKeyDexOAuthRedirectURI is the key for the OAuth Redirect URI annotation.
	ArgoCDKeyDexOAuthRedirectURI = "serviceaccounts.openshift.io/oauth-redirecturi.argocd"

	// ArgoCDKeyDexConfig is the key for dex configuration.
	ArgoCDKeyDexConfig = "dex.config"

	// ArgoCDKeyFailureDomainZone is the failure-domain zone key for labels.
	ArgoCDKeyFailureDomainZone = "failure-domain.beta.kubernetes.io/zone"

	// ArgoCDKeyGATrackingID is the configuration key for the Google  Analytics Tracking ID.
	ArgoCDKeyGATrackingID = "ga.trackingid"

	// ArgoCDKeyGAAnonymizeUsers is the configuration key for the Google Analytics user anonymization.
	ArgoCDKeyGAAnonymizeUsers = "ga.anonymizeusers"

	// ArgoCDKeyGrafanaAdminUsername is the admin username key for labels.
	ArgoCDKeyGrafanaAdminUsername = "admin.username"

	// ArgoCDKeyGrafanaAdminPassword is the admin password key for labels.
	ArgoCDKeyGrafanaAdminPassword = "admin.password"

	// ArgoCDKeyGrafanaSecretKey is the "secret key" key for labels.
	ArgoCDKeyGrafanaSecretKey = "secret.key"

	// ArgoCDKeyHelpChatURL is the congifuration key for the help chat URL.
	ArgoCDKeyHelpChatURL = "help.chatUrl"

	// ArgoCDKeyHelpChatText is the congifuration key for the help chat text.
	ArgoCDKeyHelpChatText = "help.chatText"

	// ArgoCDKeyHostname is the resource hostname key for labels.
	ArgoCDKeyHostname = "kubernetes.io/hostname"

	// ArgoCDKeyIngressBackendProtocol is the backend-protocol key for labels.
	ArgoCDKeyIngressBackendProtocol = "nginx.ingress.kubernetes.io/backend-protocol"

	// ArgoCDKeyIngressClass is the ingress class key for labels.
	ArgoCDKeyIngressClass = "kubernetes.io/ingress.class"

	// ArgoCDKeyIngressSSLRedirect is the ssl force-redirect key for labels.
	ArgoCDKeyIngressSSLRedirect = "nginx.ingress.kubernetes.io/force-ssl-redirect"

	// ArgoCDKeyIngressSSLPassthrough is the ssl passthrough key for labels.
	ArgoCDKeyIngressSSLPassthrough = "nginx.ingress.kubernetes.io/ssl-passthrough"

	// ArgoCDKeyKustomizeBuildOptions is the configuration key for the kustomize build options.
	ArgoCDKeyKustomizeBuildOptions = "kustomize.buildOptions"

	// ArgoCDKeyMetrics is the resource metrics key for labels.
	ArgoCDKeyMetrics = "metrics"

	// ArgoCDKeyName is the resource name key for labels.
	ArgoCDKeyName = "app.kubernetes.io/name"

	// ArgoCDKeyOIDCConfig is the configuration key for the OIDC configuration.
	ArgoCDKeyOIDCConfig = "oidc.config"

	// ArgoCDKeyPartOf is the resource part-of key for labels.
	ArgoCDKeyPartOf = "app.kubernetes.io/part-of"

	// ArgoCDKeyStatefulSetPodName is the resource StatefulSet Pod Name key for labels.
	ArgoCDKeyStatefulSetPodName = "statefulset.kubernetes.io/pod-name"

	// ArgoCDKeyPrometheus is the resource prometheus key for labels.
	ArgoCDKeyPrometheus = "prometheus"

	// ArgoCDKeyRBACPolicyCSV is the configuration key for the Argo CD RBAC policy CSV.
	ArgoCDKeyRBACPolicyCSV = "policy.csv"

	// ArgoCDKeyRBACPolicyDefault is the configuration key for the Argo CD RBAC default policy.
	ArgoCDKeyRBACPolicyDefault = "policy.default"

	// ArgoCDKeyRBACScopes is the configuration key for the Argo CD RBAC scopes.
	ArgoCDKeyRBACScopes = "scopes"

	// ArgoCDKeyRelease is the prometheus release key for labels.
	ArgoCDKeyRelease = "release"

	// ArgoCDKeyResourceCustomizations is the configuration key for resource customizations.
	ArgoCDKeyResourceCustomizations = "resource.customizations"

	// ArgoCDKeyResourceExclusions is the configuration key for resource exclusions.
	ArgoCDKeyResourceExclusions = "resource.exclusions"

	// ArgoCDKeyResourceInclusions is the configuration key for resource inclusions.
	ArgoCDKeyResourceInclusions = "resource.inclusions"

	// ArgoCDKeyRepositories is the configuration key for repositories.
	ArgoCDKeyRepositories = "repositories"

	// ArgoCDKeyRepositoryCredentials is the configuration key for repository.credentials.
	ArgoCDKeyRepositoryCredentials = "repository.credentials"

	// ArgoCDKeyServerSecretKey is the server secret key property name for the Argo secret.
	ArgoCDKeyServerSecretKey = "server.secretkey"

	// ArgoCDKeyServerURL is the key for server url.
	ArgoCDKeyServerURL = "url"

	// ArgoCDKeySSHKnownHosts is the resource ssh_known_hosts key for labels.
	ArgoCDKeySSHKnownHosts = "ssh_known_hosts"

	// ArgoCDKeyStatusBadgeEnabled is the configuration key for enabling the status badge.
	ArgoCDKeyStatusBadgeEnabled = "statusbadge.enabled"

	// ArgoCDKeyTLSCACert is the key for TLS CA certificates.
	ArgoCDKeyTLSCACert = tlsutil.TLSCACertKey

	// ArgoCDKeyTLSCert is the key for TLS certificates.
	ArgoCDKeyTLSCert = corev1.TLSCertKey

	// ArgoCDKeyTLSPrivateKey is the key for TLS private keys.
	ArgoCDKeyTLSPrivateKey = corev1.TLSPrivateKeyKey

	// ArgoCDKeyTolerateUnreadyEndpounts is the resource tolerate unready endpoints key for labels.
	ArgoCDKeyTolerateUnreadyEndpounts = "service.alpha.kubernetes.io/tolerate-unready-endpoints"

	// ArgoCDKeyUsersAnonymousEnabled is the configuration key for anonymous user access.
	ArgoCDKeyUsersAnonymousEnabled = "users.anonymous.enabled"

	// ArgoCDDexImageEnvName is the environment variable used to get the image
	// to used for the Dex container.
	ArgoCDDexImageEnvName = "ARGOCD_DEX_IMAGE"

	// ArgoCDImageEnvName is the environment variable used to get the image
	// to used for the argocd container.
	ArgoCDImageEnvName = "ARGOCD_IMAGE"

	// ArgoCDRedisHAImageEnvName is the environment variable used to get the image
	// to used for the Redis HA Proxy image.
	ArgoCDRedisHAImageEnvName = "ARGOCD_REDIS_HA_IMAGE"

	// ArgoCDRedisImageEnvName is the environment variable used to get the image
	// to used for the Redis container.
	ArgoCDRedisImageEnvName = "ARGOCD_REDIS_IMAGE"

	// ArgoCDGrafanaImageEnvName is the environment variable used to get the image
	// to used for the Grafana container.
	ArgoCDGrafanaImageEnvName = "ARGOCD_GRAFANA_IMAGE"
)
