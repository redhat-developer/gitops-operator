package argocd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	routev1 "github.com/openshift/api/route/v1"
	argov1beta1api "github.com/redhat-developer/gitops-operator/argocd-operator/api/v1beta1"
	portforwardFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/portforward"
	routeFixture "github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/route"
	"github.com/redhat-developer/gitops-operator/test/openshift/e2e/ginkgo/fixture/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ArgoCDCLISession is an isolated 'argocd' CLI session: it keeps its own 'argocd' CLI config file (the file that
// stores CLI login/context state), stored in a uniquely-named temporary directory.
//
// The 'argocd' CLI config file defaults to a single, fixed path on disk (e.g. ~/.config/argocd/config). If two
// callers (for example, two tests running in parallel) log in via the CLI at the same time using the default
// config path, they will corrupt each other's login state. ArgoCDCLISession avoids this by passing
// '--config <path-to-this-session's-config-file>' to every 'argocd' CLI invocation made through it, so that
// multiple sessions (and thus multiple logins) can safely run concurrently.
//
// Tests running in test/openshift/e2e/ginkgo/parallel that need to use the 'argocd' CLI MUST use an
// ArgoCDCLISession, rather than running the 'argocd' CLI binary directly.
type ArgoCDCLISession struct {
	// configPath is the path to this session's 'argocd' CLI config file, within a uniquely-named temporary directory.
	configPath string
}

// NewArgoCDCLISession creates a new ArgoCDCLISession with its own, uniquely-named temporary directory to store the
// 'argocd' CLI config file. The temporary directory is automatically removed at the end of the running Ginkgo spec
// (via DeferCleanup).
func NewArgoCDCLISession() (*ArgoCDCLISession, error) {
	GinkgoHelper()

	configDir, err := os.MkdirTemp("", fmt.Sprintf("argocd-cli-config-%s-", uuid.NewUUID()))
	if err != nil {
		return nil, fmt.Errorf("unable to create temporary directory for Argo CD CLI session: %w", err)
	}

	DeferCleanup(func() {
		_ = os.RemoveAll(configDir)
	})

	return &ArgoCDCLISession{configPath: filepath.Join(configDir, "config")}, nil
}

// RunArgoCDCLI runs an 'argocd' CLI command using this session's isolated CLI config file, so that it is safe to
// run concurrently with 'argocd' CLI commands from other ArgoCDCLISessions (for example, from other parallel tests).
func (s *ArgoCDCLISession) RunArgoCDCLI(args ...string) (string, error) {

	cmdArgs := append([]string{"argocd", "--config", s.configPath}, args...)

	if len(args) > 0 && args[0] == "login" {
		GinkgoWriter.Println("executing command: argocd login (...)")
	} else {
		GinkgoWriter.Println("executing command", cmdArgs)
	}

	// #nosec G204
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)

	output, err := cmd.CombinedOutput()
	GinkgoWriter.Println(string(output))

	return string(output), err
}

// LogInToDefaultArgoCDInstance logs in (via this session) to the default 'openshift-gitops' Argo CD instance, via its
// Route. The default instance has its server Route enabled out of the box, so this only works on OpenShift clusters.
func (s *ArgoCDCLISession) LogInToDefaultArgoCDInstance() error {
	return s.LogInToArgoCDInstanceViaRoute(&argov1beta1api.ArgoCD{
		ObjectMeta: metav1.ObjectMeta{Name: "openshift-gitops", Namespace: "openshift-gitops"},
	})
}

// LogInToArgoCDInstanceViaRoute logs in (via this session) to the given Argo CD instance via its Route.
// Only works on OpenShift clusters, and only if the instance's server Route is enabled.
func (s *ArgoCDCLISession) LogInToArgoCDInstanceViaRoute(argoCD *argov1beta1api.ArgoCD) error {
	k8sClient, _, err := utils.GetE2ETestKubeClientWithError()
	if err != nil {
		return err
	}

	route := &routev1.Route{ObjectMeta: metav1.ObjectMeta{Name: argoCD.Name + "-server", Namespace: argoCD.Namespace}}

	Eventually(func() error {
		return k8sClient.Get(context.Background(), client.ObjectKeyFromObject(route), route)
	}, "3m", "2s").Should(Succeed())

	Eventually(route, "3m", "2s").Should(routeFixture.HaveAdmittedIngress())

	secretName := argoCD.Name + "-cluster"
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: argoCD.Namespace}}
	if err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(secret), secret); err != nil {
		return fmt.Errorf("unable to locate %q Secret", secretName)
	}

	// Note: '--skip-test-tls' parameter was added in Feb 2025, to work around OpenShift Routes not supporting HTTP2 by default, along with Argo CD upstream bugs https://github.com/argoproj/argo-cd/issues/21764, and https://github.com/argoproj/argo-cd/issues/20121
	output, err := s.RunArgoCDCLI("login", route.Spec.Host, "--username", "admin", "--password", string(secret.Data["admin.password"]), "--insecure", "--skip-test-tls")
	if err != nil {
		return err
	}

	if !strings.Contains(output, "'admin:login' logged in successfully") {
		return fmt.Errorf("unable to log in to Argo CD")
	}

	return nil
}

// LogInToArgoCDInstanceWithoutRoute logs in (via this session) to the given Argo CD instance via a port-forward
// instead of an OpenShift Route, so it works on xks clusters.
// instanceName is the ArgoCD CR name (e.g. "openshift-gitops"); namespace is its namespace.
// The returned cancel func stops the port-forward; call it (or defer it) after all argocd
// CLI calls in the test are done.
func (s *ArgoCDCLISession) LogInToArgoCDInstanceWithoutRoute(instanceName, namespace string) (func(), error) {
	k8sClient, _, err := utils.GetE2ETestKubeClientWithError()
	if err != nil {
		return nil, err
	}

	secretName := instanceName + "-cluster"
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace}}
	if err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(secret), secret); err != nil {
		return nil, fmt.Errorf("unable to locate %q Secret", secretName)
	}

	localPort := portforwardFixture.ReserveLocalPort()
	server := fmt.Sprintf("localhost:%d", localPort)
	cancel := portforwardFixture.StartPortForward(namespace, "svc/"+instanceName+"-server", fmt.Sprintf("%d:80", localPort))

	output, err := s.RunArgoCDCLI("login", server, "--username", "admin",
		"--password", string(secret.Data["admin.password"]), "--insecure")
	if err != nil {
		cancel()
		return nil, err
	}

	if !strings.Contains(output, "'admin:login' logged in successfully") {
		cancel()
		return nil, fmt.Errorf("unable to log in to ArgoCD instance %q in namespace %q", instanceName, namespace)
	}

	return cancel, nil
}
