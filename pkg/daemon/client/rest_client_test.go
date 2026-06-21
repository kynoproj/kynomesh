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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/wrapperspb"

	pb "github.com/kynoproj/kynomesh/pkg/apis/proto/daemon"
)

// startRESTServer brings up a TLS httptest server that lets the
// test inspect each request and return canned JSON.
type restServerState struct {
	lastPath        string
	lastEscapedPath string
	lastQuery       string
	status          int
	body            string
}

func startRESTServer(t *testing.T, st *restServerState) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		st.lastPath = r.URL.Path
		st.lastEscapedPath = r.URL.EscapedPath()
		st.lastQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(st.status)
		_, _ = w.Write([]byte(st.body))
	}))
	srv.TLS = &tls.Config{MinVersion: tls.VersionTLS12}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

// canonicalBody returns the JSON wire form that the daemon's
// grpc-gateway would produce for a response containing the given
// values. Built dynamically so the test stays decoupled from the
// generated proto field names.
func canonicalBody(t *testing.T, m *pb.AgentDeployMetrics) string {
	t.Helper()
	b, err := jsonMarshaller.Marshal(&pb.GetAgentDeployMetricsResponse{Metrics: m})
	require.NoError(t, err)
	return string(b)
}

func TestRESTClient_GetAgentDeployMetrics_HappyPath(t *testing.T) {
	st := &restServerState{status: http.StatusOK}
	st.body = canonicalBody(t, &pb.AgentDeployMetrics{
		Agentdeploy: "greeter",
		ProcessingRates: map[string]*wrapperspb.DoubleValue{
			"1m": wrapperspb.Double(2.5),
		},
		Inflights: map[string]*wrapperspb.DoubleValue{
			"1m": wrapperspb.Double(4),
		},
	})
	srv := startRESTServer(t, st)
	c, err := NewRESTClient(srv.URL)
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	m, err := c.GetAgentDeployMetrics(ctx, "greeter", 120)
	require.NoError(t, err)
	assert.Equal(t, "greeter", m.GetAgentdeploy())
	assert.Equal(t, 2.5, m.GetProcessingRates()["1m"].GetValue())
	assert.Equal(t, float64(4), m.GetInflights()["1m"].GetValue())
	assert.Equal(t, "/api/v1/agentdeploys/greeter/metrics", st.lastPath)
	assert.Equal(t, "lookback_seconds=120", st.lastQuery)
}

func TestRESTClient_ZeroLookbackOmitsQueryParam(t *testing.T) {
	st := &restServerState{status: http.StatusOK}
	st.body = canonicalBody(t, &pb.AgentDeployMetrics{Agentdeploy: "x"})
	srv := startRESTServer(t, st)
	c, err := NewRESTClient(srv.URL)
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	_, err = c.GetAgentDeployMetrics(ctx, "x", 0)
	require.NoError(t, err)
	assert.Empty(t, st.lastQuery, "lookback=0 should produce no query string")
}

func TestRESTClient_AddressWithoutSchemeAssumedHTTPS(t *testing.T) {
	st := &restServerState{status: http.StatusOK}
	st.body = canonicalBody(t, &pb.AgentDeployMetrics{Agentdeploy: "x"})
	srv := startRESTServer(t, st)
	// Strip "https://" so we can verify the client adds it back.
	bare := strings.TrimPrefix(srv.URL, "https://")
	c, err := NewRESTClient(bare)
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	_, err = c.GetAgentDeployMetrics(ctx, "x", 0)
	require.NoError(t, err)
}

func TestRESTClient_404SurfacesAsError(t *testing.T) {
	st := &restServerState{status: http.StatusNotFound, body: `{"code":5,"message":"unknown AgentDeploy"}`}
	srv := startRESTServer(t, st)
	c, err := NewRESTClient(srv.URL)
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	_, err = c.GetAgentDeployMetrics(ctx, "missing", 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestRESTClient_503SurfacesAsError(t *testing.T) {
	st := &restServerState{status: http.StatusServiceUnavailable, body: `{"code":14}`}
	srv := startRESTServer(t, st)
	c, err := NewRESTClient(srv.URL)
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	_, err = c.GetAgentDeployMetrics(ctx, "a", 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "503")
}

func TestRESTClient_NameIsPathEscaped(t *testing.T) {
	st := &restServerState{status: http.StatusOK}
	st.body = canonicalBody(t, &pb.AgentDeployMetrics{Agentdeploy: "x"})
	srv := startRESTServer(t, st)
	c, err := NewRESTClient(srv.URL)
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	// K8s names can't contain spaces, but the client must not
	// silently mis-encode anything that could be passed in. We
	// assert against EscapedPath, which preserves what was actually
	// transmitted on the wire.
	_, err = c.GetAgentDeployMetrics(ctx, "weird name", 0)
	require.NoError(t, err)
	assert.Equal(t, "/api/v1/agentdeploys/weird%20name/metrics", st.lastEscapedPath)
}

func TestRESTClient_BodyDecodeError(t *testing.T) {
	st := &restServerState{status: http.StatusOK, body: "not-json"}
	srv := startRESTServer(t, st)
	c, err := NewRESTClient(srv.URL)
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	_, err = c.GetAgentDeployMetrics(ctx, "x", 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode")
}

func TestRESTClient_DialFailureReturnsError(t *testing.T) {
	c, err := NewRESTClient("127.0.0.1:1") // unbound
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	_, err = c.GetAgentDeployMetrics(ctx, "x", 0)
	require.Error(t, err)
}

func TestRESTClient_ContextCancellation(t *testing.T) {
	// Server that holds the request until the client disconnects.
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	srv.TLS = &tls.Config{MinVersion: tls.VersionTLS12}
	srv.StartTLS()
	defer srv.Close()

	c, err := NewRESTClient(srv.URL)
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = c.GetAgentDeployMetrics(ctx, "x", 0)
	require.Error(t, err)
	assert.Less(t, time.Since(start), time.Second,
		"client must give up promptly when context expires")
}

// Sanity-check the constructor's URL handling: full URL passes
// through, trailing slash is trimmed.
func TestRESTClient_TrailingSlashTrimmed(t *testing.T) {
	st := &restServerState{status: http.StatusOK}
	st.body = canonicalBody(t, &pb.AgentDeployMetrics{Agentdeploy: "x"})
	srv := startRESTServer(t, st)
	c, err := NewRESTClient(srv.URL + "/")
	require.NoError(t, err)
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	_, err = c.GetAgentDeployMetrics(ctx, "x", 0)
	require.NoError(t, err)
	// Path should still have a single leading slash, not two.
	assert.Equal(t, "/api/v1/agentdeploys/x/metrics", st.lastPath)
}
