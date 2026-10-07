package portforward

import (
	"bufio"
	"io"
	"net"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// StartPortForward runs 'kubectl port-forward' against the given namespace/subject (e.g. 'svc/my-service' or
// 'service/my-service') using the given port mapping (e.g. '8443:https' or '2222:22'), and waits for the
// port-forward to report that it is ready.
//
// The caller is responsible for calling the returned cleanup function (e.g. via 'defer', or by appending it to a
// slice of cleanup functions) to terminate the port-forward once it is no longer needed.
func StartPortForward(namespace string, subject string, portMapping string) func() {
	GinkgoHelper()

	cmdArgs := []string{"kubectl", "port-forward", "-n", namespace, subject, portMapping}
	GinkgoWriter.Println("executing command:", cmdArgs)

	// #nosec G204
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)

	stdout, err := cmd.StdoutPipe()
	Expect(err).ToNot(HaveOccurred())
	stderr, err := cmd.StderrPipe()
	Expect(err).ToNot(HaveOccurred())

	// 'kubectl port-forward' will print this output indicating it has successfully started port-forwarding:
	// Forwarding from 127.0.0.1:8443 -> 8080
	// Forwarding from [::1]:8443 -> 8080
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
		if scanErr := scanner.Err(); scanErr != nil {
			GinkgoWriter.Println("port-forward scanner error:", scanErr)
		}
	}

	Expect(cmd.Start()).To(Succeed())
	go streamOutput(stdout, func() { close(ready) })
	go streamOutput(stderr, nil)

	go func() {
		defer GinkgoRecover()
		if err := cmd.Wait(); err != nil && !strings.Contains(err.Error(), "killed") && !strings.Contains(err.Error(), "signal: killed") {
			GinkgoWriter.Println("port-forward process error:", err)
		}
	}()

	select {
	case <-ready:
		GinkgoWriter.Println("port-forward is ready")
	case <-time.After(60 * time.Second):
		Fail("timed out waiting for port-forward to be ready")
	}

	return func() {
		GinkgoWriter.Println("terminating port-forward")
		if cmd.Process != nil {
			if err := cmd.Process.Kill(); err != nil && !strings.Contains(err.Error(), "process already finished") {
				GinkgoWriter.Println("error on process kill:", err)
			}
		}
	}
}

// ReserveLocalPort returns an available local TCP port for use as the local side of a port-forward.
//
// Note: there is an inherent race between reserving the port and 'kubectl port-forward' binding to it, but this is
// the same approach used elsewhere to pick an ephemeral local port for port-forwarding.
func ReserveLocalPort() int {
	GinkgoHelper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	Expect(err).NotTo(HaveOccurred())
	port := listener.Addr().(*net.TCPAddr).Port
	Expect(listener.Close()).To(Succeed())
	return port
}
