package gitserver

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/argoproj-labs/argocd-operator/common"
	argocdutil "github.com/argoproj-labs/argocd-operator/controllers/argocd"
	"github.com/argoproj-labs/argocd-operator/tests/ginkgo/fixture"
	podFixture "github.com/argoproj-labs/argocd-operator/tests/ginkgo/fixture/pod"
	routev1 "github.com/openshift/api/route/v1"
)

const (
	serverName = "e2e-gitserver"

	httpPort       = int32(3000)
	sshPort        = int32(2222) // rootless image listens on 2222
	sshServicePort = int32(22)   // service maps 22 -> 2222 for callers
	gitUsername    = "gituser"
	giteaSSHLogin  = "git" // rootless builtin SSH authenticates as RUN_USER (git), not the Gitea account name
)

// Server exposes connection details for a Gitea instance started in the test namespace.
type Server struct {
	namespace   string
	serviceName string

	clusterDomain string // in-cluster service DNS name (matches the TLS certificate SAN)
	domain        string // external HTTPS route hostname for local git clients
	localSSHPort  int32  // kubectl port-forward local port for git clients on the test runner
	httpURL       string
	httpUsername  string
	httpPassword  string
	sshPrivateKey []byte
	sshPublicKey  string
	sshKnownHosts string
	sshKeyFile    string
	caCert        []byte

	stopPortForward func()
	httpsRoute      *routev1.Route
}

func (s *Server) getSSHKeyFile() (string, error) {
	if s.sshKeyFile != "" {
		return s.sshKeyFile, nil
	}

	f, err := os.CreateTemp("", "operator-gitserver-ssh-key-")
	if err != nil {
		return "", err
	}

	if _, err := f.Write(s.getSSHPrivateKey()); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", err
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}

	s.sshKeyFile = f.Name()
	return s.sshKeyFile, nil
}

func (s *Server) removeSSHKeyFile() {
	if s.sshKeyFile == "" {
		return
	}
	_ = os.Remove(s.sshKeyFile)
	s.sshKeyFile = ""
}

func (s *Server) getSSHPrivateKey() []byte {
	return append([]byte(nil), s.sshPrivateKey...)
}

// TLSHostKey returns the hostname key used in argocd-tls-certs-cm and InitialCerts.
// Use the in-cluster DNS name so it matches the Git server TLS certificate SAN.
func (s *Server) TLSHostKey() string {
	return s.clusterDomain
}

// GetCACert returns the PEM-encoded CA certificate used by the Git server HTTPS endpoint.
func (s *Server) GetCACert() []byte {
	return append([]byte(nil), s.caCert...)
}

// StartServer deploys a functional Git instance with HTTPS and SSH enabled in the given namespace.
func StartServer(ctx context.Context, k8sClient client.Client, ns *corev1.Namespace) (server *Server, cleanup func()) {
	Expect(ns).ToNot(BeNil())

	By("Deploying Git server")
	clusterDomain := fmt.Sprintf("%s.%s.svc.cluster.local", serverName, ns.Name)

	tls := generateTLSSecretData(clusterDomain, serverName, ns.Name)
	httpPassword := argocdutil.GenerateRandomString(24)
	internalToken := argocdutil.GenerateRandomString(24)
	sshKeys := generateSSHKeyPair()

	server = &Server{
		namespace:     ns.Name,
		serviceName:   serverName,
		clusterDomain: clusterDomain,
		httpUsername:  gitUsername,
		httpPassword:  httpPassword,
		sshPrivateKey: sshKeys.privateKeyPEM,
		sshPublicKey:  strings.TrimSpace(sshKeys.publicKey),
		caCert:        tls["ca.crt"],
	}

	tlsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serverName + "-tls",
			Namespace: ns.Name,
			Labels:    fixture.NamespaceLabels,
		},
		Type: corev1.SecretTypeTLS,
		Data: tls,
	}
	Expect(k8sClient.Create(ctx, tlsSecret)).To(Succeed())

	httpCredentialsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serverName + "-http-credentials",
			Namespace: ns.Name,
			Labels:    fixture.NamespaceLabels,
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"username": gitUsername,
			"password": httpPassword,
		},
	}
	Expect(k8sClient.Create(ctx, httpCredentialsSecret)).To(Succeed())

	sshCredentialsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serverName + "-ssh-credentials",
			Namespace: ns.Name,
			Labels:    fixture.NamespaceLabels,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"ssh-privatekey": sshKeys.privateKeyPEM,
			"ssh-publickey":  []byte(strings.TrimSpace(sshKeys.publicKey)),
		},
	}
	Expect(k8sClient.Create(ctx, sshCredentialsSecret)).To(Succeed())

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serverName,
			Namespace: ns.Name,
			Labels: map[string]string{
				fixture.E2ETestLabelsKey:       fixture.E2ETestLabelsValue,
				"app.kubernetes.io/name":       serverName,
				"app.kubernetes.io/component":  "git-server",
				"app.kubernetes.io/instance":   serverName,
				"app.kubernetes.io/managed-by": "argocd-operator-e2e",
			},
		},
		Spec: corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{
				FSGroup: ptr.To(int64(1000)),
			},
			Containers: []corev1.Container{
				{
					Name:            serverName,
					Image:           giteaImage,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Env:             giteaEnvVars(clusterDomain, internalToken),
					Ports: []corev1.ContainerPort{
						{Name: "http", ContainerPort: httpPort, Protocol: corev1.ProtocolTCP},
						{Name: "ssh", ContainerPort: sshPort, Protocol: corev1.ProtocolTCP},
					},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "data", MountPath: "/data"},
						{Name: "tls", MountPath: "/etc/gitea/certs", ReadOnly: true},
						{Name: "http-credentials", MountPath: "/etc/gitea/credentials/http", ReadOnly: true},
						{Name: "ssh-credentials", MountPath: "/etc/gitea/credentials/ssh", ReadOnly: true},
					},
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							TCPSocket: &corev1.TCPSocketAction{
								Port: intstr.FromInt32(httpPort),
							},
						},
						InitialDelaySeconds: 10,
						PeriodSeconds:       5,
					},
					SecurityContext: &corev1.SecurityContext{
						RunAsUser:  ptr.To(int64(1000)),
						RunAsGroup: ptr.To(int64(1000)),
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
						AllowPrivilegeEscalation: ptr.To(false),
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{
								"ALL",
							},
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "data",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
				{
					Name: "tls",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{SecretName: tlsSecret.Name},
					},
				},
				{
					Name: "http-credentials",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{SecretName: httpCredentialsSecret.Name},
					},
				},
				{
					Name: "ssh-credentials",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{SecretName: sshCredentialsSecret.Name},
					},
				},
			},
		},
	}
	Expect(k8sClient.Create(ctx, pod)).To(Succeed())

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serverName,
			Namespace: ns.Name,
			Labels:    fixture.NamespaceLabels,
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				"app.kubernetes.io/name": serverName,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "https",
					Port:       httpPort,
					TargetPort: intstr.FromString("http"),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "ssh",
					Port:       sshServicePort,
					TargetPort: intstr.FromInt32(sshPort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
	Expect(k8sClient.Create(ctx, service)).To(Succeed())

	Eventually(pod, "2m", "10s").Should(podFixture.HavePhase(corev1.PodRunning))

	By("exposing Git server outside the cluster")
	server.httpsRoute = &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serverName + "-https",
			Namespace: ns.Name,
			Labels:    fixture.NamespaceLabels,
		},
		Spec: routev1.RouteSpec{
			To: routev1.RouteTargetReference{
				Kind:   "Service",
				Name:   service.Name,
				Weight: ptr.To(int32(100)),
			},
			Port: &routev1.RoutePort{
				TargetPort: intstr.FromString("https"),
			},
			TLS: &routev1.TLSConfig{
				Termination: routev1.TLSTerminationPassthrough,
			},
		},
	}
	Expect(k8sClient.Create(ctx, server.httpsRoute)).To(Succeed())
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(server.httpsRoute), server.httpsRoute)).To(Succeed())
		g.Expect(server.httpsRoute.Status.Ingress).NotTo(BeEmpty())
		g.Expect(server.httpsRoute.Status.Ingress[0].Host).NotTo(BeEmpty())
	}, "3m", "5s").Should(Succeed())

	server.domain = server.httpsRoute.Status.Ingress[0].Host
	server.httpURL = fmt.Sprintf("https://%s", server.domain)
	server.localSSHPort = reserveLocalSSHPort()
	server.stopPortForward = startSSHPortForward(ns.Name, service.Name, server.localSSHPort)
	GinkgoWriter.Printf("Git server HTTPS endpoint: %s\n", server.httpURL)
	GinkgoWriter.Printf("Git server local SSH endpoint: 127.0.0.1:%d\n", server.localSSHPort)

	configureGiteaAdmin(server)
	server.sshKnownHosts = fetchSSHKnownHosts(server)

	By("registering repository credentials for Argo CD to use")
	repoCredentialsSecrets := server.repoCredentialsSecrets(ns.Name)
	for _, secret := range repoCredentialsSecrets {
		Expect(k8sClient.Create(ctx, secret)).To(Succeed())
	}

	cleanup = func() {
		server.removeSSHKeyFile()
		if server.stopPortForward != nil {
			server.stopPortForward()
		}
		if server.httpsRoute != nil {
			err := k8sClient.Delete(ctx, server.httpsRoute)
			if err != nil && !apierrors.IsNotFound(err) {
				GinkgoWriter.Println("gitserver cleanup:", client.ObjectKeyFromObject(server.httpsRoute), err)
			}
		}

		resources := []client.Object{
			service, pod, sshCredentialsSecret, httpCredentialsSecret, tlsSecret,
		}
		for _, secret := range repoCredentialsSecrets {
			resources = append(resources, secret)
		}
		for _, obj := range resources {
			err := k8sClient.Delete(ctx, obj)
			if err != nil && !apierrors.IsNotFound(err) {
				GinkgoWriter.Println("gitserver cleanup:", client.ObjectKeyFromObject(obj), err)
			}
		}
	}

	return server, cleanup
}

func (s *Server) sshRepoURLPrefix() string {
	return fmt.Sprintf("ssh://%s@%s:%d/%s/", giteaSSHLogin, s.clusterDomain, sshServicePort, s.httpUsername)
}

// SSHKnownHosts returns the known_hosts entry for the server's SSH host key.
func (s *Server) SSHKnownHosts() string {
	Expect(s.sshKnownHosts).NotTo(BeEmpty())
	return s.sshKnownHosts
}

func (s *Server) httpRepoURLPrefix() string {
	return fmt.Sprintf("https://%s:%d/%s/", s.clusterDomain, httpPort, s.httpUsername)
}

func (s *Server) repoCredentialsSecrets(namespace string) []*corev1.Secret {
	return []*corev1.Secret{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      serverName + "-argocd-ssh-repo-creds",
				Namespace: namespace,
				Labels: map[string]string{
					common.ArgoCDSecretTypeLabel: "repo-creds",
				},
			},
			StringData: map[string]string{
				"type":          "git",
				"url":           s.sshRepoURLPrefix(),
				"sshPrivateKey": string(s.getSSHPrivateKey()),
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      serverName + "-argocd-ssh-repo-write-creds",
				Namespace: namespace,
				Labels: map[string]string{
					common.ArgoCDSecretTypeLabel: "repo-write-creds",
				},
			},
			StringData: map[string]string{
				"type":          "git",
				"url":           s.sshRepoURLPrefix(),
				"sshPrivateKey": string(s.getSSHPrivateKey()),
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      serverName + "-argocd-http-repo-creds",
				Namespace: namespace,
				Labels: map[string]string{
					common.ArgoCDSecretTypeLabel: "repo-creds",
				},
			},
			StringData: map[string]string{
				"type":     "git",
				"url":      s.httpRepoURLPrefix(),
				"username": s.httpUsername,
				"password": s.httpPassword,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      serverName + "-argocd-http-repo-write-creds",
				Namespace: namespace,
				Labels: map[string]string{
					common.ArgoCDSecretTypeLabel: "repo-write-creds",
				},
			},
			StringData: map[string]string{
				"type":     "git",
				"url":      s.httpRepoURLPrefix(),
				"username": s.httpUsername,
				"password": s.httpPassword,
			},
		},
	}
}

func (s *Server) CreateRepo(repoName string) Repo {
	_, err := giteaAPIPost(
		s.namespace,
		s.httpPassword,
		fmt.Sprintf("/api/v1/admin/users/%s/repos", gitUsername),
		fmt.Sprintf(`{"name":"%s","private":false}`, repoName),
	)
	if err != nil {
		GinkgoWriter.Println("gitea repo create returned error (may already exist):", err)
	}

	Eventually(func() error {
		output, err := giteaAPIGet(s.namespace, s.httpPassword,
			fmt.Sprintf("/api/v1/repos/%s/%s", gitUsername, repoName),
		)
		if err != nil {
			return err
		}
		if !strings.Contains(output, repoName) {
			return fmt.Errorf("repository %q not found in API response", repoName)
		}
		return nil
	}, "30s", "5s").Should(Succeed())

	return Repo{
		server:   s,
		repoName: repoName,
	}
}

func reserveLocalSSHPort() int32 {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	Expect(err).NotTo(HaveOccurred())
	port := int32(listener.Addr().(*net.TCPAddr).Port)
	Expect(listener.Close()).To(Succeed())
	return port
}

func startSSHPortForward(namespace, serviceName string, localPort int32) func() {
	portMapping := fmt.Sprintf("%d:%d", localPort, sshServicePort)
	cmdArgs := []string{"kubectl", "port-forward", "-n", namespace, "svc/" + serviceName, portMapping}
	GinkgoWriter.Println("executing command:", cmdArgs)

	// #nosec G204
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)

	stdout, err := cmd.StdoutPipe()
	Expect(err).NotTo(HaveOccurred())
	stderr, err := cmd.StderrPipe()
	Expect(err).NotTo(HaveOccurred())

	ready := make(chan struct{})
	streamOutput := func(pipe io.Reader, signalReady func()) {
		defer GinkgoRecover()

		scanner := bufio.NewScanner(pipe)
		for scanner.Scan() {
			line := scanner.Text()
			GinkgoWriter.Println("port-forward:", line)
			if signalReady != nil && strings.HasPrefix(line, "Forwarding from") {
				signalReady()
				signalReady = nil
			}
		}
	}

	Expect(cmd.Start()).To(Succeed())
	go streamOutput(stdout, func() { close(ready) })
	go streamOutput(stderr, nil)

	select {
	case <-ready:
		GinkgoWriter.Println("SSH port-forward is ready")
	case <-time.After(60 * time.Second):
		Fail("timed out waiting for SSH port-forward to be ready")
	}

	go func() {
		defer GinkgoRecover()
		if err := cmd.Wait(); err != nil && !strings.Contains(err.Error(), "killed") {
			GinkgoWriter.Println("port-forward process error:", err)
		}
	}()

	return func() {
		GinkgoWriter.Println("terminating SSH port-forward")
		_ = cmd.Process.Kill()
	}
}
