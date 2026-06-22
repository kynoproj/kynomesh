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

package scraper

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTLSServer wraps an httptest TLS server and returns the host and
// port a Scraper would target (httptest binds 127.0.0.1 by default).
func newTLSServer(t *testing.T, body string, status int) (host string, port int, close func()) {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metrics" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	srv.TLS = &tls.Config{MinVersion: tls.VersionTLS12}
	srv.StartTLS()

	u, err := url.Parse(srv.URL)
	require.NoError(t, err)
	h, p, err := net.SplitHostPort(u.Host)
	require.NoError(t, err)
	pi, err := strconv.Atoi(p)
	require.NoError(t, err)
	return h, pi, srv.Close
}

// newScraperPointingAt replaces the introspection port the Scraper
// uses so tests can drive httptest's random port.
func newScraperPointingAt(port int) *Scraper {
	s := New(2 * time.Second)
	s.port = port
	return s
}

const sampleMetrics = `# HELP broker_inflight_requests Number of in-flight requests
# TYPE broker_inflight_requests gauge
broker_inflight_requests{transport="jsonrpc"} 2
broker_inflight_requests{transport="rest"} 5
broker_inflight_requests{transport="grpc"} 1
broker_inflight_requests{transport="passthrough"} 0
# HELP broker_requests_total Total requests
# TYPE broker_requests_total counter
broker_requests_total{transport="jsonrpc"} 100
broker_requests_total{transport="rest"} 250
broker_requests_total{transport="grpc"} 50
broker_requests_total{transport="passthrough"} 0
# HELP broker_stream_messages_total Total stream messages
# TYPE broker_stream_messages_total counter
broker_stream_messages_total{transport="rest"} 17
broker_stream_messages_total{transport="grpc"} 9
`

func TestScrape_HappyPath(t *testing.T) {
	host, port, closeSrv := newTLSServer(t, sampleMetrics, http.StatusOK)
	defer closeSrv()
	s := newScraperPointingAt(port)

	sample, err := s.Scrape(context.Background(), host)
	require.NoError(t, err)
	assert.Equal(t, float64(2), sample.InflightByTransport["jsonrpc"])
	assert.Equal(t, float64(5), sample.InflightByTransport["rest"])
	assert.Equal(t, float64(1), sample.InflightByTransport["grpc"])
	assert.Equal(t, float64(0), sample.InflightByTransport["passthrough"])
	// ProcessedByTransport sums requests_total + stream_messages_total.
	assert.Equal(t, float64(100), sample.ProcessedByTransport["jsonrpc"], "jsonrpc has no stream messages")
	assert.Equal(t, float64(267), sample.ProcessedByTransport["rest"], "rest = 250 requests + 17 SSE events")
	assert.Equal(t, float64(59), sample.ProcessedByTransport["grpc"], "grpc = 50 streams + 9 server frames")
	assert.Equal(t, float64(0), sample.ProcessedByTransport["passthrough"])
}

func TestScrape_LabelAgnostic_UnknownTransport(t *testing.T) {
	body := `# HELP broker_inflight_requests x
# TYPE broker_inflight_requests gauge
broker_inflight_requests{transport="someNewProtocol"} 9
`
	host, port, closeSrv := newTLSServer(t, body, http.StatusOK)
	defer closeSrv()
	s := newScraperPointingAt(port)

	sample, err := s.Scrape(context.Background(), host)
	require.NoError(t, err)
	assert.Equal(t, float64(9), sample.InflightByTransport["someNewProtocol"])
}

func TestScrape_MetricsWithoutTransportLabelSkipped(t *testing.T) {
	body := `# HELP broker_inflight_requests x
# TYPE broker_inflight_requests gauge
broker_inflight_requests 99
`
	host, port, closeSrv := newTLSServer(t, body, http.StatusOK)
	defer closeSrv()
	s := newScraperPointingAt(port)

	sample, err := s.Scrape(context.Background(), host)
	require.NoError(t, err)
	assert.Empty(t, sample.InflightByTransport)
}

func TestScrape_MissingMetricsYieldsEmptySample(t *testing.T) {
	// An empty /metrics body is valid Prometheus output.
	host, port, closeSrv := newTLSServer(t, "", http.StatusOK)
	defer closeSrv()
	s := newScraperPointingAt(port)

	sample, err := s.Scrape(context.Background(), host)
	require.NoError(t, err)
	assert.NotNil(t, sample)
	assert.Empty(t, sample.InflightByTransport)
	assert.Empty(t, sample.ProcessedByTransport)
}

func TestScrape_OnlyRequestsCounterPresent(t *testing.T) {
	// Mid-upgrade scenario: broker emits requests_total but
	// stream_messages_total hasn't rolled out yet. Sum still works,
	// stream side just contributes 0.
	body := `# HELP broker_requests_total x
# TYPE broker_requests_total counter
broker_requests_total{transport="rest"} 42
`
	host, port, closeSrv := newTLSServer(t, body, http.StatusOK)
	defer closeSrv()
	s := newScraperPointingAt(port)

	sample, err := s.Scrape(context.Background(), host)
	require.NoError(t, err)
	assert.Equal(t, float64(42), sample.ProcessedByTransport["rest"])
	assert.Empty(t, sample.InflightByTransport)
}

func TestScrape_OnlyStreamMessagesCounterPresent(t *testing.T) {
	// Defensive: counter-name-agnostic summing should also handle
	// the inverse case.
	body := `# HELP broker_stream_messages_total x
# TYPE broker_stream_messages_total counter
broker_stream_messages_total{transport="grpc"} 13
`
	host, port, closeSrv := newTLSServer(t, body, http.StatusOK)
	defer closeSrv()
	s := newScraperPointingAt(port)

	sample, err := s.Scrape(context.Background(), host)
	require.NoError(t, err)
	assert.Equal(t, float64(13), sample.ProcessedByTransport["grpc"])
}

func TestScrape_Non200Errors(t *testing.T) {
	host, port, closeSrv := newTLSServer(t, "", http.StatusServiceUnavailable)
	defer closeSrv()
	s := newScraperPointingAt(port)

	_, err := s.Scrape(context.Background(), host)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "503")
}

func TestScrape_DialFailureErrors(t *testing.T) {
	s := newScraperPointingAt(1) // port 1 is unbound; dial will fail fast
	_, err := s.Scrape(context.Background(), "127.0.0.1")
	require.Error(t, err)
}

func TestScrape_ContextCancellationHonored(t *testing.T) {
	// Slow server: blocks until ctx is canceled.
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	srv.TLS = &tls.Config{MinVersion: tls.VersionTLS12}
	srv.StartTLS()
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	host, p, _ := net.SplitHostPort(u.Host)
	port, _ := strconv.Atoi(p)
	s := newScraperPointingAt(port)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := s.Scrape(ctx, host)
	require.Error(t, err)
	assert.Less(t, time.Since(start), 500*time.Millisecond, "should not wait for full client timeout")
	// Just sanity-check the error mentions one of context-related strings.
	msg := err.Error()
	assert.True(t,
		strings.Contains(msg, "context") ||
			strings.Contains(msg, "deadline") ||
			strings.Contains(msg, "canceled"),
		"unexpected error: %v", err)
}

// Sanity-test the metric-name constants haven't been accidentally
// changed, since they're part of the broker contract.
func TestMetricNamesAreStable(t *testing.T) {
	assert.Equal(t, "broker_inflight_requests", MetricInflightName)
	assert.Equal(t, "broker_requests_total", MetricRequestsName)
	assert.Equal(t, "broker_stream_messages_total", MetricStreamMessagesName)
	assert.Equal(t, "transport", TransportLabelName)
}

// Ensure the introspection port we target matches the API constant.
func TestDefaultPortMatchesAPIConst(t *testing.T) {
	s := New(time.Second)
	assert.Equal(t, 8491, s.port)
	assert.Equal(t, fmt.Sprint(s.port), "8491")
}
