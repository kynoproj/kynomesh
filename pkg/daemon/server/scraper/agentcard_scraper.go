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
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"
	"github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"

	"github.com/kynoproj/kynomesh/pkg/daemon/server/rater"
)

// AgentCardScraper fetches a peer's live AgentCard from its per-AgentDeploy
// ClusterIP Service and returns its hash.
type AgentCardScraper struct {
	resolver *agentcard.Resolver
}

var _ rater.AgentCardScraper = (*AgentCardScraper)(nil)

// NewAgentCardScraper returns an AgentCardScraper.
func NewAgentCardScraper(timeout time.Duration) *AgentCardScraper {
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, //nolint:gosec
				MinVersion:         tls.VersionTLS12,
			},
			IdleConnTimeout: 30 * time.Second,
		},
		Timeout: timeout,
	}
	return &AgentCardScraper{resolver: agentcard.NewResolver(client)}
}

// ScrapeAgentCard fetches the AgentCard served at baseURL (a peer's
// per-AgentDeploy ClusterIP Service address) and returns its hash.
func (s *AgentCardScraper) ScrapeAgentCard(ctx context.Context, baseURL string) (*rater.PeerAgentCard, error) {
	card, err := s.resolver.Resolve(ctx, baseURL)
	if err != nil {
		return nil, fmt.Errorf("resolve AgentCard at %s: %w", baseURL, err)
	}
	hash, err := hashAgentCard(card)
	if err != nil {
		return nil, fmt.Errorf("hash AgentCard from %s: %w", baseURL, err)
	}
	return &rater.PeerAgentCard{Hash: hash}, nil
}

// hashAgentCard returns the hex-encoded SHA-256 digest of card's JSON
// encoding, canonicalized per JCS (RFC 8785) before hashing.
//
// This MUST stay byte-for-byte identical to kynomesh-go's
// pkg/client/hash.go (hashAgentCard) and kynomesh-py's equivalent — the
// daemon's LatestHash is only ever meaningful if it can equal a pod's
// self-reported hash of the same card, and there is no shared package
// across repos/languages to enforce that; JCS is used specifically
// because it's a language-agnostic canonical JSON form, not because it
// happens to match Go's own map-key-sorting behavior.
func hashAgentCard(card *a2a.AgentCard) (string, error) {
	data, err := json.Marshal(card)
	if err != nil {
		return "", fmt.Errorf("encode agent card: %w", err)
	}
	canonical, err := jsoncanonicalizer.Transform(data)
	if err != nil {
		return "", fmt.Errorf("canonicalize agent card: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}
