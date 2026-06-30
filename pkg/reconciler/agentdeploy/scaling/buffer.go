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

package scaling

import (
	"sync"
	"time"
)

const (
	// defaultMaxAge bounds how far back history is kept. Recency decay in the
	// estimator already makes data older than ~a day near-weightless, so this
	// is mostly a coverage horizon, not a hard requirement.
	defaultMaxAge = 7 * 24 * time.Hour
	// defaultMaxRecords is the byte backstop: the count cap that keeps the
	// encoded blob well under the ConfigMap 1MB limit regardless of cadence.
	defaultMaxRecords = 12000
)

// record is one persisted observation: a Sample plus bookkeeping. count is the
// number of raw samples folded into this record — always 1 today; reserved for
// when tier rollup lands. generation is the AgentDeploy generation at capture,
// for deploy-reset.
type record struct {
	sample     Sample
	count      uint16
	generation int64
}

// buffer is an in-memory, time-ordered ring of records bounded by both age and
// record count. Safe for concurrent use: the collector appends while the
// reconcile loop reads and flushes. Records are assumed appended in
// non-decreasing timestamp order (the collector samples forward in time).
type buffer struct {
	mu         sync.Mutex
	records    []record
	maxAge     time.Duration
	maxRecords int
	generation int64
}

type bufferOption func(*buffer)

func withMaxAge(d time.Duration) bufferOption { return func(b *buffer) { b.maxAge = d } }
func withMaxRecords(n int) bufferOption       { return func(b *buffer) { b.maxRecords = n } }

func newBuffer(opts ...bufferOption) *buffer {
	b := &buffer{maxAge: defaultMaxAge, maxRecords: defaultMaxRecords}
	for _, o := range opts {
		o(b)
	}
	return b
}

// setGeneration stamps subsequently-added records with the given AgentDeploy
// generation.
func (b *buffer) setGeneration(gen int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.generation = gen
}

// add appends a sample and evicts anything beyond the age/count bounds, using
// the sample's own timestamp as the reference "now".
func (b *buffer) add(s Sample) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.records = append(b.records, record{sample: s, count: 1, generation: b.generation})
	b.evictLocked(s.Timestamp)
}

// evictLocked drops records older than maxAge and, if still over maxRecords,
// the oldest records. Caller holds the lock.
func (b *buffer) evictLocked(now time.Time) {
	cutoff := now.Add(-b.maxAge)
	i := 0
	for i < len(b.records) && b.records[i].sample.Timestamp.Before(cutoff) {
		i++
	}
	if i > 0 {
		b.records = append(b.records[:0], b.records[i:]...)
	}
	if over := len(b.records) - b.maxRecords; over > 0 {
		b.records = append(b.records[:0], b.records[over:]...)
	}
}

// samples returns a time-ordered copy of the buffered samples still within the
// age window relative to now.
func (b *buffer) samples(now time.Time) []Sample {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.evictLocked(now)
	out := make([]Sample, len(b.records))
	for i, r := range b.records {
		out[i] = r.sample
	}
	return out
}

// snapshot returns a copy of all records, for encoding.
func (b *buffer) snapshot() []record {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]record, len(b.records))
	copy(out, b.records)
	return out
}

// load replaces the buffer contents, used on startup rehydration.
func (b *buffer) load(records []record) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.records = append(b.records[:0], records...)
}
