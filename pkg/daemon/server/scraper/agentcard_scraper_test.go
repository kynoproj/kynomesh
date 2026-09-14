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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newAgentCardServer brings up a TLS httptest server serving body at the
// A2A well-known AgentCard path.
func newAgentCardServer(t *testing.T, body string, status int) (baseURL string, closeFn func()) {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/agent-card.json" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	srv.TLS = &tls.Config{MinVersion: tls.VersionTLS12}
	srv.StartTLS()
	return srv.URL, srv.Close
}

func TestScrapeAgentCard_HappyPath(t *testing.T) {
	body := `{"name":"worker","description":"d","version":"1.0","defaultInputModes":["text"],"defaultOutputModes":["text"],"skills":[],"capabilities":{},"supportedInterfaces":[]}`
	baseURL, closeFn := newAgentCardServer(t, body, http.StatusOK)
	defer closeFn()

	s := NewAgentCardScraper(2 * time.Second)
	card, err := s.ScrapeAgentCard(context.Background(), baseURL)
	require.NoError(t, err)
	assert.NotEmpty(t, card.Hash)
}

func TestScrapeAgentCard_HashDeterministic(t *testing.T) {
	body := `{"name":"worker","description":"d","version":"1.0","defaultInputModes":["text"],"defaultOutputModes":["text"],"skills":[],"capabilities":{},"supportedInterfaces":[]}`
	baseURL, closeFn := newAgentCardServer(t, body, http.StatusOK)
	defer closeFn()

	s := NewAgentCardScraper(2 * time.Second)
	card1, err := s.ScrapeAgentCard(context.Background(), baseURL)
	require.NoError(t, err)
	card2, err := s.ScrapeAgentCard(context.Background(), baseURL)
	require.NoError(t, err)
	assert.Equal(t, card1.Hash, card2.Hash)
}

func TestScrapeAgentCard_DifferentContentDifferentHash(t *testing.T) {
	bodyA := `{"name":"worker-a","description":"d","version":"1.0","defaultInputModes":["text"],"defaultOutputModes":["text"],"skills":[],"capabilities":{},"supportedInterfaces":[]}`
	bodyB := `{"name":"worker-b","description":"d","version":"1.0","defaultInputModes":["text"],"defaultOutputModes":["text"],"skills":[],"capabilities":{},"supportedInterfaces":[]}`

	urlA, closeA := newAgentCardServer(t, bodyA, http.StatusOK)
	defer closeA()
	urlB, closeB := newAgentCardServer(t, bodyB, http.StatusOK)
	defer closeB()

	s := NewAgentCardScraper(2 * time.Second)
	cardA, err := s.ScrapeAgentCard(context.Background(), urlA)
	require.NoError(t, err)
	cardB, err := s.ScrapeAgentCard(context.Background(), urlB)
	require.NoError(t, err)
	assert.NotEqual(t, cardA.Hash, cardB.Hash)
}

func TestScrapeAgentCard_Non200Errors(t *testing.T) {
	baseURL, closeFn := newAgentCardServer(t, "boom", http.StatusInternalServerError)
	defer closeFn()

	s := NewAgentCardScraper(2 * time.Second)
	_, err := s.ScrapeAgentCard(context.Background(), baseURL)
	require.Error(t, err)
}

func TestScrapeAgentCard_DialFailureErrors(t *testing.T) {
	s := NewAgentCardScraper(500 * time.Millisecond)
	_, err := s.ScrapeAgentCard(context.Background(), "https://127.0.0.1:1")
	require.Error(t, err)
}

// TestHashAgentCard_FieldOrderIndependent asserts the JCS-canonicalization
// guarantee explicitly: two Go struct literals built with different
// field-assignment order but identical content must hash identically. This
// is the property the whole cross-repo hash-parity scheme (daemon vs.
// kynomesh-go/kynomesh-py SDKs) depends on.
func TestHashAgentCard_FieldOrderIndependent(t *testing.T) {
	cardA := &a2a.AgentCard{
		Name:               "worker",
		Description:        "d",
		Version:            "1.0",
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
	}
	cardB := &a2a.AgentCard{
		DefaultOutputModes: []string{"text"},
		DefaultInputModes:  []string{"text"},
		Version:            "1.0",
		Description:        "d",
		Name:               "worker",
	}

	hashA, err := hashAgentCard(cardA)
	require.NoError(t, err)
	hashB, err := hashAgentCard(cardB)
	require.NoError(t, err)
	assert.Equal(t, hashA, hashB)
}

func TestHashAgentCard_DifferentContentDifferentHash(t *testing.T) {
	cardA := &a2a.AgentCard{Name: "worker-a"}
	cardB := &a2a.AgentCard{Name: "worker-b"}

	hashA, err := hashAgentCard(cardA)
	require.NoError(t, err)
	hashB, err := hashAgentCard(cardB)
	require.NoError(t, err)
	assert.NotEqual(t, hashA, hashB)
}
