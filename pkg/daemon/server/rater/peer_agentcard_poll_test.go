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

package rater

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

// stubAgentCardScraper returns canned hashes per peer, driven off the
// baseURL passed in (tests key by peer name via a lookup func, since the
// exact ClusterIP DNS string isn't the point under test).
type stubAgentCardScraper struct {
	mu     sync.Mutex
	hashes map[string]string // baseURL -> hash
	err    error
	calls  []string
}

func (s *stubAgentCardScraper) ScrapeAgentCard(_ context.Context, baseURL string) (*PeerAgentCard, error) {
	s.mu.Lock()
	s.calls = append(s.calls, baseURL)
	s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	hash, ok := s.hashes[baseURL]
	if !ok {
		return nil, errors.New("no hash for baseURL")
	}
	return &PeerAgentCard{Hash: hash}, nil
}

func (s *stubAgentCardScraper) setHash(baseURL, hash string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hashes == nil {
		s.hashes = map[string]string{}
	}
	s.hashes[baseURL] = hash
}

// peerBaseURL mirrors scrapePeerCardsOnce's URL construction so tests can
// key stubAgentCardScraper without hardcoding the DNS format twice.
func peerBaseURL(agentSet, peer, namespace string) string {
	return "https://" + agentSet + "-" + peer + "." + namespace + ".svc:8490"
}

func TestGetPeerCardDrift_StabilityGate_HoldsUntilThreshold(t *testing.T) {
	scr := &stubAgentCardScraper{}
	url := peerBaseURL("set", "b", "")
	scr.setHash(url, "hash-1")

	r := NewRater(Options{
		AgentSetObject:   testAgentSetObject("set", kmv1.AgentPatternSupervisor, "a", "a", "b"),
		Discover:         stubDiscover(map[string][]string{"a": {}}),
		MetricsScraper:   &stubScraper{samples: map[string][]*PodSample{}, idx: map[string]int{}},
		AgentCardScraper: scr,
		StabilityWindow:  3,
	})

	// Polls 1 and 2: not yet promoted.
	r.scrapeAllOnce(context.Background())
	drift, err := r.GetPeerCardDrift(context.Background(), "a")
	require.NoError(t, err)
	assert.Empty(t, drift["b"].LatestHash, "must not promote before stabilityWindow consecutive polls")

	r.scrapeAllOnce(context.Background())
	drift, err = r.GetPeerCardDrift(context.Background(), "a")
	require.NoError(t, err)
	assert.Empty(t, drift["b"].LatestHash)

	// Poll 3: promoted.
	r.scrapeAllOnce(context.Background())
	drift, err = r.GetPeerCardDrift(context.Background(), "a")
	require.NoError(t, err)
	assert.Equal(t, "hash-1", drift["b"].LatestHash)
	assert.False(t, drift["b"].LatestHashObservedAt.IsZero())
}

func TestGetPeerCardDrift_StabilityGate_FlipFlopNeverPromotes(t *testing.T) {
	scr := &stubAgentCardScraper{}
	url := peerBaseURL("set", "b", "")
	scr.setHash(url, "hash-A")

	r := NewRater(Options{
		AgentSetObject:   testAgentSetObject("set", kmv1.AgentPatternSupervisor, "a", "a", "b"),
		Discover:         stubDiscover(map[string][]string{"a": {}}),
		MetricsScraper:   &stubScraper{samples: map[string][]*PodSample{}, idx: map[string]int{}},
		AgentCardScraper: scr,
		StabilityWindow:  2,
	})

	// Promote hash-A first (2 consecutive polls).
	r.scrapeAllOnce(context.Background())
	r.scrapeAllOnce(context.Background())
	drift, err := r.GetPeerCardDrift(context.Background(), "a")
	require.NoError(t, err)
	require.Equal(t, "hash-A", drift["b"].LatestHash)

	// Bounce: B, A, B — B never accumulates 2 consecutive observations.
	scr.setHash(url, "hash-B")
	r.scrapeAllOnce(context.Background())
	scr.setHash(url, "hash-A")
	r.scrapeAllOnce(context.Background())
	scr.setHash(url, "hash-B")
	r.scrapeAllOnce(context.Background())

	drift, err = r.GetPeerCardDrift(context.Background(), "a")
	require.NoError(t, err)
	assert.Equal(t, "hash-A", drift["b"].LatestHash, "flip-flopping candidate must not promote")
}

func TestGetPeerCardDrift_StabilityGate_ScrapeFailureDoesNotResetProgress(t *testing.T) {
	scr := &stubAgentCardScraper{}
	url := peerBaseURL("set", "b", "")
	scr.setHash(url, "hash-1")

	r := NewRater(Options{
		AgentSetObject:   testAgentSetObject("set", kmv1.AgentPatternSupervisor, "a", "a", "b"),
		Discover:         stubDiscover(map[string][]string{"a": {}}),
		MetricsScraper:   &stubScraper{samples: map[string][]*PodSample{}, idx: map[string]int{}},
		AgentCardScraper: scr,
		StabilityWindow:  3,
	})

	// Two successful polls of hash-1 (pendingCount=2), then a failure,
	// then one more success of hash-1 should promote — a failed poll must
	// not restart the count from zero.
	r.scrapeAllOnce(context.Background())
	r.scrapeAllOnce(context.Background())

	scr.mu.Lock()
	scr.err = errors.New("scrape failed")
	scr.mu.Unlock()
	r.scrapeAllOnce(context.Background())

	scr.mu.Lock()
	scr.err = nil
	scr.mu.Unlock()
	r.scrapeAllOnce(context.Background())

	drift, err := r.GetPeerCardDrift(context.Background(), "a")
	require.NoError(t, err)
	assert.Equal(t, "hash-1", drift["b"].LatestHash, "a scrape failure must not reset stability-gate progress")
}

func TestGetPeerCardDrift_ExternalPeersSkippedForAgentCardPolling(t *testing.T) {
	scr := &stubAgentCardScraper{}
	as := testAgentSetObject("set", kmv1.AgentPatternHandoff, "", "a")
	as.Spec.ExternalAgents = []kmv1.ExternalAgentRef{{Name: "ext", URL: "https://external.example.com"}}

	r := NewRater(Options{
		AgentSetObject:   as,
		Discover:         stubDiscover(map[string][]string{"a": {}}),
		MetricsScraper:   &stubScraper{samples: map[string][]*PodSample{}, idx: map[string]int{}},
		AgentCardScraper: scr,
		StabilityWindow:  1,
	})

	r.scrapeAllOnce(context.Background())
	drift, err := r.GetPeerCardDrift(context.Background(), "a")
	require.NoError(t, err)
	if _, ok := drift["ext"]; ok {
		t.Fatalf("external peer must not appear in managed-only PeerCardDrift output")
	}

	scr.mu.Lock()
	defer scr.mu.Unlock()
	for _, call := range scr.calls {
		assert.NotContains(t, call, "ext", "AgentCardScraper must never be called for an external peer")
	}
}

func TestGetPeerCardDrift_AgentCardScraperNil_LatestHashStaysEmpty(t *testing.T) {
	r := NewRater(Options{
		AgentSetObject: testAgentSetObject("set", kmv1.AgentPatternSupervisor, "a", "a", "b"),
		Discover:       stubDiscover(map[string][]string{"a": {}}),
		MetricsScraper: &stubScraper{samples: map[string][]*PodSample{}, idx: map[string]int{}},
	})

	r.scrapeAllOnce(context.Background())
	drift, err := r.GetPeerCardDrift(context.Background(), "a")
	require.NoError(t, err)
	require.Contains(t, drift, "b")
	assert.Empty(t, drift["b"].LatestHash)
}

func TestScrapePeerCardsOnce_RunsEvenWhenScaledToZero(t *testing.T) {
	scr := &stubAgentCardScraper{}
	url := peerBaseURL("set", "b", "")
	scr.setHash(url, "hash-1")

	r := NewRater(Options{
		AgentSetObject:   testAgentSetObject("set", kmv1.AgentPatternSupervisor, "a", "a", "b"),
		Discover:         stubDiscover(map[string][]string{"a": nil}), // no ready pods for "a" itself
		MetricsScraper:   &stubScraper{samples: map[string][]*PodSample{}, idx: map[string]int{}},
		AgentCardScraper: scr,
		StabilityWindow:  1,
	})

	r.scrapeAllOnce(context.Background())
	drift, err := r.GetPeerCardDrift(context.Background(), "a")
	require.NoError(t, err)
	assert.Equal(t, "hash-1", drift["b"].LatestHash, "peer AgentCard polling must not depend on ad's own discovery")
}
