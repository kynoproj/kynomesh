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
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// echoBackend returns an httptest.Server that records the request path
// + method + body, and writes a fixed response.
type echoBackend struct {
	method string
	path   string
	body   []byte
}

func newEchoBackend(t *testing.T, response string) (*httptest.Server, *echoBackend) {
	t.Helper()
	rec := &echoBackend{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.method = r.Method
		rec.path = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		rec.body = b
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(ts.Close)
	return ts, rec
}

func TestJSONRPCReverseProxy_ForwardsRequestVerbatim(t *testing.T) {
	backend, rec := newEchoBackend(t, "backend-reply")
	backendURL, err := url.Parse(backend.URL)
	require.NoError(t, err)

	counters := &Counters{}
	proxy := NewJSONRPCReverseProxy(backendURL, counters)

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
	// The reverse proxy must bump the JSON-RPC counter — and only that
	// counter — for the duration of every forwarded request. The
	// in-handler observation runs inside the backend so the counter is
	// still elevated at the time we read it.
	counters := &Counters{}
	var midCallJSONRPC, midCallREST, midCallGRPC int64

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		midCallJSONRPC = counters.JSONRPC()
		midCallREST = counters.REST()
		midCallGRPC = counters.GRPC()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(backend.Close)

	backendURL, err := url.Parse(backend.URL)
	require.NoError(t, err)
	proxy := NewJSONRPCReverseProxy(backendURL, counters)
	proxy.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/rpc", nil))

	assert.Equal(t, int64(1), midCallJSONRPC, "JSON-RPC counter must be 1 while the request is in flight")
	assert.Equal(t, int64(0), midCallREST, "REST counter must not move on a JSON-RPC request")
	assert.Equal(t, int64(0), midCallGRPC, "gRPC counter must not move on a JSON-RPC request")
	assert.Equal(t, int64(0), counters.JSONRPC(), "JSON-RPC counter must return to 0 after request completes")
}

func TestRESTReverseProxy_IncrementsRESTCounter(t *testing.T) {
	// Symmetric to the JSON-RPC variant.
	counters := &Counters{}
	var midCallREST, midCallJSONRPC int64

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		midCallREST = counters.REST()
		midCallJSONRPC = counters.JSONRPC()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(backend.Close)

	backendURL, err := url.Parse(backend.URL)
	require.NoError(t, err)
	proxy := NewRESTReverseProxy(backendURL, counters)
	proxy.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/foo", nil))

	assert.Equal(t, int64(1), midCallREST)
	assert.Equal(t, int64(0), midCallJSONRPC)
	assert.Equal(t, int64(0), counters.REST())
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
