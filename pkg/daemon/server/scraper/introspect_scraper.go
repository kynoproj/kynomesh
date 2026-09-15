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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
	"github.com/kynoproj/kynomesh/pkg/daemon/server/rater"
)

// introspectResponse mirrors the broker's /introspect JSON response shape
// (pkg/broker/introspection.go's introspectResponse / PeerHashEntry).
// Duplicated rather than imported: pkg/daemon must not depend on pkg/broker
// for a single response-decoding struct.
type introspectResponse struct {
	Host       string                        `json:"host"`
	PeerHashes map[string]introspectPeerHash `json:"peerHashes"`
}

type introspectPeerHash struct {
	Hash       string `json:"hash"`
	ObservedAt string `json:"observedAt"`
}

// IntrospectScraper fetches one pod's broker-exposed /introspect endpoint
// and decodes its peerHashes.
type IntrospectScraper struct {
	client *http.Client
	port   int
}

var _ rater.IntrospectScraper = (*IntrospectScraper)(nil)

// NewIntrospectScraper returns an IntrospectScraper.
func NewIntrospectScraper(timeout time.Duration) *IntrospectScraper {
	return &IntrospectScraper{
		client: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true, //nolint:gosec
					MinVersion:         tls.VersionTLS12,
				},
				IdleConnTimeout: 30 * time.Second,
			},
			Timeout: timeout,
		},
		port: kmv1.AgentBrokerIntrospectionPort,
	}
}

// ScrapeIntrospect fetches https://<host>:<port>/introspect and returns its
// peer-hashes, decoded into rater.IntrospectSample.
func (s *IntrospectScraper) ScrapeIntrospect(ctx context.Context, host string) (*rater.IntrospectSample, error) {
	url := fmt.Sprintf("https://%s:%d/introspect", host, s.port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("scrape %s: %w", host, err)
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("scrape %s: status %d", host, resp.StatusCode)
	}

	var decoded introspectResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode introspect response from %s: %w", host, err)
	}

	peerHashes := make(map[string]rater.IntrospectPeerHash, len(decoded.PeerHashes))
	for peer, ph := range decoded.PeerHashes {
		peerHashes[peer] = rater.IntrospectPeerHash{Hash: ph.Hash, ObservedAt: ph.ObservedAt}
	}
	return &rater.IntrospectSample{PodName: decoded.Host, PeerHashes: peerHashes}, nil
}
