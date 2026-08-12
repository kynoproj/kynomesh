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

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSemaphore_AdmitsUpToLimitThenRejects(t *testing.T) {
	s := newSemaphore(2)
	r1, ok := s.Acquire()
	require.True(t, ok)
	r2, ok := s.Acquire()
	require.True(t, ok)

	_, ok = s.Acquire()
	assert.False(t, ok, "acquire at the limit must be rejected")

	r1()
	r3, ok := s.Acquire()
	assert.True(t, ok, "a released slot must be reusable")
	r3()
	r2()
}

func TestSemaphore_SetLimitUpAdmitsMore(t *testing.T) {
	s := newSemaphore(1)
	r1, ok := s.Acquire()
	require.True(t, ok)
	_, ok = s.Acquire()
	require.False(t, ok)

	s.SetLimit(3)
	r2, ok := s.Acquire()
	assert.True(t, ok, "raising the limit must admit more")
	r3, ok := s.Acquire()
	assert.True(t, ok)
	_, ok = s.Acquire()
	assert.False(t, ok, "still bounded by the new, higher limit")

	r1()
	r2()
	r3()
}

func TestSemaphore_SetLimitDownStopsAdmittingButKeepsHeld(t *testing.T) {
	s := newSemaphore(3)
	r1, ok := s.Acquire()
	require.True(t, ok)
	r2, ok := s.Acquire()
	require.True(t, ok)
	r3, ok := s.Acquire()
	require.True(t, ok)

	// Lower the limit below the 3 currently held: no eviction, but no new
	// admissions until held drains under the new limit.
	s.SetLimit(1)
	_, ok = s.Acquire()
	assert.False(t, ok, "lowered limit must stop new admissions while over-subscribed")

	r1()
	_, ok = s.Acquire()
	assert.False(t, ok, "still 2 held vs limit 1 — must keep rejecting")
	r2()
	_, ok = s.Acquire()
	assert.False(t, ok, "1 held vs limit 1 — still at capacity")
	r3()
	r4, ok := s.Acquire()
	assert.True(t, ok, "once held drops under the limit, admit again")
	r4()
}

func TestSemaphore_ConcurrentNeverExceedsLimit(t *testing.T) {
	const limit = 8
	s := newSemaphore(limit)

	var mu sync.Mutex
	held, peak := 0, 0
	var wg sync.WaitGroup
	for range 300 {
		wg.Go(func() {
			release, ok := s.Acquire()
			if !ok {
				return
			}
			mu.Lock()
			held++
			if held > peak {
				peak = held
			}
			mu.Unlock()

			mu.Lock()
			held--
			mu.Unlock()
			release()
		})
	}
	wg.Wait()
	assert.LessOrEqual(t, peak, limit)
}

func TestNewLimiter_UnlimitedForNonPositive(t *testing.T) {
	for _, max := range []int{0, -1, -100} {
		l := NewLimiter(max)
		for range 100 {
			release, ok := l.Acquire()
			require.True(t, ok)
			require.NotNil(t, release)
			release()
		}
	}
}
