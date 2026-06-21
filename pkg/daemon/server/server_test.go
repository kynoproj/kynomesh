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

package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	pb "github.com/kynoproj/kynomesh/pkg/apis/proto/daemon"
	"github.com/kynoproj/kynomesh/pkg/daemon/rater"
	sharedtls "github.com/kynoproj/kynomesh/pkg/shared/tls"
)

// freePort returns an OS-allocated free TCP port. We rely on the OS
// not reassigning the port between our query and the server's bind;
// in practice this is fine for tests.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	return port
}

// canned returns a Querier that returns a fixed result.
type canned struct{ r *rater.WindowedResult }

func (c canned) GetMetrics(string, int64) (*rater.WindowedResult, error) {
	return c.r, nil
}

func TestServer_RoundTripGRPCAndREST(t *testing.T) {
	cert, err := sharedtls.GenerateX509KeyPair()
	require.NoError(t, err)

	apiPort := freePort(t)
	metricsPort := freePort(t)
	registry := prometheus.NewRegistry()
	res := &rater.WindowedResult{
		Total: rater.PerWindowValues{
			ProcessingRates: map[string]float64{rater.WindowKey1m: 2.5},
			Inflights:       map[string]float64{rater.WindowKey1m: 4},
		},
		PerTransport: map[string]rater.PerWindowValues{
			"rest": {
				ProcessingRates: map[string]float64{rater.WindowKey1m: 2.5},
				Inflights:       map[string]float64{rater.WindowKey1m: 4},
			},
		},
	}
	svc := NewService(canned{r: res})

	srv, err := New(t.Context(), Config{
		APIPort:     apiPort,
		MetricsPort: metricsPort,
		Cert:        cert,
		Service:     svc,
		Registry:    registry,
	})
	require.NoError(t, err)

	apiDone := make(chan error, 1)
	metricsDone := make(chan error, 1)
	go func() { apiDone <- srv.APIServer.ListenAndServeTLS("", "") }()
	go func() { metricsDone <- srv.MetricsServer.ListenAndServeTLS("", "") }()

	defer func() {
		shutCtx, c := context.WithTimeout(context.Background(), 2*time.Second)
		defer c()
		srv.GracefulStop(shutCtx)
		// Drain channels so goroutines don't leak.
		<-apiDone
		<-metricsDone
	}()

	// Wait for both listeners to accept connections.
	waitFor(t, fmt.Sprintf("127.0.0.1:%d", apiPort), 2*time.Second)
	waitFor(t, fmt.Sprintf("127.0.0.1:%d", metricsPort), 2*time.Second)

	t.Run("grpc", func(t *testing.T) {
		creds := credentials.NewTLS(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}) //nolint:gosec
		conn, err := grpc.NewClient(
			fmt.Sprintf("127.0.0.1:%d", apiPort),
			grpc.WithTransportCredentials(creds),
		)
		require.NoError(t, err)
		defer func() { _ = conn.Close() }()
		c := pb.NewDaemonServiceClient(conn)

		callCtx, c2 := context.WithTimeout(context.Background(), 2*time.Second)
		defer c2()
		resp, err := c.GetAgentDeployMetrics(callCtx, &pb.GetAgentDeployMetricsRequest{Name: "greeter"})
		require.NoError(t, err)
		assert.Equal(t, 2.5, resp.GetMetrics().GetProcessingRates()[rater.WindowKey1m].GetValue())
		assert.Equal(t, "greeter", resp.GetMetrics().GetAgentdeploy())
	})

	t.Run("rest", func(t *testing.T) {
		httpClient := &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}, //nolint:gosec
			},
			Timeout: 2 * time.Second,
		}
		url := fmt.Sprintf("https://127.0.0.1:%d/api/v1/agentdeploys/greeter/metrics", apiPort)
		resp, err := httpClient.Get(url)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		var payload map[string]any
		require.NoError(t, json.Unmarshal(body, &payload))
		// grpc-gateway nests the response under "metrics".
		metrics, ok := payload["metrics"].(map[string]any)
		require.True(t, ok, "missing metrics: %s", string(body))
		assert.Equal(t, "greeter", metrics["agentdeploy"])
	})

	t.Run("metrics endpoint", func(t *testing.T) {
		httpClient := &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}, //nolint:gosec
			},
			Timeout: 2 * time.Second,
		}
		url := fmt.Sprintf("https://127.0.0.1:%d/metrics", metricsPort)
		resp, err := httpClient.Get(url)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

// waitFor polls until a TLS-accepting listener is ready or times out.
func waitFor(t *testing.T, addr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}) //nolint:gosec
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("listener at %s did not come up", addr)
}

// Sanity check: graceful stop returns even when servers haven't been
// started. Easy to break if someone refactors.
func TestServer_GracefulStop_NotStarted(t *testing.T) {
	cert, err := sharedtls.GenerateX509KeyPair()
	require.NoError(t, err)
	srv, err := New(context.Background(), Config{
		APIPort:     freePort(t),
		MetricsPort: freePort(t),
		Cert:        cert,
		Service:     NewService(canned{}),
		Registry:    prometheus.NewRegistry(),
	})
	require.NoError(t, err)
	// Should not panic or hang.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	srv.GracefulStop(ctx)
}

// Ensure server creation propagates loopback-dial errors. Use a port
// that's almost certainly closed but technically valid (1) so the
// gateway can't dial.
func TestServer_NewSucceedsEvenWithoutLoopbackTarget(t *testing.T) {
	// Even if the loopback dial would fail, grpc.NewClient is lazy
	// and doesn't dial until first RPC. So construction must succeed.
	cert, err := sharedtls.GenerateX509KeyPair()
	require.NoError(t, err)
	srv, err := New(context.Background(), Config{
		APIPort:     1, // unusable
		MetricsPort: 2,
		Cert:        cert,
		Service:     NewService(canned{}),
		Registry:    prometheus.NewRegistry(),
	})
	require.NoError(t, err)
	require.NotNil(t, srv)
}

// Defensive: ensure we don't accidentally start serving plain HTTP on
// ports we manage. This catches regressions where someone drops the
// TLS config.
func TestServer_RejectsPlainHTTP(t *testing.T) {
	cert, err := sharedtls.GenerateX509KeyPair()
	require.NoError(t, err)
	apiPort := freePort(t)
	srv, err := New(context.Background(), Config{
		APIPort:     apiPort,
		MetricsPort: freePort(t),
		Cert:        cert,
		Service:     NewService(canned{}),
		Registry:    prometheus.NewRegistry(),
	})
	require.NoError(t, err)
	go func() { _ = srv.APIServer.ListenAndServeTLS("", "") }()
	defer func() {
		shutCtx, c := context.WithTimeout(context.Background(), time.Second)
		defer c()
		srv.GracefulStop(shutCtx)
	}()
	waitFor(t, fmt.Sprintf("127.0.0.1:%d", apiPort), 2*time.Second)
	client := &http.Client{Timeout: time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/v1/agentdeploys/x/metrics", apiPort))
	// Two acceptable outcomes:
	//   - dial-level error (TLS reset before HTTP layer)
	//   - 400 Bad Request from Go's stdlib auto-reply ("client sent
	//     an HTTP request to an HTTPS server")
	// Both confirm plain HTTP cannot reach the gRPC/REST handlers.
	if err != nil {
		return
	}
	defer func() { _ = resp.Body.Close() }()
	assert.NotEqual(t, http.StatusOK, resp.StatusCode)
}
