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
	"sync"
	"time"

	sharedqueue "github.com/kynoproj/kynomesh/pkg/shared/queue"
)

// Retention is the total horizon of samples kept per pod. Chosen to
// cover the 15m fixed lookback window with headroom for slow first
// requests after a daemon (re)start.
const Retention = 30 * time.Minute

// MaxSamplesPerPod caps each pod's ring buffer. Sized so that a 5s
// scrape interval over Retention fits without dropping samples; the
// extra headroom absorbs catch-up scrapes after transient unreach-
// ability without spilling.
const MaxSamplesPerPod = int(Retention/(5*time.Second)) * 2

// PodSample is one observation from one pod, taken at scrape-
// completion time. Timestamps are raw unix seconds (no bucket
// alignment) — slow scrapes, future per-pod schedules, and broker-
// pushed samples all just record the wall clock at which they
// arrived.
//
// ProcessedByTransport is the per-transport cumulative count of
// messages the broker has processed since pod start. Monotonic
// across consecutive observations of the same pod; processing rate
// comes from the delta between in-window samples, with counter-
// reset handling for pod/broker restarts.
//
// InflightByTransport is the per-transport count of requests
// currently in flight at scrape time. Instantaneous; averaged
// across a window using time-weighting so uneven sample spacing
// doesn't bias the result.
type PodSample struct {
	Timestamp            int64
	ProcessedByTransport map[string]float64
	InflightByTransport  map[string]float64
}

// AgentDeployBuffers owns one ring buffer per pod for a single
// AgentDeploy. Buffers are created lazily on first sample.
//
// Pods that disappear (scaled-down, terminated) keep their buffer:
// their samples naturally age out of every query window without any
// cleanup code.
type AgentDeployBuffers struct {
	mu    sync.RWMutex
	byPod map[string]*sharedqueue.OverflowQueue[*PodSample]
}

func NewAgentDeployBuffers() *AgentDeployBuffers {
	return &AgentDeployBuffers{
		byPod: map[string]*sharedqueue.OverflowQueue[*PodSample]{},
	}
}

// Append records a sample for the named pod. A nil sample is
// ignored: a failed scrape must not corrupt the previous successful
// observation, which would otherwise produce a giant counter delta
// against the next successful sample.
func (b *AgentDeployBuffers) Append(pod string, s *PodSample) {
	if s == nil {
		return
	}
	b.mu.Lock()
	q, ok := b.byPod[pod]
	if !ok {
		q = sharedqueue.New[*PodSample](MaxSamplesPerPod)
		b.byPod[pod] = q
	}
	b.mu.Unlock()
	q.Append(s)
}

// Pods returns the list of pod names currently tracked. Order is
// unspecified; callers that need stable ordering must sort the
// result.
func (b *AgentDeployBuffers) Pods() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]string, 0, len(b.byPod))
	for p := range b.byPod {
		out = append(out, p)
	}
	return out
}

// Samples returns the named pod's samples in append order. Returns
// nil if the pod has never been observed.
func (b *AgentDeployBuffers) Samples(pod string) []*PodSample {
	b.mu.RLock()
	q, ok := b.byPod[pod]
	b.mu.RUnlock()
	if !ok {
		return nil
	}
	return q.Items()
}
