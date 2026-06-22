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

package broker

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shortSocketDir returns a tempdir with a path short enough to fit
// inside the per-platform sockaddr_un.sun_path limit (~104 bytes on
// macOS). t.TempDir() uses the test name and can blow that budget.
func shortSocketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "k")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// echoBackend records what the most recent request looked like so tests
// can assert that the broker forwarded the wire bytes faithfully.
type echoBackend struct {
	method string
	path   string
	body   []byte
}

// newUDSEchoBackend brings up an HTTP server bound to a UDS in
// t.TempDir() that records each incoming request and replies with a
// fixed body. Returns the socket path so the test can build a UDS
// transport that targets it.
func newUDSEchoBackend(t *testing.T, response string) (string, *echoBackend) {
	t.Helper()
	rec := &echoBackend{}
	socketPath := filepath.Join(shortSocketDir(t), "e.sock")
	ln, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec.method = r.Method
			rec.path = r.URL.Path
			b, _ := io.ReadAll(r.Body)
			rec.body = b
			_, _ = w.Write([]byte(response))
		}),
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return socketPath, rec
}

func TestJSONRPCReverseProxy_ForwardsRequestVerbatim(t *testing.T) {
	socketPath, rec := newUDSEchoBackend(t, "backend-reply")

	counters := NewMetrics(prometheus.NewRegistry())
	proxy := NewJSONRPCReverseProxy(NewUDSHTTPTransport(socketPath), counters)

	req := httptest.NewRequest("POST", "/rpc", stringReader("hello"))
	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "backend-reply", w.Body.String())
	assert.Equal(t, "POST", rec.method)
	assert.Equal(t, "/rpc", rec.path)
	assert.Equal(t, "hello", string(rec.body))
}

func TestJSONRPCReverseProxy_IncrementsJSONRPCCounter(t *testing.T) {
	counters := NewMetrics(prometheus.NewRegistry())
	var midCallJSONRPC, midCallREST, midCallGRPC float64

	socketPath := filepath.Join(shortSocketDir(t), "a.sock")
	ln, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			midCallJSONRPC = testutil.ToFloat64(counters.JSONRPC())
			midCallREST = testutil.ToFloat64(counters.REST())
			midCallGRPC = testutil.ToFloat64(counters.GRPC())
			w.WriteHeader(http.StatusOK)
		}),
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	proxy := NewJSONRPCReverseProxy(NewUDSHTTPTransport(socketPath), counters)
	proxy.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/rpc", nil))

	assert.Equal(t, float64(1), midCallJSONRPC, "JSON-RPC counter must be 1 while the request is in flight")
	assert.Equal(t, float64(0), midCallREST, "REST counter must not move on a JSON-RPC request")
	assert.Equal(t, float64(0), midCallGRPC, "gRPC counter must not move on a JSON-RPC request")
	assert.Equal(t, float64(0), testutil.ToFloat64(counters.JSONRPC()),
		"JSON-RPC counter must return to 0 after request completes")
}

func TestRESTReverseProxy_IncrementsRESTCounter(t *testing.T) {
	// Symmetric to the JSON-RPC variant.
	counters := NewMetrics(prometheus.NewRegistry())
	var midCallREST, midCallJSONRPC float64

	socketPath := filepath.Join(shortSocketDir(t), "a.sock")
	ln, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			midCallREST = testutil.ToFloat64(counters.REST())
			midCallJSONRPC = testutil.ToFloat64(counters.JSONRPC())
			w.WriteHeader(http.StatusOK)
		}),
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	proxy := NewRESTReverseProxy(NewUDSHTTPTransport(socketPath), counters)
	proxy.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/foo", nil))

	assert.Equal(t, float64(1), midCallREST)
	assert.Equal(t, float64(0), midCallJSONRPC)
	assert.Equal(t, float64(0), testutil.ToFloat64(counters.REST()))
}

func TestPassthroughReverseProxy_ForwardsArbitraryPath(t *testing.T) {
	socketPath, rec := newUDSEchoBackend(t, "ui-html")

	counters := NewMetrics(prometheus.NewRegistry())
	proxy := NewPassthroughReverseProxy(NewUDSHTTPTransport(socketPath), counters)

	req := httptest.NewRequest("GET", "/my-app/v1/sessions", nil)
	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "ui-html", w.Body.String())
	assert.Equal(t, "GET", rec.method)
	assert.Equal(t, "/my-app/v1/sessions", rec.path,
		"passthrough must forward the path verbatim — no rewriting")
}

func TestPassthroughReverseProxy_OnlyIncrementsPassthroughCounter(t *testing.T) {
	counters := NewMetrics(prometheus.NewRegistry())
	var midPassthrough, midJSONRPC, midREST, midGRPC float64

	socketPath := filepath.Join(shortSocketDir(t), "p.sock")
	ln, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			midPassthrough = testutil.ToFloat64(counters.Passthrough())
			midJSONRPC = testutil.ToFloat64(counters.JSONRPC())
			midREST = testutil.ToFloat64(counters.REST())
			midGRPC = testutil.ToFloat64(counters.GRPC())
			w.WriteHeader(http.StatusOK)
		}),
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	proxy := NewPassthroughReverseProxy(NewUDSHTTPTransport(socketPath), counters)
	proxy.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/ui", nil))

	assert.Equal(t, float64(1), midPassthrough, "Passthrough counter must be 1 in flight")
	assert.Equal(t, float64(0), midJSONRPC)
	assert.Equal(t, float64(0), midREST)
	assert.Equal(t, float64(0), midGRPC)
	assert.Equal(t, float64(0), testutil.ToFloat64(counters.Passthrough()),
		"Passthrough counter must return to 0 after request completes")
}

// stringReader returns an io.Reader for a string without pulling strings.Reader
// out of the strings package — keeping the test's imports minimal.
func stringReader(s string) io.Reader {
	return &readerAt{data: []byte(s)}
}

type readerAt struct {
	data []byte
	off  int
}

func (r *readerAt) Read(p []byte) (int, error) {
	if r.off >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.off:])
	r.off += n
	return n, nil
}
