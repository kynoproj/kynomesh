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

package ratelimit

import "sync"

// semaphore is a non-blocking counting semaphore whose limit can change while
// slots are held.
type semaphore struct {
	mu    sync.Mutex
	held  int
	limit int
}

func newSemaphore(limit int) *semaphore {
	return &semaphore{limit: limit}
}

// Acquire is non-blocking: it takes a slot if held < limit, otherwise reports
// ok=false immediately so the caller can shed load rather than queue.
func (s *semaphore) Acquire() (func(), bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.held >= s.limit {
		return nil, false
	}
	s.held++
	return s.release, true
}

// release frees one held slot. It is idempotent-safe only in the sense that the
// limiter contract calls it exactly once per successful Acquire; held never goes
// negative because release is only ever the return value of a successful take.
func (s *semaphore) release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.held > 0 {
		s.held--
	}
}

// SetLimit updates the admission ceiling. Held slots are untouched; if the new
// limit is below held, no new requests are admitted until enough release.
func (s *semaphore) SetLimit(limit int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.limit = limit
}

// limitValue returns the current limit — test-only introspection.
func (s *semaphore) limitValue() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.limit
}
