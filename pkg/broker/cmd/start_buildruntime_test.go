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
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	sharedtls "github.com/kynoproj/kynomesh/pkg/shared/tls"
)

// withStubGRPCDial overrides the package-level dialAgentGRPC seam for the
// duration of the test so buildRuntime can take its gRPC branch without
// needing the production UDS path on disk.
func withStubGRPCDial(t *testing.T, fn func(string) (*grpc.ClientConn, error)) {
	t.Helper()
	prev := dialAgentGRPC
	dialAgentGRPC = fn
	t.Cleanup(func() { dialAgentGRPC = prev })
}

func TestBuildRuntime_CardBranches(t *testing.T) {
	cases := []struct {
		name        string
		card        *a2a.AgentCard
		wantEnabled []a2a.TransportProtocol
		wantHTTP    []a2a.TransportProtocol
		wantGRPC    bool
	}{
		{
			name: "JSONRPC only",
			card: &a2a.AgentCard{SupportedInterfaces: []*a2a.AgentInterface{
				{ProtocolBinding: a2a.TransportProtocolJSONRPC, URL: "http://x/rpc"},
			}},
			wantEnabled: []a2a.TransportProtocol{a2a.TransportProtocolJSONRPC},
			wantHTTP:    []a2a.TransportProtocol{a2a.TransportProtocolJSONRPC},
		},
		{
			name: "HTTPJSON only",
			card: &a2a.AgentCard{SupportedInterfaces: []*a2a.AgentInterface{
				{ProtocolBinding: a2a.TransportProtocolHTTPJSON, URL: "http://x/api"},
			}},
			wantEnabled: []a2a.TransportProtocol{a2a.TransportProtocolHTTPJSON},
			wantHTTP:    []a2a.TransportProtocol{a2a.TransportProtocolHTTPJSON},
		},
		{
			name: "GRPC only",
			card: &a2a.AgentCard{SupportedInterfaces: []*a2a.AgentInterface{
				{ProtocolBinding: a2a.TransportProtocolGRPC, URL: "x:50051"},
			}},
			wantEnabled: []a2a.TransportProtocol{a2a.TransportProtocolGRPC},
			wantGRPC:    true,
		},
		{
			name: "all three transports",
			card: &a2a.AgentCard{SupportedInterfaces: []*a2a.AgentInterface{
				{ProtocolBinding: a2a.TransportProtocolJSONRPC, URL: "http://x/rpc"},
				{ProtocolBinding: a2a.TransportProtocolHTTPJSON, URL: "http://x/api"},
				{ProtocolBinding: a2a.TransportProtocolGRPC, URL: "x:50051"},
			}},
			wantEnabled: []a2a.TransportProtocol{a2a.TransportProtocolJSONRPC, a2a.TransportProtocolHTTPJSON, a2a.TransportProtocolGRPC},
			wantHTTP:    []a2a.TransportProtocol{a2a.TransportProtocolJSONRPC, a2a.TransportProtocolHTTPJSON},
			wantGRPC:    true,
		},
		{
			name: "unknown ProtocolBinding is skipped with a warning",
			card: &a2a.AgentCard{SupportedInterfaces: []*a2a.AgentInterface{
				{ProtocolBinding: a2a.TransportProtocol("unknown"), URL: "http://x"},
				{ProtocolBinding: a2a.TransportProtocolJSONRPC, URL: "http://x/rpc"},
			}},
			wantEnabled: []a2a.TransportProtocol{a2a.TransportProtocolJSONRPC},
			wantHTTP:    []a2a.TransportProtocol{a2a.TransportProtocolJSONRPC},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withStubGRPCDial(t, func(_ string) (*grpc.ClientConn, error) {
				return grpc.NewClient("passthrough:///stub", grpc.WithTransportCredentials(insecure.NewCredentials()))
			})

			rt, err := buildRuntime(zap.NewNop().Sugar(), prometheus.NewRegistry(), &http.Transport{}, tc.card)
			require.NoError(t, err)
			require.NotNil(t, rt)

			for _, p := range tc.wantEnabled {
				assert.True(t, rt.enabled[p], "expected transport %s enabled", p)
			}
			assert.Len(t, rt.enabled, len(tc.wantEnabled), "enabled set size mismatch")

			for _, p := range tc.wantHTTP {
				_, ok := rt.httpProxies[p]
				assert.True(t, ok, "expected HTTP proxy for %s", p)
			}
			assert.Len(t, rt.httpProxies, len(tc.wantHTTP))

			if tc.wantGRPC {
				assert.NotNil(t, rt.grpcServer, "gRPC interface should create server")
				assert.NotNil(t, rt.grpcConn, "gRPC interface should create conn")
				if rt.grpcConn != nil {
					_ = rt.grpcConn.Close()
				}
			} else {
				assert.Nil(t, rt.grpcServer)
				assert.Nil(t, rt.grpcConn)
			}
			assert.NotNil(t, rt.passthrough)
		})
	}
}

// TestBuildRuntime_GRPCDialError ensures a dial failure surfaces as a
// wrapped error containing the socket path, so operators can diagnose
// missing or mis-permissioned sockets from the log line alone.
func TestBuildRuntime_GRPCDialError(t *testing.T) {
	sentinel := errors.New("dial blew up")
	withStubGRPCDial(t, func(_ string) (*grpc.ClientConn, error) {
		return nil, sentinel
	})

	card := &a2a.AgentCard{SupportedInterfaces: []*a2a.AgentInterface{
		{ProtocolBinding: a2a.TransportProtocolGRPC, URL: "x:50051"},
	}}
	rt, err := buildRuntime(zap.NewNop().Sugar(), prometheus.NewRegistry(), &http.Transport{}, card)
	require.Error(t, err)
	assert.Nil(t, rt)
	assert.ErrorIs(t, err, sentinel)
	assert.Contains(t, err.Error(), "dial agent gRPC over UDS")
}

// TestDialAgentGRPCOverUDS verifies the production helper produces a
// usable *grpc.ClientConn for a "unix://<path>" target. grpc.NewClient
// does lazy dialing so the socket does not need to exist for this call
// to succeed; what matters is the returned client is wired with the
// insecure-credential transport that the broker depends on.
func TestDialAgentGRPCOverUDS(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "agent.sock")
	conn, err := dialAgentGRPCOverUDS(sock)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	assert.NotNil(t, conn)
	assert.Equal(t, "unix://"+sock, conn.Target())
}

func TestEnabledTransportNames(t *testing.T) {
	cases := []struct {
		name string
		in   map[a2a.TransportProtocol]bool
		want []string
	}{
		{
			name: "empty input yields empty slice",
			in:   map[a2a.TransportProtocol]bool{},
			want: []string{},
		},
		{
			name: "single entry",
			in:   map[a2a.TransportProtocol]bool{a2a.TransportProtocolJSONRPC: true},
			want: []string{string(a2a.TransportProtocolJSONRPC)},
		},
		{
			name: "multiple entries",
			in: map[a2a.TransportProtocol]bool{
				a2a.TransportProtocolJSONRPC:  true,
				a2a.TransportProtocolHTTPJSON: true,
			},
			// Map iteration order is unspecified, so we compare as sets.
			want: []string{
				string(a2a.TransportProtocolJSONRPC),
				string(a2a.TransportProtocolHTTPJSON),
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := enabledTransportNames(tc.in)
			assert.ElementsMatch(t, tc.want, got)
		})
	}
}

// TestNewIntrospectionServer checks the constructor wires the bind
// address, handler, and TLS config the broker depends on. We do not
// boot the server here — that integration is exercised by the broker
// startup tests under pkg/broker.
func TestNewIntrospectionServer(t *testing.T) {
	cert, err := sharedtls.GenerateX509KeyPair()
	require.NoError(t, err)

	called := false
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })

	srv := newIntrospectionServer(9443, handler, cert)
	require.NotNil(t, srv)
	assert.Equal(t, ":9443", srv.Addr)
	require.NotNil(t, srv.TLSConfig)
	require.Len(t, srv.TLSConfig.Certificates, 1)
	assert.Equal(t, uint16(tls.VersionTLS12), srv.TLSConfig.MinVersion)
	require.NotNil(t, srv.Handler)

	// Exercise the handler indirectly so we know the constructor wired
	// the right one through.
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.True(t, called)
}
