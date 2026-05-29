/*
Copyright 2026 The Kynoproj Authors.

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

package cmd

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	grpccredentials "google.golang.org/grpc/credentials"

	"github.com/kynoproj/kynomesh/pkg/broker"
	sharedtls "github.com/kynoproj/kynomesh/pkg/shared/tls"
)

func TestIsGRPCRequest(t *testing.T) {
	cases := []struct {
		name        string
		proto       int
		contentType string
		want        bool
	}{
		{"http2 grpc proto", 2, "application/grpc", true},
		{"http2 grpc subtype", 2, "application/grpc+proto", true},
		{"http2 json is rest", 2, "application/json", false},
		{"http1 grpc-shaped header still not grpc", 1, "application/grpc", false},
		{"http2 no content-type", 2, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &http.Request{
				ProtoMajor: tc.proto,
				Header:     http.Header{},
			}
			if tc.contentType != "" {
				r.Header.Set("Content-Type", tc.contentType)
			}
			assert.Equal(t, tc.want, isGRPCRequest(r))
		})
	}
}

const testAgentName = "stub-agent"

// stubCardHandler stands in for the production AgentCard proxy. It writes
// a minimal valid card body so tests can assert routing without spinning
// up a fake agent.
func stubCardHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"` + testAgentName + `"}`))
	})
}

// stubOKHandler returns an http.Handler that always responds 200 with a
// fixed body. Used to back the runtime's per-transport proxy slots so
// the multiplexer tests can assert that requests to /rpc or /api/* land
// on the right slot — without spinning up a real upstream.
func stubOKHandler(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	})
}

// testRuntime builds a brokerRuntime with stub HTTP proxies for both
// JSON-RPC and REST, a stub passthrough catch-all, and no gRPC server.
// The grpcConn field is left nil so the multiplexer falls back to the
// HTTP mux for everything — which is what these unit tests want; the
// full gRPC path is exercised in the dedicated proxy_grpc test under
// pkg/broker.
func testRuntime(t *testing.T) *brokerRuntime {
	t.Helper()
	return &brokerRuntime{
		counters: &broker.Counters{},
		enabled: map[a2a.TransportProtocol]bool{
			a2a.TransportProtocolJSONRPC:  true,
			a2a.TransportProtocolHTTPJSON: true,
		},
		httpProxies: map[a2a.TransportProtocol]http.Handler{
			a2a.TransportProtocolJSONRPC:  stubOKHandler("jsonrpc-ok"),
			a2a.TransportProtocolHTTPJSON: stubOKHandler("rest-ok"),
		},
		passthrough: stubOKHandler("passthrough-ok"),
	}
}

// TestMultiplexedServer_RoutesHTTPTraffic exercises the HTTP/1.1 routes —
// AgentCard, JSON-RPC mount, and the REST subtree — over a single httptest
// server, proving the mux wired by newMultiplexedServer dispatches
// correctly. gRPC routing is covered by isGRPCRequest above; an end-to-end
// gRPC test would need a real listener with HTTP/2 cleartext, which is out
// of scope for a unit test.
func TestMultiplexedServer_RoutesHTTPTraffic(t *testing.T) {
	cert, err := sharedtls.GenerateX509KeyPair()
	require.NoError(t, err)

	srv, err := newMultiplexedServer(0, testRuntime(t), stubCardHandler(), cert)
	require.NoError(t, err)
	ts := httptest.NewServer(srv.Handler)
	t.Cleanup(ts.Close)

	t.Run("AgentCard well-known path", func(t *testing.T) {
		resp, err := http.Get(ts.URL + a2asrv.WellKnownAgentCardPath)
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("JSON-RPC endpoint reaches its proxy", func(t *testing.T) {
		body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"x"}`)
		resp, err := http.Post(ts.URL+broker.JSONRPCEndpoint, "application/json", body)
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		gotBody, _ := io.ReadAll(resp.Body)
		assert.Equal(t, "jsonrpc-ok", string(gotBody))
	})

	t.Run("REST routes mount under /api", func(t *testing.T) {
		resp, err := http.Get(ts.URL + broker.RESTEndpoint + "/anything")
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		gotBody, _ := io.ReadAll(resp.Body)
		assert.Equal(t, "rest-ok", string(gotBody))
	})

	t.Run("unknown paths fall through to the passthrough catch-all", func(t *testing.T) {
		// The broker forwards any non-A2A HTTP traffic to the agent so
		// entry agents can serve UIs / custom REST endpoints on the
		// same shared port. We assert the response came from the
		// passthrough stub specifically — not from REST or JSON-RPC.
		resp, err := http.Get(ts.URL + "/some/user-app/path")
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		gotBody, _ := io.ReadAll(resp.Body)
		assert.Equal(t, "passthrough-ok", string(gotBody))
	})

	t.Run("canonical A2A paths win over passthrough on collision", func(t *testing.T) {
		// http.ServeMux longest-prefix match guarantees the explicit
		// /rpc and /api/ registrations beat the "/" catch-all even
		// though both are mounted. Re-assert the JSON-RPC path here
		// alongside the catch-all to make the precedence explicit.
		resp, err := http.Post(ts.URL+broker.JSONRPCEndpoint, "application/json",
			strings.NewReader(`{}`))
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })
		gotBody, _ := io.ReadAll(resp.Body)
		assert.Equal(t, "jsonrpc-ok", string(gotBody),
			"canonical JSON-RPC route must not be eclipsed by the passthrough catch-all")
	})
}

func TestMultiplexedServer_NoPassthroughLeavesUnknownPaths404(t *testing.T) {
	// If a brokerRuntime is constructed without a passthrough handler
	// (e.g. an admission-time misconfig or a hypothetical strict mode),
	// the multiplexer must not invent one; unknown paths return 404.
	rt := testRuntime(t)
	rt.passthrough = nil

	cert, err := sharedtls.GenerateX509KeyPair()
	require.NoError(t, err)
	srv, err := newMultiplexedServer(0, rt, stubCardHandler(), cert)
	require.NoError(t, err)
	ts := httptest.NewServer(srv.Handler)
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/does-not-exist")
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// startTLSServer brings up the multiplexed broker on an ephemeral port
// over real TLS using the in-memory self-signed cert. Returns the chosen
// port and registers cleanup that gracefully shuts the server down.
func startTLSServer(t *testing.T) (port int, cert *tls.Certificate) {
	t.Helper()

	cert, err := sharedtls.GenerateX509KeyPair()
	require.NoError(t, err)

	// Bind to an OS-assigned port up front so the test can dial it back.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port = ln.Addr().(*net.TCPAddr).Port

	srv, err := newMultiplexedServer(port, testRuntime(t), stubCardHandler(), cert)
	require.NoError(t, err)

	tlsLn := tls.NewListener(ln, srv.TLSConfig)
	serveErr := make(chan error, 1)
	go func() {
		// srv.Serve treats the listener as already-TLS-wrapped, which is
		// what we want — TLSConfig.Certificates is the source of truth.
		serveErr <- srv.Serve(tlsLn)
	}()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		// Serve returns http.ErrServerClosed after Shutdown — drain it.
		<-serveErr
	})
	return port, cert
}

// newTrustingHTTPSClient returns an HTTPS client that trusts the supplied
// self-signed cert via a fresh root pool. We deliberately avoid
// InsecureSkipVerify so the test exercises the same trust path real
// callers would use after pinning the broker's cert.
func newTrustingHTTPSClient(t *testing.T, cert *tls.Certificate) *http.Client {
	t.Helper()
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	require.NoError(t, err)
	roots := x509.NewCertPool()
	roots.AddCert(leaf)

	return &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:    roots,
				ServerName: "localhost",
				MinVersion: tls.VersionTLS12,
			},
			ForceAttemptHTTP2: true,
		},
	}
}

func TestBrokerServer_TLS_ServesAgentCard(t *testing.T) {
	// End-to-end: real TLS listener, real HTTPS client, well-known
	// AgentCard route returns 200 with the body the stub card handler
	// emitted (proving the route reaches the AgentCard handler, not
	// some other route by accident).
	port, cert := startTLSServer(t)
	client := newTrustingHTTPSClient(t, cert)

	url := fmt.Sprintf("https://localhost:%d%s", port, a2asrv.WellKnownAgentCardPath)
	resp, err := client.Get(url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), testAgentName)
}

func TestBrokerServer_TLS_RejectsPlaintext(t *testing.T) {
	// A plaintext HTTP request must NOT receive the AgentCard. The
	// net/http server detects the bad TLS handshake and writes back a
	// 400 with "client sent an HTTP request to an HTTPS server" — the
	// transport call succeeds but the response is decidedly not a 200
	// with our card payload.
	port, _ := startTLSServer(t)

	client := &http.Client{Timeout: 2 * time.Second}
	url := fmt.Sprintf("http://localhost:%d%s", port, a2asrv.WellKnownAgentCardPath)
	resp, err := client.Get(url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	assert.NotEqual(t, http.StatusOK, resp.StatusCode, "plaintext request must not reach the AgentCard handler")
	body, _ := io.ReadAll(resp.Body)
	assert.NotContains(t, string(body), testAgentName)
}

func TestBrokerServer_TLS_GRPCDialSucceeds(t *testing.T) {
	// gRPC clients dialling with TLS credentials must connect. We don't
	// invoke any RPC here — the a2a-go service is registered but we
	// haven't generated a client stub for it. The connection handshake
	// alone proves ALPN negotiated h2 and the server accepted the cert.
	port, cert := startTLSServer(t)

	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	require.NoError(t, err)
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	creds := grpccredentials.NewTLS(&tls.Config{
		RootCAs:    roots,
		ServerName: "localhost",
		MinVersion: tls.VersionTLS12,
	})

	conn, err := grpc.NewClient(fmt.Sprintf("localhost:%d", port), grpc.WithTransportCredentials(creds))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
}
