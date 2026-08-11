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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sharedtls "github.com/kynoproj/kynomesh/pkg/shared/tls"
)

// TestProbeAgentCard exercises the narrowing rule: a 404 from the
// well-known path means "agent reachable but has no AgentCard" — every
// other error stays fatal so the startup liveness gate is preserved.
func TestProbeAgentCard(t *testing.T) {
	cases := []struct {
		name        string
		handler     http.HandlerFunc
		wantNilCard bool
		wantErr     bool
	}{
		{
			name: "404 means no card, nil error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.NotFound(w, nil)
			},
			wantNilCard: true,
			wantErr:     false,
		},
		{
			name: "500 propagates as error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "boom", http.StatusInternalServerError)
			},
			wantNilCard: true,
			wantErr:     true,
		},
		{
			name: "malformed JSON propagates as error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`}{`))
			},
			wantNilCard: true,
			wantErr:     true,
		},
		{
			name: "200 with valid card returns card",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"name":"agent-1"}`))
			},
			wantNilCard: false,
			wantErr:     false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != a2asrv.WellKnownAgentCardPath {
					http.NotFound(w, r)
					return
				}
				tc.handler(w, r)
			}))
			t.Cleanup(ts.Close)

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			t.Cleanup(cancel)
			card, err := probeAgentCard(ctx, ts.Client(), ts.URL)

			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			if tc.wantNilCard {
				assert.Nil(t, card)
			} else {
				assert.NotNil(t, card)
			}
		})
	}
}

// TestProbeAgentCard_DialError covers the "agent unreachable" case — the
// startup gate must still fail when no listener is on the other end.
func TestProbeAgentCard_DialError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	t.Cleanup(cancel)
	card, err := probeAgentCard(ctx, &http.Client{Timeout: 500 * time.Millisecond}, "http://127.0.0.1:1")
	assert.Error(t, err)
	assert.Nil(t, card)
}

// TestBuildRuntime_NilCard documents the contract: a nil card produces a
// runtime with the catch-all passthrough wired up and no per-transport
// A2A proxies. The broker boots and forwards everything to the agent's
// own HTTP surface.
func TestBuildRuntime_NilCard(t *testing.T) {
	rt, err := buildRuntime(context.TODO(), prometheus.NewRegistry(), &http.Transport{}, nil, nil, agentDial{udsPath: "/tmp/stub"})
	require.NoError(t, err)
	require.NotNil(t, rt)
	assert.NotNil(t, rt.passthrough, "passthrough must be wired so non-A2A traffic still reaches the agent")
	assert.Empty(t, rt.enabled, "no AgentCard means no A2A transports advertised")
	assert.Empty(t, rt.httpProxies, "no AgentCard means no per-transport proxies")
	assert.Nil(t, rt.grpcServer)
	assert.Nil(t, rt.grpcConn)
}

// TestMultiplexedServer_NoCardHandler exercises the passthrough-only
// runtime: with cardHandler nil, the well-known AgentCard path is not
// short-circuited by the mux and falls through to the catch-all (which a
// real agent could respond to as it wishes — typically 404). Canonical
// A2A routes that are not enabled likewise fall through, since
// per-transport proxy slots are empty.
func TestMultiplexedServer_NoCardHandler(t *testing.T) {
	rt, err := buildRuntime(context.TODO(), prometheus.NewRegistry(), &http.Transport{}, nil, nil, agentDial{udsPath: "/tmp/stub"})
	require.NoError(t, err)
	// Swap the production passthrough (which would try to dial the UDS
	// agent) for a stub so we can assert routing without an upstream.
	rt.passthrough = stubOKHandler("passthrough-ok")

	cert, err := sharedtls.GenerateX509KeyPair()
	require.NoError(t, err)
	srv, ln, err := newMultiplexedServer(0, rt, nil, cert)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	ts := httptest.NewServer(srv.Handler)
	t.Cleanup(ts.Close)

	t.Run("well-known card path falls through to passthrough", func(t *testing.T) {
		resp, err := http.Get(ts.URL + a2asrv.WellKnownAgentCardPath)
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("arbitrary non-A2A path reaches the passthrough", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/users/me")
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}
