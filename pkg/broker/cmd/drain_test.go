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
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// metricsServer is a TLS httptest server that serves broker_inflight_requests
// with a value the test controls via inflight.
type metricsServer struct {
	*httptest.Server
	inflight atomic.Int64
	fail     atomic.Bool
	port     int
}

func newMetricsServer(t *testing.T) *metricsServer {
	t.Helper()
	ms := &metricsServer{}
	ms.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if ms.fail.Load() {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		fmt.Fprintf(w, "# HELP broker_inflight_requests test\n# TYPE broker_inflight_requests gauge\n")
		fmt.Fprintf(w, "broker_inflight_requests{transport=\"jsonrpc\"} %d\n", ms.inflight.Load())
		fmt.Fprintf(w, "broker_inflight_requests{transport=\"grpc\"} 0\n")
	}))
	// Extract the port so Drain can build https://localhost:<port>/metrics.
	_, portStr, err := net.SplitHostPort(strings.TrimPrefix(ms.URL, "https://"))
	require.NoError(t, err)
	ms.port, err = strconv.Atoi(portStr)
	require.NoError(t, err)
	t.Cleanup(ms.Close)
	return ms
}

func (ms *metricsServer) cfg() DrainConfig {
	return DrainConfig{
		IntrospectionPort: ms.port,
		PropagationDelay:  0,
		Budget:            2 * time.Second,
		PollInterval:      10 * time.Millisecond,
	}
}

func TestDrain_ReturnsWhenInflightZero(t *testing.T) {
	ms := newMetricsServer(t)
	ms.inflight.Store(0)

	done := Drain(context.Background(), ms.cfg())
	assert.True(t, done, "drain should succeed immediately when nothing is in flight")
}

func TestDrain_WaitsThenDrains(t *testing.T) {
	ms := newMetricsServer(t)
	ms.inflight.Store(3)
	// Drop to zero shortly after the drain starts polling.
	go func() {
		time.Sleep(50 * time.Millisecond)
		ms.inflight.Store(0)
	}()

	start := time.Now()
	done := Drain(context.Background(), ms.cfg())
	assert.True(t, done, "drain should succeed once in-flight reaches zero")
	assert.Less(t, time.Since(start), 2*time.Second, "should return well before the budget")
}

func TestDrain_TimesOutWhenNeverIdle(t *testing.T) {
	ms := newMetricsServer(t)
	ms.inflight.Store(5) // never drops

	cfg := ms.cfg()
	cfg.Budget = 150 * time.Millisecond
	done := Drain(context.Background(), cfg)
	assert.False(t, done, "drain should report not-drained when the budget elapses with work in flight")
}

func TestDrain_ScrapeFailureTreatedAsDrained(t *testing.T) {
	ms := newMetricsServer(t)
	ms.fail.Store(true) // /metrics returns 500 → broker likely already stopped serving

	done := Drain(context.Background(), ms.cfg())
	assert.True(t, done, "a failed scrape means nothing left to drain")
}

func TestScrapeInflight_SumsAcrossTransports(t *testing.T) {
	ms := newMetricsServer(t)
	ms.inflight.Store(7) // jsonrpc=7, grpc=0 → total 7

	client := insecureClient()
	total, err := scrapeInflight(context.Background(), client, fmt.Sprintf("https://localhost:%d/metrics", ms.port))
	require.NoError(t, err)
	assert.Equal(t, float64(7), total)
}

func TestSumInflight(t *testing.T) {
	tests := []struct {
		name string
		body string
		want float64
	}{
		{"absent", "# nothing here\n", 0},
		{"single", `broker_inflight_requests{transport="jsonrpc"} 4`, 4},
		{
			"multi transport",
			"# HELP x\n# TYPE broker_inflight_requests gauge\n" +
				"broker_inflight_requests{transport=\"jsonrpc\"} 3\n" +
				"broker_inflight_requests{transport=\"grpc\"} 2\n" +
				"broker_inflight_requests{transport=\"rest\"} 0\n",
			5,
		},
		{"ignores other metrics", "broker_requests_total{transport=\"grpc\"} 99\n", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := sumInflight(strings.NewReader(tc.body))
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestScrapeInflight_MetricAbsentIsZero(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "# no broker metrics here")
	}))
	t.Cleanup(srv.Close)

	client := insecureClient()
	total, err := scrapeInflight(context.Background(), client, srv.URL)
	require.NoError(t, err)
	assert.Equal(t, float64(0), total, "absent metric means nothing in flight")
}
