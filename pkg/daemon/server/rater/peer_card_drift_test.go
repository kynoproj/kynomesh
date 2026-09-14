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

// mutableDiscover returns whatever host list was most recently set via
// setHosts, letting a test change discovery results between successive
// scrapeAllOnce calls (e.g. to simulate a pod scaling down).
type mutableDiscover struct {
	mu    sync.Mutex
	hosts map[string][]string
}

func (d *mutableDiscover) setHosts(ad string, hosts []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.hosts[ad] = hosts
}

func (d *mutableDiscover) discover(_ context.Context, _, ad string) ([]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.hosts[ad], nil
}

// testAgentSetObject builds a minimal *kmv1.AgentSet with the given managed
// agent names, suitable for Options.AgentSetObject in tests.
func testAgentSetObject(name string, pattern kmv1.AgentPattern, entry string, agentNames ...string) *kmv1.AgentSet {
	as := &kmv1.AgentSet{Spec: kmv1.AgentSetSpec{Pattern: pattern, Entry: entry}}
	as.Name = name
	for _, n := range agentNames {
		as.Spec.Agents = append(as.Spec.Agents, kmv1.AbstractAgentDeploy{Name: n})
	}
	return as
}

func TestGetPeerCardDrift_UnknownAgentDeploy(t *testing.T) {
	r := NewRater(Options{
		AgentSetObject: testAgentSetObject("set", kmv1.AgentPatternSupervisor, "a", "a"),
		Discover:       stubDiscover(map[string][]string{}),
		MetricsScraper: &stubScraper{samples: map[string][]*PodSample{}, idx: map[string]int{}},
	})
	_, err := r.GetPeerCardDrift(context.Background(), "nope")
	require.ErrorIs(t, err, ErrUnknownAgentDeploy)
}

func TestGetPeerCardDrift_KnownAgentDeployNoPeers(t *testing.T) {
	r := NewRater(Options{
		AgentSetObject: testAgentSetObject("set", kmv1.AgentPatternSupervisor, "a", "a"),
		Discover:       stubDiscover(map[string][]string{}),
		MetricsScraper: &stubScraper{samples: map[string][]*PodSample{}, idx: map[string]int{}},
	})
	drift, err := r.GetPeerCardDrift(context.Background(), "a")
	require.NoError(t, err)
	assert.Empty(t, drift)
}

func TestGetPeerCardDrift_ManagedPeersEnumerated(t *testing.T) {
	r := NewRater(Options{
		AgentSetObject: testAgentSetObject("set", kmv1.AgentPatternSupervisor, "a", "a", "b", "c"),
		Discover:       stubDiscover(map[string][]string{}),
		MetricsScraper: &stubScraper{samples: map[string][]*PodSample{}, idx: map[string]int{}},
	})

	drift, err := r.GetPeerCardDrift(context.Background(), "a")
	require.NoError(t, err)
	assert.Contains(t, drift, "b")
	assert.Contains(t, drift, "c")

	// Non-entry agent has no peers under Supervisor.
	drift, err = r.GetPeerCardDrift(context.Background(), "b")
	require.NoError(t, err)
	assert.Empty(t, drift)
}

// stubIntrospectScraper returns canned IntrospectSamples per host.
type stubIntrospectScraper struct {
	samples map[string]*IntrospectSample
	err     error
}

func (s *stubIntrospectScraper) ScrapeIntrospect(_ context.Context, host string) (*IntrospectSample, error) {
	if s.err != nil {
		return nil, s.err
	}
	sample, ok := s.samples[host]
	if !ok {
		return nil, errors.New("no sample for host")
	}
	return sample, nil
}

func TestGetPeerCardDrift_ReportedHashesKeyedByPodNameNotDNSHost(t *testing.T) {
	r := NewRater(Options{
		AgentSetObject: testAgentSetObject("set", kmv1.AgentPatternSupervisor, "a", "a", "b"),
		Discover:       stubDiscover(map[string][]string{"a": {"a-0.a-headless.ns.svc"}}),
		MetricsScraper: &stubScraper{samples: map[string][]*PodSample{}, idx: map[string]int{}},
		IntrospectScraper: &stubIntrospectScraper{samples: map[string]*IntrospectSample{
			"a-0.a-headless.ns.svc": {
				PodName: "a-0",
				PeerHashes: map[string]IntrospectPeerHash{
					"b": {Hash: "hash-old"},
				},
			},
		}},
	})

	r.scrapeAllOnce(context.Background())
	drift, err := r.GetPeerCardDrift(context.Background(), "a")
	require.NoError(t, err)
	reported := drift["b"].ReportedHashes
	require.Contains(t, reported, "a-0", "must be keyed by the pod's real name, not the DNS host used to scrape it")
	assert.NotContains(t, reported, "a-0.a-headless.ns.svc")
}

func TestGetPeerCardDrift_PodRecreatedAtSameHostDropsOldPodName(t *testing.T) {
	scraper := &stubIntrospectScraper{samples: map[string]*IntrospectSample{
		"a-0.a-headless.ns.svc": {
			PodName:    "a-0-old",
			PeerHashes: map[string]IntrospectPeerHash{"b": {Hash: "hash-old"}},
		},
	}}
	r := NewRater(Options{
		AgentSetObject:    testAgentSetObject("set", kmv1.AgentPatternSupervisor, "a", "a", "b"),
		Discover:          stubDiscover(map[string][]string{"a": {"a-0.a-headless.ns.svc"}}),
		MetricsScraper:    &stubScraper{samples: map[string][]*PodSample{}, idx: map[string]int{}},
		IntrospectScraper: scraper,
	})

	r.scrapeAllOnce(context.Background())
	drift, err := r.GetPeerCardDrift(context.Background(), "a")
	require.NoError(t, err)
	require.Contains(t, drift["b"].ReportedHashes, "a-0-old")

	// Same DNS host (same ordinal), but the pod behind it was recreated with
	// a new random-suffixed name — Discover still reports the host as live.
	scraper.samples["a-0.a-headless.ns.svc"] = &IntrospectSample{
		PodName:    "a-0-new",
		PeerHashes: map[string]IntrospectPeerHash{"b": {Hash: "hash-new"}},
	}

	r.scrapeAllOnce(context.Background())
	drift, err = r.GetPeerCardDrift(context.Background(), "a")
	require.NoError(t, err)
	reported := drift["b"].ReportedHashes
	require.Contains(t, reported, "a-0-new")
	assert.NotContains(t, reported, "a-0-old", "a pod recreated at the same DNS host must not leave its old name cached forever")
}

func TestGetPeerCardDrift_ReportedHashesFromIntrospectScrape(t *testing.T) {
	r := NewRater(Options{
		AgentSetObject: testAgentSetObject("set", kmv1.AgentPatternSupervisor, "a", "a", "b"),
		Discover:       stubDiscover(map[string][]string{"a": {"a-0", "a-1"}}),
		MetricsScraper: &stubScraper{samples: map[string][]*PodSample{}, idx: map[string]int{}},
		IntrospectScraper: &stubIntrospectScraper{samples: map[string]*IntrospectSample{
			"a-0": {PeerHashes: map[string]IntrospectPeerHash{
				"b": {Hash: "hash-old", ObservedAt: "2026-09-07T09:02:11Z"},
			}},
			"a-1": {PeerHashes: map[string]IntrospectPeerHash{
				"b": {Hash: "hash-new", ObservedAt: "2026-09-09T06:34:29Z"},
			}},
		}},
	})

	r.scrapeAllOnce(context.Background())
	drift, err := r.GetPeerCardDrift(context.Background(), "a")
	require.NoError(t, err)
	require.Contains(t, drift, "b")
	reported := drift["b"].ReportedHashes
	require.Contains(t, reported, "a-0")
	require.Contains(t, reported, "a-1")
	assert.Equal(t, "hash-old", reported["a-0"].Hash)
	assert.Equal(t, "hash-new", reported["a-1"].Hash)
	assert.Equal(t, 2026, reported["a-1"].ObservedAt.Year())
}

func TestGetPeerCardDrift_IntrospectScrapeFailureLeavesPodUnreported(t *testing.T) {
	r := NewRater(Options{
		AgentSetObject:    testAgentSetObject("set", kmv1.AgentPatternSupervisor, "a", "a", "b"),
		Discover:          stubDiscover(map[string][]string{"a": {"a-0"}}),
		MetricsScraper:    &stubScraper{samples: map[string][]*PodSample{}, idx: map[string]int{}},
		IntrospectScraper: &stubIntrospectScraper{err: errors.New("scrape failed")},
	})

	r.scrapeAllOnce(context.Background())
	drift, err := r.GetPeerCardDrift(context.Background(), "a")
	require.NoError(t, err)
	require.Contains(t, drift, "b")
	assert.Empty(t, drift["b"].ReportedHashes, "a scrape failure must not fabricate a reported hash")
}

func TestGetPeerCardDrift_UnknownPeerFromPodIgnored(t *testing.T) {
	r := NewRater(Options{
		AgentSetObject: testAgentSetObject("set", kmv1.AgentPatternSupervisor, "a", "a", "b"),
		Discover:       stubDiscover(map[string][]string{"a": {"a-0"}}),
		MetricsScraper: &stubScraper{samples: map[string][]*PodSample{}, idx: map[string]int{}},
		IntrospectScraper: &stubIntrospectScraper{samples: map[string]*IntrospectSample{
			"a-0": {PeerHashes: map[string]IntrospectPeerHash{
				"not-a-declared-peer": {Hash: "x"},
			}},
		}},
	})

	r.scrapeAllOnce(context.Background())
	drift, err := r.GetPeerCardDrift(context.Background(), "a")
	require.NoError(t, err)
	require.Contains(t, drift, "b")
	assert.Empty(t, drift["b"].ReportedHashes)
	assert.NotContains(t, drift, "not-a-declared-peer")
}

func TestGetPeerCardDrift_PrunesPodsNoLongerDiscovered(t *testing.T) {
	discover := &mutableDiscover{hosts: map[string][]string{"a": {"a-0", "a-1"}}}
	r := NewRater(Options{
		AgentSetObject: testAgentSetObject("set", kmv1.AgentPatternSupervisor, "a", "a", "b"),
		Discover:       discover.discover,
		MetricsScraper: &stubScraper{samples: map[string][]*PodSample{}, idx: map[string]int{}},
		IntrospectScraper: &stubIntrospectScraper{samples: map[string]*IntrospectSample{
			"a-0": {PeerHashes: map[string]IntrospectPeerHash{"b": {Hash: "hash-0"}}},
			"a-1": {PeerHashes: map[string]IntrospectPeerHash{"b": {Hash: "hash-1"}}},
		}},
	})

	r.scrapeAllOnce(context.Background())
	drift, err := r.GetPeerCardDrift(context.Background(), "a")
	require.NoError(t, err)
	reported := drift["b"].ReportedHashes
	require.Contains(t, reported, "a-0")
	require.Contains(t, reported, "a-1")

	// a-1 scales down: subsequent discovery only returns a-0.
	discover.setHosts("a", []string{"a-0"})
	r.scrapeAllOnce(context.Background())
	drift, err = r.GetPeerCardDrift(context.Background(), "a")
	require.NoError(t, err)
	reported = drift["b"].ReportedHashes
	require.Contains(t, reported, "a-0")
	assert.NotContains(t, reported, "a-1", "a pod no longer discovered must be pruned from the cache")
}
