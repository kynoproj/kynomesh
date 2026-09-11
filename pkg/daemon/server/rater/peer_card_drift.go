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

import "time"

// ReportedHash is a single pod's self-reported AgentCard hash for one
// peer, as scraped from that pod's broker-exposed /introspect endpoint.
type ReportedHash struct {
	// Hash is that pod's currently-reported AgentCard hash for this peer.
	Hash string
	// ObservedAt is when that pod's agent SDK recorded this hash — passed
	// through from the pod's peer-hashes file, not a scrape time.
	ObservedAt time.Time
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
// peers.
//
// TODO(#214): this is a stub. It always returns an empty map (no known
// peers, no drift data) for any known AgentDeploy. The real
// implementation needs to:
//  1. Poll each managed peer's live AgentCard on some cadence, hash it,
//     and hold a newly-observed hash change behind a stability gate
//     before treating it as LatestHash.
//  2. Scrape each of name's live pods' broker-exposed /introspect
//     endpoint for their peerHashes, and surface each pod's per-peer
//     {hash, observedAt} as ReportedHashes.
//
// Returns ErrUnknownAgentDeploy if name is not in the configured list,
// matching GetMetrics.
func (r *Rater) GetPeerCardDrift(name string) (map[string]PeerCardDrift, error) {
	if _, ok := r.buffers[name]; !ok {
		return nil, ErrUnknownAgentDeploy
	}
	return map[string]PeerCardDrift{}, nil
}
