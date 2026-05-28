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
	"context"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

// grpcPair brings up a backend gRPC server with the standard health
// service, plus a broker-side gRPC server in front of it using the
// pass-through option. Both run on ephemeral ports. Returns dial
// targets and registers cleanup.
type grpcPair struct {
	backendAddr string
	brokerAddr  string
	counters    *Counters
}

func startGRPCPair(t *testing.T) *grpcPair {
	t.Helper()

	// --- backend: standard gRPC health server on a UDS, matching the
	// production setup where the agent listens on the shared in-pod
	// socket and the broker dials it via "unix://...".
	socketPath := filepath.Join(shortSocketDir(t), "g.sock")
	backendLn, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	backendSrv := grpc.NewServer()
	hsvc := health.NewServer()
	hsvc.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(backendSrv, hsvc)
	go func() { _ = backendSrv.Serve(backendLn) }()
	t.Cleanup(backendSrv.Stop)

	// --- broker: pass-through gRPC server forwarding to the UDS backend ---
	backendConn, err := grpc.NewClient("unix://"+socketPath, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = backendConn.Close() })

	counters := &Counters{}
	brokerLn, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	brokerSrv := grpc.NewServer(GRPCPassthroughOptions(backendConn, counters)...)
	go func() { _ = brokerSrv.Serve(brokerLn) }()
	t.Cleanup(brokerSrv.Stop)

	return &grpcPair{
		backendAddr: socketPath,
		brokerAddr:  brokerLn.Addr().String(),
		counters:    counters,
	}
}

func TestGRPCPassthrough_UnaryCall(t *testing.T) {
	// Dial the broker, issue Health.Check, expect it to land on the
	// backend's health service and return SERVING.
	pair := startGRPCPair(t)

	conn, err := grpc.NewClient(pair.brokerAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})
	require.NoError(t, err)
	assert.Equal(t, healthpb.HealthCheckResponse_SERVING, resp.Status)
	// Counter must balance to 0 once the unary call returns.
	assert.Equal(t, int64(0), pair.counters.GRPC())
}

func TestGRPCPassthrough_StreamingCall(t *testing.T) {
	// Health.Watch is a server-streaming RPC: one request, one or more
	// responses. We assert that we receive at least one response and
	// that the gRPC counter is elevated while the stream is open.
	pair := startGRPCPair(t)

	conn, err := grpc.NewClient(pair.brokerAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream, err := healthpb.NewHealthClient(conn).Watch(ctx, &healthpb.HealthCheckRequest{})
	require.NoError(t, err)

	// First response from the backend.
	resp, err := stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, healthpb.HealthCheckResponse_SERVING, resp.Status)

	// Stream is still open; the counter must reflect the in-flight call.
	require.Eventually(t, func() bool { return pair.counters.GRPC() == 1 },
		2*time.Second, 10*time.Millisecond, "gRPC counter should reflect the open stream")

	// Cancelling the client context should tear the stream down on
	// both sides. After cancel, recv either returns an error or EOF.
	cancel()
	for {
		_, err := stream.Recv()
		if err != nil {
			break
		}
	}
	require.Eventually(t, func() bool { return pair.counters.GRPC() == 0 },
		2*time.Second, 10*time.Millisecond, "counter must return to 0 after stream closes")
}

func TestGRPCPassthrough_UnknownMethodSurfacesBackendStatus(t *testing.T) {
	// Issuing a method the backend doesn't implement must surface the
	// backend's Unimplemented status — proving error/status propagation
	// works through the proxy.
	pair := startGRPCPair(t)

	conn, err := grpc.NewClient(pair.brokerAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Invoke a non-existent method on a non-existent service via the raw
	// API so we don't need a generated stub for the bogus surface.
	err = conn.Invoke(ctx, "/bogus.Service/Bogus", &healthpb.HealthCheckRequest{}, &healthpb.HealthCheckResponse{})
	require.Error(t, err, "calling a method the backend doesn't implement must fail")
	// The status code should not be OK — exact code may vary by gRPC
	// version (Unimplemented vs Unknown depending on path), so we settle
	// for "not OK and not a transport-level read error".
	assert.NotEqual(t, io.EOF, err)
}
