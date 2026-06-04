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

package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

// shortSocketDir returns a tempdir whose path fits inside sockaddr_un.sun_path
// (~104 bytes on macOS). t.TempDir() embeds the test name and can overflow.
func shortSocketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "k")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func startGRPCHealthServer(t *testing.T, socket string, status healthpb.HealthCheckResponse_ServingStatus) {
	t.Helper()
	ln, err := net.Listen("unix", socket)
	require.NoError(t, err)

	srv := grpc.NewServer()
	hs := health.NewServer()
	hs.SetServingStatus("", status)
	healthpb.RegisterHealthServer(srv, hs)

	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() {
		srv.Stop()
		_ = ln.Close()
	})
}

func startHTTPServer(t *testing.T, socket string, status int) {
	t.Helper()
	ln, err := net.Listen("unix", socket)
	require.NoError(t, err)

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
	}))
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
}

func TestRun_RejectsMissingSocket(t *testing.T) {
	err := Run(context.Background(), probeConfig{Mode: ModeGRPC})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--socket is required")
}

func TestRun_RejectsUnknownMode(t *testing.T) {
	err := Run(context.Background(), probeConfig{Mode: "tcp", Socket: "/tmp/x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown --mode "tcp"`)
}

func TestRun_GRPC(t *testing.T) {
	cases := []struct {
		name       string
		status     healthpb.HealthCheckResponse_ServingStatus
		wantErr    bool
		wantSubstr string
	}{
		{name: "serving", status: healthpb.HealthCheckResponse_SERVING},
		{name: "not_serving", status: healthpb.HealthCheckResponse_NOT_SERVING, wantErr: true, wantSubstr: "NOT_SERVING"},
		{name: "unknown", status: healthpb.HealthCheckResponse_UNKNOWN, wantErr: true, wantSubstr: "UNKNOWN"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			socket := filepath.Join(shortSocketDir(t), "g.sock")
			startGRPCHealthServer(t, socket, tc.status)

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			err := Run(ctx, probeConfig{Mode: ModeGRPC, Socket: socket, Timeout: 2 * time.Second})
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantSubstr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestRun_GRPC_SocketMissing(t *testing.T) {
	socket := filepath.Join(shortSocketDir(t), "absent.sock")
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := Run(ctx, probeConfig{Mode: ModeGRPC, Socket: socket, Timeout: 500 * time.Millisecond})
	require.Error(t, err)
}

func TestRun_HTTP(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		wantErr bool
	}{
		{name: "ok", status: http.StatusOK},
		{name: "no_content", status: http.StatusNoContent},
		{name: "server_error", status: http.StatusInternalServerError, wantErr: true},
		{name: "service_unavailable", status: http.StatusServiceUnavailable, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			socket := filepath.Join(shortSocketDir(t), "h.sock")
			startHTTPServer(t, socket, tc.status)

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			err := Run(ctx, probeConfig{Mode: ModeHTTP, Socket: socket, HTTPPath: "/healthz", Timeout: 2 * time.Second})
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestRun_HTTP_SocketMissing(t *testing.T) {
	socket := filepath.Join(shortSocketDir(t), "absent.sock")
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := Run(ctx, probeConfig{Mode: ModeHTTP, Socket: socket, HTTPPath: "/healthz", Timeout: 500 * time.Millisecond})
	require.Error(t, err)
}
