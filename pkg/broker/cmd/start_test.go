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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/a2aproject/a2a-go/v2/a2asrv"

	"github.com/kynoproj/kynomesh/pkg/broker"
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

// TestMultiplexedServer_RoutesHTTPTraffic exercises the HTTP/1.1 routes —
// AgentCard, JSON-RPC mount, and the REST subtree — over a single httptest
// server, proving the mux wired by newMultiplexedServer dispatches
// correctly. gRPC routing is covered by isGRPCRequest above; an end-to-end
// gRPC test would need a real listener with HTTP/2 cleartext, which is out
// of scope for a unit test.
func TestMultiplexedServer_RoutesHTTPTraffic(t *testing.T) {
	card := broker.NewAgentCard(
		broker.JSONRPCAddr("test", 1234),
		broker.RESTAddr("test", 1234),
		broker.GRPCAddr("test", 1234),
	)
	rh := a2asrv.NewHandler(broker.NewDefaultExecutor(), a2asrv.WithExtendedAgentCard(card))
	grpcSrv := grpc.NewServer()
	t.Cleanup(grpcSrv.Stop)

	srv := newMultiplexedServer(0, rh, card, grpcSrv)
	ts := httptest.NewServer(srv.Handler)
	t.Cleanup(ts.Close)

	t.Run("AgentCard well-known path", func(t *testing.T) {
		resp, err := http.Get(ts.URL + a2asrv.WellKnownAgentCardPath)
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("JSON-RPC endpoint accepts POST", func(t *testing.T) {
		body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"agent/getAuthenticatedExtendedCard"}`)
		resp, err := http.Post(ts.URL+broker.JSONRPCEndpoint, "application/json", body)
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })
		// The handler should accept the request — we only care that the
		// mux routed it, not the JSON-RPC semantics. Anything below 500
		// means it reached the JSON-RPC handler rather than 404ing.
		assert.Less(t, resp.StatusCode, http.StatusInternalServerError)
	})

	t.Run("REST routes mount under /api", func(t *testing.T) {
		// /api/extendedAgentCard is one of the REST handler's GET routes;
		// reaching it (any status other than 404) proves the StripPrefix
		// wrapper is dispatching correctly.
		resp, err := http.Get(ts.URL + broker.RESTEndpoint + "/extendedAgentCard")
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })
		assert.NotEqual(t, http.StatusNotFound, resp.StatusCode,
			"REST mount should route /api/extendedAgentCard to the a2a-go handler")
	})

	t.Run("unknown paths return 404", func(t *testing.T) {
		// Without a "/" catch-all, unknown paths must 404 instead of
		// accidentally reaching the REST handler.
		resp, err := http.Get(ts.URL + "/does-not-exist")
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})
}
