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
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newIntrospectServer brings up a TLS httptest server serving body at
// /introspect with the given status, and returns the host:port a scraper
// would target.
func newIntrospectServer(t *testing.T, body string, status int) (host string, port int, closeFn func()) {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/introspect" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
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

func newIntrospectScraperPointingAt(port int) *IntrospectScraper {
	s := NewIntrospectScraper(2 * time.Second)
	s.port = port
	return s
}

func TestScrapeIntrospect_HappyPath(t *testing.T) {
	body := `{"host":"pod-0","peerHashes":{"searcher":{"hash":"abc123","observedAt":"2026-09-09T06:34:29Z"}}}`
	host, port, closeFn := newIntrospectServer(t, body, http.StatusOK)
	defer closeFn()

	s := newIntrospectScraperPointingAt(port)
	sample, err := s.ScrapeIntrospect(context.Background(), host)
	require.NoError(t, err)
	require.Contains(t, sample.PeerHashes, "searcher")
	assert.Equal(t, "abc123", sample.PeerHashes["searcher"].Hash)
	assert.Equal(t, "2026-09-09T06:34:29Z", sample.PeerHashes["searcher"].ObservedAt)
}

func TestScrapeIntrospect_EmptyPeerHashes(t *testing.T) {
	body := `{"host":"pod-0","peerHashes":{}}`
	host, port, closeFn := newIntrospectServer(t, body, http.StatusOK)
	defer closeFn()

	s := newIntrospectScraperPointingAt(port)
	sample, err := s.ScrapeIntrospect(context.Background(), host)
	require.NoError(t, err)
	assert.Empty(t, sample.PeerHashes)
}

func TestScrapeIntrospect_Non200Errors(t *testing.T) {
	host, port, closeFn := newIntrospectServer(t, "boom", http.StatusInternalServerError)
	defer closeFn()

	s := newIntrospectScraperPointingAt(port)
	_, err := s.ScrapeIntrospect(context.Background(), host)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestScrapeIntrospect_MalformedJSON(t *testing.T) {
	host, port, closeFn := newIntrospectServer(t, "not-json", http.StatusOK)
	defer closeFn()

	s := newIntrospectScraperPointingAt(port)
	_, err := s.ScrapeIntrospect(context.Background(), host)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode")
}

func TestScrapeIntrospect_DialFailureErrors(t *testing.T) {
	s := NewIntrospectScraper(500 * time.Millisecond)
	s.port = 1 // nothing listens here
	_, err := s.ScrapeIntrospect(context.Background(), "127.0.0.1")
	require.Error(t, err)
}
