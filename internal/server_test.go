package internal_test

import (
	"errors"
	"net"
	"net/http"
	"testing"

	"github.com/kazz187/taskguild/internal"
	"github.com/kazz187/taskguild/internal/config"
)

// TestServeProtocols verifies that a single listener accepts both HTTP/1.1 and
// cleartext HTTP/2 (h2c) with prior knowledge, which is the behavior the
// golang.org/x/net/http2/h2c handler wrapper used to provide.
func TestServeProtocols(t *testing.T) {
	t.Parallel()

	baseURL := "http://" + startTestServer(t)

	tests := []struct {
		name      string
		protocols *http.Protocols
		wantProto string
	}{
		{name: "http1", protocols: http1Protocols(), wantProto: "HTTP/1.1"},
		{name: "h2c", protocols: h2cProtocols(), wantProto: "HTTP/2.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := &http.Client{Transport: &http.Transport{Protocols: tt.protocols}}
			t.Cleanup(client.CloseIdleConnections)

			// /health bypasses apiKeyMiddleware, so no credentials are needed.
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, baseURL+"/health", nil)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}

			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("GET /health: %v", err)
			}

			defer func() {
				closeErr := resp.Body.Close()
				if closeErr != nil {
					t.Errorf("close response body: %v", closeErr)
				}
			}()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("GET /health status = %d, want %d", resp.StatusCode, http.StatusOK)
			}

			if resp.Proto != tt.wantProto {
				t.Errorf("GET /health proto = %q, want %q", resp.Proto, tt.wantProto)
			}
		})
	}
}

// startTestServer starts the production HTTP server on an ephemeral loopback
// port and returns its "host:port" address.
func startTestServer(t *testing.T) string {
	t.Helper()

	env := &config.Env{APIKey: "test-api-key"}

	// The generated Connect handler constructors only take method values off
	// the service pointers, so nil services are safe here: nothing is
	// dereferenced until an RPC is dispatched, and this test only calls /health.
	srv := internal.NewServer(
		env,
		nil, nil, nil, nil,
		nil, nil, nil, nil,
		nil, nil, nil, nil,
		nil, nil, nil, nil,
	)

	ctx := t.Context()

	var lc net.ListenConfig

	ln, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	serveErr := make(chan error, 1)

	go func() {
		serveErr <- srv.Serve(ctx, ln)
	}()

	t.Cleanup(func() {
		closeErr := ln.Close()
		if closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
			t.Errorf("close listener: %v", closeErr)
		}

		doneErr := <-serveErr
		if doneErr != nil && !errors.Is(doneErr, net.ErrClosed) && !errors.Is(doneErr, http.ErrServerClosed) {
			t.Errorf("serve: %v", doneErr)
		}
	})

	return ln.Addr().String()
}

// http1Protocols returns a transport protocol set restricted to HTTP/1.1.
func http1Protocols() *http.Protocols {
	p := new(http.Protocols)
	p.SetHTTP1(true)

	return p
}

// h2cProtocols returns a transport protocol set that speaks cleartext HTTP/2
// with prior knowledge. net/http only does so for http:// URLs when
// UnencryptedHTTP2 is enabled and HTTP1 is not.
func h2cProtocols() *http.Protocols {
	p := new(http.Protocols)
	p.SetUnencryptedHTTP2(true)

	return p
}
