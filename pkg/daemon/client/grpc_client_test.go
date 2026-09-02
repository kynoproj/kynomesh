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

package client

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"

	pb "github.com/kynoproj/kynomesh/pkg/apis/proto/daemon"
	sharedtls "github.com/kynoproj/kynomesh/pkg/shared/tls"
)

// fakeServer satisfies pb.DaemonServiceServer with caller-supplied
// response or error. Used to drive the gRPC and REST clients against
// known outputs.
type fakeServer struct {
	pb.UnimplementedDaemonServiceServer
	resp *pb.GetAgentDeployMetricsResponse
	err  error

	lastReq *pb.GetAgentDeployMetricsRequest
}

func (f *fakeServer) GetAgentDeployMetrics(_ context.Context, req *pb.GetAgentDeployMetricsRequest) (*pb.GetAgentDeployMetricsResponse, error) {
	f.lastReq = req
	if f.err != nil {
		return nil, f.err
	}
	return f.resp, nil
}

// startGRPCServer brings up a TLS gRPC server on a random port,
// returns the address and a teardown func.
func startGRPCServer(t *testing.T, srv *fakeServer) (string, func()) {
	t.Helper()
	cert, err := sharedtls.GenerateX509KeyPair()
	require.NoError(t, err)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	creds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{*cert},
		MinVersion:   tls.VersionTLS12,
	})
	g := grpc.NewServer(grpc.Creds(creds))
	pb.RegisterDaemonServiceServer(g, srv)
	go func() { _ = g.Serve(lis) }()
	return lis.Addr().String(), func() {
		g.GracefulStop()
		_ = lis.Close()
	}
}

func TestGRPCClient_GetAgentDeployMetrics_HappyPath(t *testing.T) {
	srv := &fakeServer{
		resp: &pb.GetAgentDeployMetricsResponse{
			Metrics: &pb.AgentDeployMetrics{
				AgentSet:    "hello",
				AgentDeploy: "greeter",
				ProcessingRates: map[string]*wrapperspb.DoubleValue{
					"1m": wrapperspb.Double(2.5),
				},
				Inflights: map[string]*wrapperspb.DoubleValue{
					"1m": wrapperspb.Double(4),
				},
			},
		},
	}
	addr, stop := startGRPCServer(t, srv)
	defer stop()

	c, err := NewGRPCClient(addr)
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	m, err := c.GetAgentDeployMetrics(ctx, "greeter", 120)
	require.NoError(t, err)
	assert.Equal(t, "hello", m.GetAgentSet())
	assert.Equal(t, "greeter", m.GetAgentDeploy())
	assert.Equal(t, 2.5, m.GetProcessingRates()["1m"].GetValue())
	assert.Equal(t, float64(4), m.GetInflights()["1m"].GetValue())

	// Confirm request fields reached the server.
	assert.Equal(t, "greeter", srv.lastReq.GetName())
	assert.Equal(t, int64(120), srv.lastReq.GetLookbackSeconds())
}

func TestGRPCClient_GetAgentDeployMetrics_PropagatesStatus(t *testing.T) {
	srv := &fakeServer{err: status.Error(codes.NotFound, "unknown AgentDeploy \"missing\"")}
	addr, stop := startGRPCServer(t, srv)
	defer stop()

	c, err := NewGRPCClient(addr)
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	_, err = c.GetAgentDeployMetrics(ctx, "missing", 0)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok, "expected gRPC status")
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestGRPCClient_GetAgentDeployMetrics_UnavailablePropagates(t *testing.T) {
	srv := &fakeServer{err: status.Error(codes.Unavailable, "no samples yet")}
	addr, stop := startGRPCServer(t, srv)
	defer stop()

	c, err := NewGRPCClient(addr)
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	_, err = c.GetAgentDeployMetrics(ctx, "a", 0)
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Unavailable, st.Code())
}

func TestGRPCClient_DialFailureReturnsErrorOnCall(t *testing.T) {
	// grpc.NewClient is lazy. Constructor succeeds; the first RPC
	// call fails when the target can't be reached.
	c, err := NewGRPCClient("127.0.0.1:1") // closed port
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	_, err = c.GetAgentDeployMetrics(ctx, "x", 0)
	require.Error(t, err)
}

func TestGRPCClient_CloseIsIdempotent(t *testing.T) {
	srv := &fakeServer{resp: &pb.GetAgentDeployMetricsResponse{}}
	addr, stop := startGRPCServer(t, srv)
	defer stop()

	c, err := NewGRPCClient(addr)
	require.NoError(t, err)
	require.NoError(t, c.Close())
	// Second Close may return an error from grpc but must not panic.
	_ = c.Close()
}

// Defensive: a nil response Metrics field shouldn't crash the
// client. The .GetMetrics() generated accessor returns nil safely;
// callers see a nil *AgentDeployMetrics, not a panic.
func TestGRPCClient_NilMetricsResponse(t *testing.T) {
	srv := &fakeServer{resp: &pb.GetAgentDeployMetricsResponse{Metrics: nil}}
	addr, stop := startGRPCServer(t, srv)
	defer stop()

	c, err := NewGRPCClient(addr)
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	m, err := c.GetAgentDeployMetrics(ctx, "x", 0)
	require.NoError(t, err)
	assert.Nil(t, m)
}

func TestGRPCClient_ContextCancellation(t *testing.T) {
	// Server that blocks until ctx is canceled, simulating a slow
	// daemon.
	srv := &slowServer{block: time.Hour}
	addr, stop := startGRPCServerImpl(t, srv)
	defer stop()

	c, err := NewGRPCClient(addr)
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err = c.GetAgentDeployMetrics(ctx, "x", 0)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	// gRPC may surface either DeadlineExceeded or Canceled.
	assert.True(t, st.Code() == codes.DeadlineExceeded || st.Code() == codes.Canceled,
		"unexpected code: %s", st.Code())
}

type slowServer struct {
	pb.UnimplementedDaemonServiceServer
	block time.Duration
}

func (s *slowServer) GetAgentDeployMetrics(ctx context.Context, _ *pb.GetAgentDeployMetricsRequest) (*pb.GetAgentDeployMetricsResponse, error) {
	t := time.NewTimer(s.block)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-t.C:
		return &pb.GetAgentDeployMetricsResponse{}, nil
	}
}

func startGRPCServerImpl(t *testing.T, impl pb.DaemonServiceServer) (string, func()) {
	t.Helper()
	cert, err := sharedtls.GenerateX509KeyPair()
	require.NoError(t, err)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	creds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{*cert},
		MinVersion:   tls.VersionTLS12,
	})
	g := grpc.NewServer(grpc.Creds(creds))
	pb.RegisterDaemonServiceServer(g, impl)
	go func() { _ = g.Serve(lis) }()
	return lis.Addr().String(), func() {
		g.GracefulStop()
		_ = lis.Close()
	}
}

// Sanity: confirm we're using the same TLS approach as the daemon
// (skip-verify). Catches accidental tightening that would break
// against the daemon's self-signed cert.
func TestGRPCClient_AcceptsSelfSignedCert(t *testing.T) {
	srv := &fakeServer{resp: &pb.GetAgentDeployMetricsResponse{
		Metrics: &pb.AgentDeployMetrics{AgentSet: "hello", AgentDeploy: "ok"},
	}}
	addr, stop := startGRPCServer(t, srv)
	defer stop()

	c, err := NewGRPCClient(addr)
	require.NoError(t, err)
	defer func() { _ = c.Close() }()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	m, err := c.GetAgentDeployMetrics(ctx, "ok", 0)
	require.NoError(t, err)
	assert.Equal(t, "hello", m.GetAgentSet())
	assert.Equal(t, "ok", m.GetAgentDeploy())
}

func TestGRPCClient_TimeoutInformsServerThroughContext(t *testing.T) {
	// Server reports back through a channel the context error it
	// saw, proving the client propagated the deadline.
	seenErr := make(chan error, 1)
	srv := &contextWatchingServer{onCall: func(ctx context.Context) error {
		<-ctx.Done()
		seenErr <- ctx.Err()
		return ctx.Err()
	}}
	addr, stop := startGRPCServerImpl(t, srv)
	defer stop()

	c, err := NewGRPCClient(addr)
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, _ = c.GetAgentDeployMetrics(ctx, "x", 0)

	select {
	case got := <-seenErr:
		require.Error(t, got)
		assert.True(t,
			errors.Is(got, context.Canceled) || errors.Is(got, context.DeadlineExceeded),
			"unexpected ctx error: %v", got)
	case <-time.After(time.Second):
		t.Fatal("server did not observe context cancellation")
	}
}

type contextWatchingServer struct {
	pb.UnimplementedDaemonServiceServer
	onCall func(context.Context) error
}

func (s *contextWatchingServer) GetAgentDeployMetrics(ctx context.Context, _ *pb.GetAgentDeployMetricsRequest) (*pb.GetAgentDeployMetricsResponse, error) {
	if err := s.onCall(ctx); err != nil {
		return nil, status.Error(codes.Canceled, fmt.Sprintf("ctx: %v", err))
	}
	return &pb.GetAgentDeployMetricsResponse{}, nil
}
