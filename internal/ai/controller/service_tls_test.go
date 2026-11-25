package controller

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Exercise the actual service with the symlink replacement used for projected
// Kubernetes Secrets. Every connection verifies the certificate and its IP SAN.
func TestServiceReloadsProjectedServingCertificate(t *testing.T) {
	dir := t.TempDir()
	roots := x509.NewCertPool()
	firstCert, firstKey := servingCertificatePair(t, 1)
	secondCert, secondKey := servingCertificatePair(t, 2)
	require.True(t, roots.AppendCertsFromPEM(firstCert))
	require.True(t, roots.AppendCertsFromPEM(secondCert))

	writeVersion := func(name string, cert, key []byte) {
		t.Helper()
		versionDir := filepath.Join(dir, name)
		require.NoError(t, os.Mkdir(versionDir, 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(versionDir, "tls.crt"), cert, 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(versionDir, "tls.key"), key, 0o600))
	}
	writeVersion("first", firstCert, firstKey)
	writeVersion("invalid", secondCert, firstKey)
	writeVersion("second", secondCert, secondKey)
	require.NoError(t, os.Symlink("first", filepath.Join(dir, "..data")))
	require.NoError(t, os.Symlink("..data/tls.crt", filepath.Join(dir, "tls.crt")))
	require.NoError(t, os.Symlink("..data/tls.key", filepath.Join(dir, "tls.key")))
	rotate := func(previous, next string) {
		t.Helper()
		require.NoError(t, os.Symlink(next, filepath.Join(dir, "..next")))
		require.NoError(t, os.Rename(filepath.Join(dir, "..next"), filepath.Join(dir, "..data")))
		require.NoError(t, os.RemoveAll(filepath.Join(dir, previous)))
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listener.Addr().String()
	require.NoError(t, listener.Close())
	service, _, _ := incidentFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		service.Run(ctx, addr, filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key"), true)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("service did not stop after cancellation")
		}
	})

	readSerial := func() (int64, error) {
		conn, err := tls.DialWithDialer(&net.Dialer{Timeout: time.Second}, "tcp", addr, &tls.Config{
			RootCAs: roots, MinVersion: tls.VersionTLS12,
		})
		if err != nil {
			return 0, err
		}
		defer conn.Close()
		return conn.ConnectionState().PeerCertificates[0].SerialNumber.Int64(), nil
	}
	require.Eventually(t, func() bool {
		serial, err := readSerial()
		return err == nil && serial == 1
	}, 5*time.Second, 20*time.Millisecond, "initial certificate was not served")

	// An incomplete or mismatched rotation must not interrupt existing service.
	rotate("first", "invalid")
	require.Never(t, func() bool {
		serial, err := readSerial()
		return err != nil || serial != 1
	}, time.Second, 20*time.Millisecond, "invalid replacement discarded the last valid certificate")

	rotate("invalid", "second")
	// The watcher also polls, covering projected symlink changes when an inode
	// watch cannot be reattached until the new target becomes valid.
	require.Eventually(t, func() bool {
		serial, err := readSerial()
		return err == nil && serial == 2
	}, 15*time.Second, 20*time.Millisecond, "rotated certificate was not served without restarting")
}

func servingCertificatePair(t *testing.T, serial int64) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(serial),
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
}
