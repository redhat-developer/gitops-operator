/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package argocdclient

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	"github.com/gorilla/websocket"
)

type ArgoRestClient struct {
	endpoint string
	username string
	password string
	token    string
	client   *http.Client
}

// NewArgoClient returns a new client for Argo CD's REST API
func NewArgoClient(endpoint, username, password string) *ArgoRestClient {
	ac := &ArgoRestClient{
		endpoint: endpoint,
		username: username,
		password: password,
		client: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true, // #nosec G402
				},
			},
		},
	}
	return ac
}

// Login creates a new Argo CD session
func (c *ArgoRestClient) Login() error {
	// Get session token from API
	authStr := fmt.Sprintf(`{"username": "%s", "password": "%s"}`, c.username, c.password)
	payload := io.NopCloser(bytes.NewReader([]byte(authStr)))
	res, err := c.client.Do(&http.Request{
		Method:        http.MethodPost,
		URL:           &url.URL{Scheme: "https", Host: c.endpoint, Path: "/api/v1/session"},
		Body:          payload,
		Header:        http.Header{"Content-Type": []string{"application/json"}},
		ContentLength: int64(len(authStr)),
	})
	if err != nil {
		return err
	}
	defer func() {
		_ = res.Body.Close()
	}()
	if res.StatusCode != 200 {
		return fmt.Errorf("expected HTTP 200, got %d", res.StatusCode)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}

	type tokenResponse struct {
		Token string `json:"token"`
	}
	token := &tokenResponse{}
	err = json.Unmarshal(body, token)
	if err != nil {
		return err
	}
	if token.Token == "" {
		return errors.New("empty token received")
	}
	c.token = token.Token
	return nil
}

// TerminalClient represents a test client for terminal WebSocket connections.
type TerminalClient struct {
	wsConn   *websocket.Conn
	mu       sync.Mutex
	closed   bool
	output   strings.Builder
	outputMu sync.Mutex
}

// ExecTerminal opens a terminal session to a pod via WebSocket.
// This replicates the behavior of the ArgoCD UI when a user opens a terminal session to an application.
// ArgoCD decides which shell to use based on the configured allowed shells.
func (c *ArgoRestClient) ExecTerminal(app *v1alpha1.Application, namespace, podName, container string) (*TerminalClient, error) {
	if err := c.ensureToken(); err != nil {
		return nil, err
	}

	// Build the exec URL
	u := &url.URL{
		Scheme: "wss",
		Host:   c.endpoint,
		Path:   "/terminal",
	}

	q := u.Query()
	q.Set("pod", podName)
	q.Set("container", container)
	q.Set("appName", app.Name)
	q.Set("appNamespace", app.Namespace)
	q.Set("projectName", app.Spec.Project)
	q.Set("namespace", namespace)
	u.RawQuery = q.Encode()

	// Create WebSocket dialer with TLS config
	dialer := websocket.Dialer{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, // #nosec G402
		},
	}

	// Set token as cookie - ArgoCD expects auth token in argocd.token cookie
	headers := http.Header{}
	headers.Set("Cookie", fmt.Sprintf("argocd.token=%s", c.token))

	// Connect to WebSocket
	wsConn, resp, err := dialer.Dial(u.String(), headers)
	if err != nil {
		if resp != nil {
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("failed to connect to terminal WebSocket: %w (status: %d, body: %s)", err, resp.StatusCode, string(body))
		}
		return nil, fmt.Errorf("failed to connect to terminal WebSocket: %w", err)
	}

	session := &TerminalClient{
		wsConn: wsConn,
	}

	// Start reading output in background
	go session.readOutput()

	return session, nil
}

// ensureToken makes sure we have a valid authentication token
func (c *ArgoRestClient) ensureToken() error {
	if c.token == "" {
		return c.Login()
	}
	return nil
}

// terminalMessage is the JSON message format used by ArgoCD terminal WebSocket
type terminalMessage struct {
	Operation string `json:"operation"`
	Data      string `json:"data"`
	Rows      uint16 `json:"rows"`
	Cols      uint16 `json:"cols"`
}

// readOutput continuously reads output from the WebSocket connection
func (s *TerminalClient) readOutput() {
	for {
		_, message, err := s.wsConn.ReadMessage()
		if err != nil {
			// Connection closed or error
			return
		}

		if len(message) < 1 {
			continue
		}

		// Parse JSON message
		var msg terminalMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			continue
		}

		switch msg.Operation {
		case "stdout":
			s.outputMu.Lock()
			s.output.WriteString(msg.Data)
			s.outputMu.Unlock()
		}
	}
}

// SendInput sends input to the terminal session
func (s *TerminalClient) SendInput(input string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return errors.New("session is closed")
	}

	// ArgoCD terminal uses JSON messages (includes rows/cols like the UI)
	msg, err := json.Marshal(terminalMessage{
		Operation: "stdin",
		Data:      input,
		Rows:      24,
		Cols:      80,
	})
	if err != nil {
		return err
	}
	return s.wsConn.WriteMessage(websocket.TextMessage, msg)
}

// SendResize sends a terminal resize message
func (s *TerminalClient) SendResize(cols, rows uint16) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return errors.New("session is closed")
	}

	// ArgoCD terminal uses JSON messages
	msg, err := json.Marshal(terminalMessage{
		Operation: "resize",
		Cols:      cols,
		Rows:      rows,
	})
	if err != nil {
		return err
	}
	return s.wsConn.WriteMessage(websocket.TextMessage, msg)
}

// GetOutput returns all captured output so far
func (s *TerminalClient) GetOutput() string {
	s.outputMu.Lock()
	defer s.outputMu.Unlock()
	return s.output.String()
}

// WaitForOutput waits until the output contains the expected string or timeout
func (s *TerminalClient) WaitForOutput(expected string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(s.GetOutput(), expected) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// Close closes the terminal session
func (s *TerminalClient) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true
	return s.wsConn.Close()
}
