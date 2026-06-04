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
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
	sharedtls "github.com/kynoproj/kynomesh/pkg/shared/tls"
)

// withStubGRPCDial overrides dialAgentGRPC for the duration of the test.
func withStubGRPCDial(t *testing.T, fn func(agentDial) (*grpc.ClientConn, error)) {
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
			withStubGRPCDial(t, func(_ agentDial) (*grpc.ClientConn, error) {
				return grpc.NewClient("passthrough:///stub", grpc.WithTransportCredentials(insecure.NewCredentials()))
			})

			rt, err := buildRuntime(context.TODO(), prometheus.NewRegistry(), &http.Transport{}, tc.card, nil, agentDial{udsPath: "/tmp/stub"})
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

// TestBuildRuntime_StashesAgentDeploy confirms the AgentDeploy decoded
// at startup is reachable on brokerRuntime for downstream consumers
// (no re-decoding from env required later).
func TestBuildRuntime_StashesAgentDeploy(t *testing.T) {
	want := &kmv1.AgentDeploy{}
	want.Namespace = "demo-ns"
	want.Name = "demo-ad"

	rt, err := buildRuntime(context.TODO(), prometheus.NewRegistry(), &http.Transport{}, nil, want, agentDial{udsPath: "/tmp/stub"})
	require.NoError(t, err)
	require.NotNil(t, rt)
	assert.Same(t, want, rt.agentDeploy, "buildRuntime should stash the AgentDeploy pointer verbatim")
}

// TestBuildRuntime_GRPCDialError ensures a dial failure surfaces as a
// wrapped error containing the dial target, so operators can diagnose
// missing or mis-permissioned agents from the log line alone.
func TestBuildRuntime_GRPCDialError(t *testing.T) {
	sentinel := errors.New("dial blew up")
	withStubGRPCDial(t, func(_ agentDial) (*grpc.ClientConn, error) {
		return nil, sentinel
	})

	card := &a2a.AgentCard{SupportedInterfaces: []*a2a.AgentInterface{
		{ProtocolBinding: a2a.TransportProtocolGRPC, URL: "x:50051"},
	}}
	rt, err := buildRuntime(context.TODO(), prometheus.NewRegistry(), &http.Transport{}, card, nil, agentDial{udsPath: "/tmp/stub"})
	require.Error(t, err)
	assert.Nil(t, rt)
	assert.ErrorIs(t, err, sentinel)
	assert.Contains(t, err.Error(), "dial agent gRPC")
}

func TestDialAgentGRPCDefault_UDS(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "agent.sock")
	conn, err := dialAgentGRPCDefault(agentDial{udsPath: sock})
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	assert.Equal(t, "unix://"+sock, conn.Target())
}

func TestDialAgentGRPCDefault_TCP(t *testing.T) {
	conn, err := dialAgentGRPCDefault(agentDial{tcpAddr: "127.0.0.1:8001"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	assert.Equal(t, "127.0.0.1:8001", conn.Target())
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
