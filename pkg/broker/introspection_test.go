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
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntrospectionHandler_Healthz(t *testing.T) {
	h := NewIntrospectionHandler(context.TODO(), prometheus.NewRegistry(), func() error { return nil })
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ok", rec.Body.String())
}

func TestIntrospectionHandler_Readyz(t *testing.T) {
	t.Run("ready returns nil → 200", func(t *testing.T) {
		h := NewIntrospectionHandler(context.TODO(), prometheus.NewRegistry(), func() error { return nil })
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/readyz", nil))
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "ready", rec.Body.String())
	})

	t.Run("ready returns error → 503", func(t *testing.T) {
		h := NewIntrospectionHandler(context.TODO(), prometheus.NewRegistry(), func() error { return errors.New("agent unreachable") })
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/readyz", nil))
		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
		assert.Contains(t, rec.Body.String(), "agent unreachable")
	})
}

func TestIntrospectionHandler_MetricsExposesCounters(t *testing.T) {
	registry := prometheus.NewRegistry()
	counters := NewMetrics(registry)
	counters.JSONRPC().Set(3)
	counters.REST().Set(7)
	counters.GRPC().Set(2)
	counters.Passthrough().Set(11)

	h := NewIntrospectionHandler(context.TODO(), registry, func() error { return nil })
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	body, err := io.ReadAll(rec.Body)
	require.NoError(t, err)
	s := string(body)

	// Help + type lines emit the metric name with the standard help text.
	assert.Contains(t, s, "# HELP broker_inflight_requests")
	assert.Contains(t, s, "# TYPE broker_inflight_requests gauge")

	// One line per transport label with the current gauge value.
	assert.Contains(t, s, `broker_inflight_requests{transport="jsonrpc"} 3`)
	assert.Contains(t, s, `broker_inflight_requests{transport="rest"} 7`)
	assert.Contains(t, s, `broker_inflight_requests{transport="grpc"} 2`)
	assert.Contains(t, s, `broker_inflight_requests{transport="passthrough"} 11`)
}

func TestIntrospectionHandler_Introspect(t *testing.T) {
	t.Run("missing peer-hashes file reports empty map", func(t *testing.T) {
		orig := peerHashesFilePath
		peerHashesFilePath = filepath.Join(t.TempDir(), "does-not-exist.json")
		t.Cleanup(func() { peerHashesFilePath = orig })
		t.Setenv("POD_NAME", "my-agent-0")

		h := NewIntrospectionHandler(context.TODO(), prometheus.NewRegistry(), func() error { return nil })
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/introspect", nil))
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.JSONEq(t, `{"host":"my-agent-0","peerHashes":{}}`, rec.Body.String())
	})

	t.Run("existing peer-hashes file surfaced under peerHashes", func(t *testing.T) {
		orig := peerHashesFilePath
		path := filepath.Join(t.TempDir(), "peer-hashes.json")
		require.NoError(t, os.WriteFile(path, []byte(`{"worker":"abc123"}`), 0o600))
		peerHashesFilePath = path
		t.Cleanup(func() { peerHashesFilePath = orig })
		t.Setenv("POD_NAME", "my-agent-0")

		h := NewIntrospectionHandler(context.TODO(), prometheus.NewRegistry(), func() error { return nil })
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/introspect", nil))
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.JSONEq(t, `{"host":"my-agent-0","peerHashes":{"worker":"abc123"}}`, rec.Body.String())
	})

	t.Run("malformed peer-hashes file returns 500", func(t *testing.T) {
		orig := peerHashesFilePath
		path := filepath.Join(t.TempDir(), "peer-hashes.json")
		require.NoError(t, os.WriteFile(path, []byte(`not-json`), 0o600))
		peerHashesFilePath = path
		t.Cleanup(func() { peerHashesFilePath = orig })

		h := NewIntrospectionHandler(context.TODO(), prometheus.NewRegistry(), func() error { return nil })
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/introspect", nil))
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

func TestIntrospectionHandler_UnknownPath404(t *testing.T) {
	h := NewIntrospectionHandler(context.TODO(), prometheus.NewRegistry(), func() error { return nil })
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/random", nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}
