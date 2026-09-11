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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetPeerCardDrift_UnknownAgentDeploy(t *testing.T) {
	r := NewRater(Options{
		AgentSet:     "set",
		AgentDeploys: []string{"a"},
		Discover:     stubDiscover(map[string][]string{}),
		Scraper:      &stubScraper{samples: map[string][]*PodSample{}, idx: map[string]int{}},
	})
	_, err := r.GetPeerCardDrift("nope")
	require.ErrorIs(t, err, ErrUnknownAgentDeploy)
}

func TestGetPeerCardDrift_KnownAgentDeployStub(t *testing.T) {
	r := NewRater(Options{
		AgentSet:     "set",
		AgentDeploys: []string{"a"},
		Discover:     stubDiscover(map[string][]string{}),
		Scraper:      &stubScraper{samples: map[string][]*PodSample{}, idx: map[string]int{}},
	})
	drift, err := r.GetPeerCardDrift("a")
	require.NoError(t, err)
	assert.Empty(t, drift)
}
