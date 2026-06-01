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
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// allTransportsEnabled is a convenience for tests that don't care about
// transport filtering — every standard A2A binding is in the set.
func allTransportsEnabled() map[a2a.TransportProtocol]bool {
	return map[a2a.TransportProtocol]bool{
		a2a.TransportProtocolJSONRPC:  true,
		a2a.TransportProtocolHTTPJSON: true,
		a2a.TransportProtocolGRPC:     true,
	}
}

// fakeUDSAgent brings up an HTTP server bound to a Unix Domain Socket
// in t.TempDir() and serves the supplied AgentCard at the well-known
// path. Returns the socket path so the test can build a UDS client
// against it. The server shuts down via t.Cleanup.
func fakeUDSAgent(t *testing.T, card *a2a.AgentCard) string {
	t.Helper()
	socketPath := filepath.Join(shortSocketDir(t), "a.sock")
	mux := http.NewServeMux()
	mux.HandleFunc(a2asrv.WellKnownAgentCardPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(card))
	})
	ln, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return socketPath
}

func TestAgentCardProxy_RewritesInterfaceURLs(t *testing.T) {
	originalCard := &a2a.AgentCard{
		Name:         "user-agent",
		Description:  "user-supplied agent",
		Capabilities: a2a.AgentCapabilities{Streaming: true},
		SupportedInterfaces: []*a2a.AgentInterface{
			{URL: "http://localhost:8000/rpc", ProtocolBinding: a2a.TransportProtocolJSONRPC},
			{URL: "http://localhost:8000/api", ProtocolBinding: a2a.TransportProtocolHTTPJSON},
			{URL: "localhost:8000", ProtocolBinding: a2a.TransportProtocolGRPC},
		},
	}
	socketPath := fakeUDSAgent(t, originalCard)

	proxy := NewAgentCardProxy(NewUDSHTTPClient(socketPath), "broker.example.com", 9100, allTransportsEnabled())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", a2asrv.WellKnownAgentCardPath, nil)
	proxy.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var got a2a.AgentCard
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))

	// Identity / metadata fields are passed through unchanged — the
	// broker doesn't claim the agent's identity, just relays it.
	assert.Equal(t, "user-agent", got.Name)
	assert.Equal(t, "user-supplied agent", got.Description)
	assert.True(t, got.Capabilities.Streaming)

	// Interface URLs are rewritten to the broker's external endpoint.
	wantPort := 9100
	require.Len(t, got.SupportedInterfaces, 3)
	for _, iface := range got.SupportedInterfaces {
		switch iface.ProtocolBinding {
		case a2a.TransportProtocolJSONRPC:
			assert.Equal(t, "https://broker.example.com:"+strconv.Itoa(wantPort)+JSONRPCEndpoint, iface.URL)
		case a2a.TransportProtocolHTTPJSON:
			assert.Equal(t, "https://broker.example.com:"+strconv.Itoa(wantPort)+RESTEndpoint, iface.URL)
		case a2a.TransportProtocolGRPC:
			assert.Equal(t, "broker.example.com:"+strconv.Itoa(wantPort), iface.URL)
		}
	}
}

func TestAgentCardProxy_StripsDisabledTransports(t *testing.T) {
	originalCard := &a2a.AgentCard{
		Name: "user-agent",
		SupportedInterfaces: []*a2a.AgentInterface{
			{URL: "http://localhost:8000/rpc", ProtocolBinding: a2a.TransportProtocolJSONRPC},
			{URL: "http://localhost:8000/api", ProtocolBinding: a2a.TransportProtocolHTTPJSON},
			{URL: "localhost:8000", ProtocolBinding: a2a.TransportProtocolGRPC},
			{URL: "http://localhost:8000/custom", ProtocolBinding: a2a.TransportProtocol("CUSTOM")},
		},
	}
	socketPath := fakeUDSAgent(t, originalCard)

	enabled := map[a2a.TransportProtocol]bool{a2a.TransportProtocolJSONRPC: true}
	proxy := NewAgentCardProxy(NewUDSHTTPClient(socketPath), "broker.example.com", 9100, enabled)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", a2asrv.WellKnownAgentCardPath, nil)
	proxy.ServeHTTP(rec, req)

	var got a2a.AgentCard
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got.SupportedInterfaces, 1)
	assert.Equal(t, a2a.TransportProtocolJSONRPC, got.SupportedInterfaces[0].ProtocolBinding)
}

func TestAgentCardProxy_AgentUnreachableReturns502(t *testing.T) {
	// Dial a UDS path that doesn't exist — every fetch must surface a
	// 502 rather than fall back to anything cached.
	deadSocket := filepath.Join(shortSocketDir(t), "x.sock")
	proxy := NewAgentCardProxy(NewUDSHTTPClient(deadSocket), "broker.example.com", 9100, allTransportsEnabled())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", a2asrv.WellKnownAgentCardPath, nil)
	proxy.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestAgentCardProxy_FetchesFreshOnEachRequest(t *testing.T) {
	// Mutating the agent's card between two proxy calls must be visible
	// to the second caller — no caching layer.
	currentCard := &a2a.AgentCard{Name: "v1"}
	socketPath := fakeUDSAgent(t, currentCard)

	proxy := NewAgentCardProxy(NewUDSHTTPClient(socketPath), "broker.example.com", 9100, allTransportsEnabled())

	rec1 := httptest.NewRecorder()
	proxy.ServeHTTP(rec1, httptest.NewRequest("GET", a2asrv.WellKnownAgentCardPath, nil))

	// Rename the agent and serve again.
	*currentCard = a2a.AgentCard{Name: "v2"}

	rec2 := httptest.NewRecorder()
	proxy.ServeHTTP(rec2, httptest.NewRequest("GET", a2asrv.WellKnownAgentCardPath, nil))

	var got1, got2 a2a.AgentCard
	require.NoError(t, json.Unmarshal(rec1.Body.Bytes(), &got1))
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &got2))
	assert.Equal(t, "v1", got1.Name)
	assert.Equal(t, "v2", got2.Name)
}
