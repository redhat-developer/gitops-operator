package gitserver

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"strings"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	. "github.com/onsi/gomega"
	"golang.org/x/crypto/ssh"

	"github.com/argoproj-labs/argocd-operator/controllers/argoutil"
)

type sshKeyPair struct {
	privateKeyPEM []byte
	publicKey     string
}

// generateSSHKeyPair returns the key the tests register with the Git server and hand to Argo CD
// as repository credentials.
//
// ECDSA P-256 rather than Ed25519, because Ed25519 is not FIPS-approved: with the RHEL 9 crypto
// policy set to FIPS, OpenSSH offers only
//
//	ecdsa-sha2-nistp256/384/521 and rsa-sha2-256/512
//
// for public key authentication, so a repo-server on a FIPS cluster cannot authenticate with an
// Ed25519 key at all. 1-141's "hydrate kustomize to another branch via ssh" failed there with
// "Permission denied (publickey)" while its https variant passed. P-256 is accepted in FIPS mode
// and out of it, so this needs no branching on the cluster's mode.
func generateSSHKeyPair() sshKeyPair {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	Expect(err).NotTo(HaveOccurred())

	sshPublicKey, err := ssh.NewPublicKey(&privateKey.PublicKey)
	Expect(err).NotTo(HaveOccurred())

	privateKeyBlock, err := ssh.MarshalPrivateKey(privateKey, "")
	Expect(err).NotTo(HaveOccurred())

	return sshKeyPair{
		privateKeyPEM: pem.EncodeToMemory(privateKeyBlock),
		publicKey:     string(ssh.MarshalAuthorizedKey(sshPublicKey)),
	}
}

func formatSSHKnownHosts(host string, port int32, publicKey ssh.PublicKey) string {
	keyLine := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(publicKey)))
	parts := strings.SplitN(keyLine, " ", 3)
	Expect(len(parts)).To(BeNumerically(">=", 2))

	hostPort := host
	if port != 22 {
		hostPort = fmt.Sprintf("[%s]:%d", host, port)
	}
	return fmt.Sprintf("%s %s %s\n", hostPort, parts[0], parts[1])
}

func generateTLSSecretData(domain string, podName string, namespace string) map[string][]byte {
	key, err := argoutil.NewPrivateKey()
	Expect(err).NotTo(HaveOccurred())

	caKey, err := argoutil.NewPrivateKey()
	Expect(err).NotTo(HaveOccurred())
	caCert, err := argoutil.NewSelfSignedCACertificate(domain, caKey)
	Expect(err).NotTo(HaveOccurred())

	certSpec := &certmanagerv1.CertificateSpec{
		CommonName: domain,
		Subject: &certmanagerv1.X509Subject{
			Organizations: []string{domain},
		},
	}
	dnsNames := []string{
		podName,
		fmt.Sprintf("%s.%s", podName, namespace),
		fmt.Sprintf("%s.%s.svc", podName, namespace),
		fmt.Sprintf("%s.%s.svc.cluster.local", podName, namespace),
	}
	cert, err := argoutil.NewSignedCertificate(certSpec, dnsNames, key, caCert, caKey)
	Expect(err).NotTo(HaveOccurred())

	return map[string][]byte{
		"tls.crt": argoutil.EncodeCertificatePEM(cert),
		"tls.key": argoutil.EncodePrivateKeyPEM(key),
		"ca.crt":  argoutil.EncodeCertificatePEM(caCert),
	}
}
