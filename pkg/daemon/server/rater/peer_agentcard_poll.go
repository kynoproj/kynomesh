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
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

// PeerAgentCard is one peer's decoded, hashed live AgentCard, as fetched
// directly from that peer's own AgentDeploy ClusterIP service.
type PeerAgentCard struct {
	// Hash is the JCS-canonicalized-then-sha256 hash of the peer's
	// current AgentCard.
	Hash string
}

// AgentCardScraper fetches one peer's live AgentCard and returns its
// hash. Implemented by pkg/daemon/server/scraper.AgentCardScraper.
type AgentCardScraper interface {
	ScrapeAgentCard(ctx context.Context, baseURL string) (*PeerAgentCard, error)
}

// gatedHash is one peer's stability-gated latest-hash state: a newly
// observed hash must be seen on stabilityWindow consecutive polls before
// it's promoted to accepted (LatestHash) - a peer mid-rollout can briefly
// serve an old and new card from different replicas, and that shouldn't
// itself flip LatestHash back and forth.
type gatedHash struct {
	accepted           string // promoted LatestHash; "" until first gate passes
	acceptedObservedAt time.Time

	pending      string // most recently observed candidate hash, not yet promoted
	pendingCount int    // consecutive polls pending has been observed
}

// observe folds one poll's hash into the gate.
func (g *gatedHash) observe(hash string, now time.Time, stabilityWindow int) {
	if hash == g.accepted {
		// Back to the already-accepted value - a competing candidate's
		// progress shouldn't survive a bounce through the old value.
		g.pending = ""
		g.pendingCount = 0
		return
	}
	if hash == g.pending {
		g.pendingCount++
	} else {
		g.pending = hash
		g.pendingCount = 1
	}
	if g.pendingCount >= stabilityWindow {
		g.accepted = g.pending
		g.acceptedObservedAt = now
		g.pending = ""
		g.pendingCount = 0
	}
}

// peerCardState holds one AgentDeploy's stability-gated LatestHash state
// for each of its own peers, keyed by peer name.
type peerCardState struct {
	mu     sync.Mutex
	byPeer map[string]*gatedHash
}

func newPeerCardState() *peerCardState {
	return &peerCardState{byPeer: map[string]*gatedHash{}}
}

// observe folds one successful poll of peer's AgentCard into that peer's
// gate, creating the gate on first observation.
func (s *peerCardState) observe(peer, hash string, now time.Time, stabilityWindow int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.byPeer[peer]
	if !ok {
		g = &gatedHash{}
		s.byPeer[peer] = g
	}
	g.observe(hash, now, stabilityWindow)
}

// snapshot returns peer's currently-accepted hash and when it was
// accepted. ok is false if the gate has never accepted a hash for peer
// (never successfully polled, or still waiting out the stability window).
func (s *peerCardState) snapshot(peer string) (hash string, observedAt time.Time, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, exists := s.byPeer[peer]
	if !exists || g.accepted == "" {
		return "", time.Time{}, false
	}
	return g.accepted, g.acceptedObservedAt, true
}

// scrapePeerCardsOnce polls ad's managed peers' live AgentCards and folds
// each into that peer's stability gate. A no-op if AgentCardScraper is unset.
func (r *Rater) scrapePeerCardsOnce(ctx context.Context, ad string) {
	if r.opts.AgentCardScraper == nil || r.opts.AgentSetObject == nil {
		return
	}
	state, ok := r.peerCards[ad]
	if !ok {
		return
	}
	topology := kmv1.ComputeTopology(r.opts.AgentSetObject, ad)
	log := r.opts.Logger.With(zap.String("agentDeploy", ad))
	namespace := r.opts.AgentSetObject.Namespace

	sem := make(chan struct{}, r.opts.ScrapeWorkers)
	var wg sync.WaitGroup
	for _, p := range topology.Peers {
		if p.Kind != kmv1.PeerKindManaged {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(peer string) {
			defer wg.Done()
			defer func() { <-sem }()
			baseURL := fmt.Sprintf("https://%s-%s.%s.svc:%d", r.agentSet, peer, namespace, kmv1.AgentBrokerPort)
			card, err := r.opts.AgentCardScraper.ScrapeAgentCard(ctx, baseURL)
			if err != nil {
				log.Debugw("Peer AgentCard scrape failed", zap.String("peer", peer), zap.Error(err))
				return
			}
			state.observe(peer, card.Hash, r.opts.Clock(), r.opts.StabilityWindow)
		}(p.Name)
	}
	wg.Wait()
}
