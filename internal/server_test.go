package internal_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/kazz187/taskguild/internal"
	"github.com/kazz187/taskguild/internal/config"
)

// testShutdownTimeout bounds the graceful shutdown performed during cleanup.
const testShutdownTimeout = 5 * time.Second

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

// TestServeGRPCTrailersOverH2C verifies that the gRPC protocol still works over
// cleartext HTTP/2, in particular that HTTP trailers reach the client. Trailers
// are mandatory in the gRPC protocol (grpc-status is sent as one) and they are
// the part most at risk from swapping the HTTP/2 implementation from
// golang.org/x/net/http2 to the one built into net/http.
func TestServeGRPCTrailersOverH2C(t *testing.T) {
	t.Parallel()

	baseURL := "http://" + startTestServer(t)

	client := &http.Client{Transport: &http.Transport{Protocols: h2cProtocols()}}
	t.Cleanup(client.CloseIdleConnections)

	// A gRPC message is a 1-byte compression flag plus a 4-byte big-endian
	// length. An empty HealthCheckRequest serializes to zero bytes, so this is
	// a complete, uncompressed, zero-length request message.
	msg := []byte{0x00, 0x00, 0x00, 0x00, 0x00}

	// /grpc.health.v1.Health/Check bypasses apiKeyMiddleware, and the static
	// checker needs no injected services, so no credentials are required.
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		baseURL+"/grpc.health.v1.Health/Check", bytes.NewReader(msg))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	req.Header.Set("Content-Type", "application/grpc+proto")
	req.Header.Set("Te", "trailers")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST Health/Check: %v", err)
	}

	defer func() {
		closeErr := resp.Body.Close()
		if closeErr != nil {
			t.Errorf("close response body: %v", closeErr)
		}
	}()

	// Trailers are only populated once the body has been drained.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	if resp.Proto != "HTTP/2.0" {
		t.Errorf("proto = %q, want %q", resp.Proto, "HTTP/2.0")
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	if len(body) == 0 {
		t.Error("response body is empty, want a gRPC message frame")
	}

	// grpc-status 0 is OK. Receiving it at all proves the trailer path works.
	gotStatus := resp.Trailer.Get("Grpc-Status")
	if gotStatus != "0" {
		t.Errorf("grpc-status trailer = %q, want %q", gotStatus, "0")
	}
}

// TestShutdownAfterListenFailure verifies that Shutdown is safe after
// ListenAndServe failed to bind. cmd/taskguild-server cancels its context on a
// server error and then calls Shutdown unconditionally, so a server that never
// started must not panic there.
func TestShutdownAfterListenFailure(t *testing.T) {
	t.Parallel()

	var lc net.ListenConfig

	// Occupy a port so that the server's own bind is guaranteed to fail.
	blocker, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	t.Cleanup(func() {
		closeErr := blocker.Close()
		if closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
			t.Errorf("close blocker: %v", closeErr)
		}
	})

	host, port, err := net.SplitHostPort(blocker.Addr().String())
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}

	env := &config.Env{APIKey: "test-api-key"}
	env.HTTPHost = host
	env.HTTPPort = port

	srv := newTestServer(env)

	err = srv.ListenAndServe(t.Context())
	if err == nil {
		t.Fatal("ListenAndServe on an occupied port returned nil, want an error")
	}

	// Must not panic on the nil inner *http.Server.
	err = srv.Shutdown(t.Context())
	if err != nil {
		t.Errorf("Shutdown after a failed listen: %v", err)
	}
}

// newTestServer builds a Server with no backing services.
//
// The generated Connect handler constructors only take method values off the
// service pointers, so nil services are safe: nothing is dereferenced until an
// RPC is dispatched, and these tests only call /health and the gRPC health
// endpoint, both of which are served without any injected service.
func newTestServer(env *config.Env) *internal.Server {
	return internal.NewServer(
		env,
		nil, nil, nil, nil,
		nil, nil, nil, nil,
		nil, nil, nil, nil,
		nil, nil, nil, nil,
	)
}

// startTestServer starts the production HTTP server on an ephemeral loopback
// port and returns its "host:port" address.
func startTestServer(t *testing.T) string {
	t.Helper()

	srv := newTestServer(&config.Env{APIKey: "test-api-key"})

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
		// Shut down rather than just closing the listener: this is the path
		// whose semantics changed when h2c stopped hijacking connections, and
		// it also reaps the connection goroutines. ctx is already canceled by
		// the time cleanups run, so detach from it.
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), testShutdownTimeout)
		defer cancel()

		shutdownErr := srv.Shutdown(shutdownCtx)
		if shutdownErr != nil {
			t.Errorf("shutdown: %v", shutdownErr)
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
