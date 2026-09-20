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
	"sync"
	"time"

	"go.uber.org/zap"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

// ReportedHash is a single pod's self-reported AgentCard hash for one
// peer, as scraped from that pod's broker-exposed /introspect endpoint.
type ReportedHash struct {
	// Hash is that pod's currently-reported AgentCard hash for this peer.
	Hash string
	// ObservedAt is when that pod's agent SDK recorded this hash - passed
	// through from the pod's peer-hashes file, not a scrape time.
	ObservedAt time.Time
}

// IntrospectSample is one pod's decoded broker /introspect response.
// Mirrored here to avoid pkg/daemon depending on pkg/broker.PeerHashEntry.
type IntrospectSample struct {
	PodName string
	// PeerHashes is keyed by peer name.
	PeerHashes map[string]IntrospectPeerHash
}

// IntrospectPeerHash mirrors broker.PeerHashEntry's JSON shape.
type IntrospectPeerHash struct {
	Hash       string
	ObservedAt string // RFC3339, verbatim as the SDK wrote it; parsed on use
}

// IntrospectScraper fetches one pod's /introspect endpoint. Implemented by
// pkg/daemon/server/scraper.IntrospectScraper.
type IntrospectScraper interface {
	ScrapeIntrospect(ctx context.Context, host string) (*IntrospectSample, error)
}

// PeerCardDrift carries one peer's drift-comparison state for a single
// dependent AgentDeploy: the daemon's own polled, stability-gated
// AgentCard hash for that peer, alongside what each of the AgentDeploy's
// live pods currently report for it.
type PeerCardDrift struct {
	// LatestHash is the daemon's own polled, stability-gated AgentCard
	// hash for this peer. Zero-value ("") when the daemon has not yet
	// completed a stable poll for this peer.
	LatestHash string
	// LatestHashObservedAt is when the daemon's stability gate last
	// accepted LatestHash. Zero-value time.Time when LatestHash is empty.
	LatestHashObservedAt time.Time

	// ReportedHashes is keyed by pod name. A pod with no entry has not
	// reported a hash for this peer yet - "unknown," not "drifted."
	ReportedHashes map[string]ReportedHash
}

// peerHashCache holds the most recently scraped /introspect peerHashes for
// one AgentDeploy, keyed by pod name then peer name. Populated by the
// rater's existing scrape tick (see scrapeOneAgentDeploy), read by
// GetPeerCardDrift.
//
// A pod's entry is replaced wholesale on each successful scrape (not
// merged), so a peer the pod stops reporting (e.g. after an SDK
// downgrade, or the file being cleared) disappears from that pod's
// entry rather than lingering forever. A pod that stops being discovered
// at all (scaled down, replaced) is dropped from the cache entirely by
// prune, called once per tick with that tick's live host list.
type peerHashCache struct {
	mu        sync.RWMutex
	byPod     map[string]map[string]ReportedHash // pod name -> peer -> hash
	updated   map[string]time.Time               // pod name -> last successful scrape time
	hostToPod map[string]string                  // DNS host -> last-known pod name scraped there
}

func newPeerHashCache() *peerHashCache {
	return &peerHashCache{
		byPod:     map[string]map[string]ReportedHash{},
		updated:   map[string]time.Time{},
		hostToPod: map[string]string{},
	}
}

// set replaces pod's entire peer-hash set after a successful scrape of host.
// host is remembered so a later prune can find pod again by discovery's DNS
// name alone, without needing another scrape.
//
// If host previously resolved to a different pod name, that old name's
// entry is dropped here too.
func (c *peerHashCache) set(host, pod string, peerHashes map[string]ReportedHash, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if prev, ok := c.hostToPod[host]; ok && prev != pod {
		delete(c.byPod, prev)
		delete(c.updated, prev)
	}
	c.byPod[pod] = peerHashes
	c.updated[pod] = now
	c.hostToPod[host] = pod
}

// prune drops every cached pod whose DNS host is not in liveHosts. Called
// once per scrape tick with that tick's freshly-discovered host list, so a
// pod that scales down or is replaced doesn't leave a stale hash behind
// indefinitely.
func (c *peerHashCache) prune(liveHosts []string) {
	live := make(map[string]struct{}, len(liveHosts))
	for _, h := range liveHosts {
		live[h] = struct{}{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for host, pod := range c.hostToPod {
		if _, ok := live[host]; !ok {
			delete(c.byPod, pod)
			delete(c.updated, pod)
			delete(c.hostToPod, host)
		}
	}
}

// reportedByPeer returns every currently-cached pod's hash for peer,
// across all pods this cache has ever successfully scraped /introspect
// for.
func (c *peerHashCache) reportedByPeer(peer string) map[string]ReportedHash {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]ReportedHash, len(c.byPod))
	for pod, peers := range c.byPod {
		if rh, ok := peers[peer]; ok {
			out[pod] = rh
		}
	}
	return out
}

// scrapeIntrospectOnce scrapes ad's live pods' /introspect endpoints and
// caches each pod's peerHashes. Called from the rater's existing scrape
// tick, alongside the metrics scrape.
func (r *Rater) scrapeIntrospectOnce(ctx context.Context, ad string, hosts []string) {
	if r.opts.IntrospectScraper == nil {
		return
	}
	cache, ok := r.peerHashes[ad]
	if !ok {
		return
	}
	cache.prune(hosts)
	log := r.opts.Logger.With(zap.String("agentDeploy", ad))

	sem := make(chan struct{}, r.opts.ScrapeWorkers)
	var wg sync.WaitGroup
	for _, host := range hosts {
		wg.Add(1)
		sem <- struct{}{}
		go func(host string) {
			defer wg.Done()
			defer func() { <-sem }()
			sample, err := r.opts.IntrospectScraper.ScrapeIntrospect(ctx, host)
			if err != nil {
				log.Debugw("Introspect scrape failed", zap.String("host", host), zap.Error(err))
				return
			}
			peerHashes := make(map[string]ReportedHash, len(sample.PeerHashes))
			for peer, ph := range sample.PeerHashes {
				var observedAt time.Time
				if t, err := time.Parse(time.RFC3339, ph.ObservedAt); err == nil {
					observedAt = t
				}
				peerHashes[peer] = ReportedHash{Hash: ph.Hash, ObservedAt: observedAt}
			}
			pod := sample.PodName
			if pod == "" {
				pod = host
			}
			cache.set(host, pod, peerHashes, r.opts.Clock())
		}(host)
	}
	wg.Wait()
}

// GetPeerCardDrift returns the current drift-comparison state for name's
// peers, read from the caches scrapeIntrospectOnce and scrapePeerCardsOnce
// populate on the rater's background scrape tick - this never scrapes live
// on the calling goroutine.
//
// Returns ErrUnknownAgentDeploy if name is not in the configured list,
// matching GetMetrics.
func (r *Rater) GetPeerCardDrift(ctx context.Context, name string) (map[string]PeerCardDrift, error) {
	if _, ok := r.buffers[name]; !ok {
		return nil, ErrUnknownAgentDeploy
	}
	topology := kmv1.ComputeTopology(r.opts.AgentSetObject, name)
	out := make(map[string]PeerCardDrift, len(topology.Peers))
	cache := r.peerHashes[name]
	cards := r.peerCards[name]

	for _, p := range topology.Peers {
		if p.Kind != kmv1.PeerKindManaged {
			continue
		}
		var drift PeerCardDrift
		if cache != nil {
			drift.ReportedHashes = cache.reportedByPeer(p.Name)
		}
		if cards != nil {
			if hash, observedAt, ok := cards.snapshot(p.Name); ok {
				drift.LatestHash = hash
				drift.LatestHashObservedAt = observedAt
			}
		}
		out[p.Name] = drift
	}
	return out, nil
}
