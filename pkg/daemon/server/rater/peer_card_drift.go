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
	"time"

	"go.uber.org/zap"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

// ReportedHash is a single pod's self-reported AgentCard hash for one
// peer, as scraped from that pod's broker-exposed /introspect endpoint.
type ReportedHash struct {
	// Hash is that pod's currently-reported AgentCard hash for this peer.
	Hash string
	// ObservedAt is when that pod's agent SDK recorded this hash — passed
	// through from the pod's peer-hashes file, not a scrape time. Zero
	// value if the pod's SDK didn't report a parseable timestamp.
	ObservedAt time.Time
}

// IntrospectSample is one pod's decoded broker /introspect response, the
// subset GetPeerCardDrift needs: its peer-hashes map (see
// broker.PeerHashEntry — mirrored here to avoid pkg/daemon depending on
// pkg/broker for a single struct shape).
type IntrospectSample struct {
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
	// reported a hash for this peer yet — "unknown," not "drifted."
	ReportedHashes map[string]ReportedHash
}

// GetPeerCardDrift returns the current drift-comparison state for name's
// peers: for each managed peer in name's topology, the live pods of name
// are scraped for their broker-exposed /introspect peerHashes, keyed by
// pod host.
//
// TODO(#214): LatestHash / LatestHashObservedAt are still a stub (always
// empty) until the daemon polls each peer's own live AgentCard on some
// cadence, hashes it, and holds a newly-observed hash change behind a
// stability gate before promoting it. Once that lands, this becomes a real
// comparison instead of reported-hashes-only.
//
// Returns ErrUnknownAgentDeploy if name is not in the configured list,
// matching GetMetrics.
func (r *Rater) GetPeerCardDrift(ctx context.Context, name string) (map[string]PeerCardDrift, error) {
	if _, ok := r.buffers[name]; !ok {
		return nil, ErrUnknownAgentDeploy
	}
	topology := kmv1.ComputeTopology(r.opts.AgentSetObject, name)
	out := make(map[string]PeerCardDrift, len(topology.Peers))
	for _, p := range topology.Peers {
		if p.Kind != kmv1.PeerKindManaged {
			continue
		}
		out[p.Name] = PeerCardDrift{}
	}
	if len(out) == 0 || r.opts.IntrospectScraper == nil {
		return out, nil
	}

	log := r.opts.Logger.With(zap.String("agentDeploy", name))
	hosts, err := r.opts.Discover(ctx, r.agentSet, name)
	if err != nil {
		log.Warnw("Pod discovery failed for peer card drift", zap.Error(err))
		return out, nil
	}
	for _, host := range hosts {
		sample, err := r.opts.IntrospectScraper.ScrapeIntrospect(ctx, host)
		if err != nil {
			log.Debugw("Introspect scrape failed", zap.String("host", host), zap.Error(err))
			continue
		}
		for peer, ph := range sample.PeerHashes {
			drift, ok := out[peer]
			if !ok {
				// Not a managed peer in this AgentDeploy's topology (or the
				// pod resolved a peer that isn't declared) — ignore rather
				// than surface an unexpected key.
				continue
			}
			if drift.ReportedHashes == nil {
				drift.ReportedHashes = make(map[string]ReportedHash, len(hosts))
			}
			var observedAt time.Time
			if t, err := time.Parse(time.RFC3339, ph.ObservedAt); err == nil {
				observedAt = t
			}
			drift.ReportedHashes[host] = ReportedHash{Hash: ph.Hash, ObservedAt: observedAt}
			out[peer] = drift
		}
	}
	return out, nil
}
