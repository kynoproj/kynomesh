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

package broker

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLimiter_UnlimitedForNonPositive(t *testing.T) {
	for _, max := range []int{0, -1, -100} {
		l := NewLimiter(max)
		for range 1000 {
			release, ok := l.Acquire()
			require.True(t, ok)
			require.NotNil(t, release)
			release()
		}
	}
}

func TestSemaphoreLimiter_AdmitsUpToCap(t *testing.T) {
	l := NewLimiter(3)
	releases := make([]func(), 0, 3)
	for i := range 3 {
		release, ok := l.Acquire()
		require.Truef(t, ok, "acquire %d under cap should succeed", i)
		releases = append(releases, release)
	}
	for _, r := range releases {
		r()
	}
}

func TestSemaphoreLimiter_RejectsAtCap(t *testing.T) {
	l := NewLimiter(2)
	r1, ok := l.Acquire()
	require.True(t, ok)
	r2, ok := l.Acquire()
	require.True(t, ok)

	_, ok = l.Acquire()
	assert.False(t, ok, "third acquire at cap=2 must be rejected")

	r1()
	r3, ok := l.Acquire()
	assert.True(t, ok, "a slot freed by release must admit the next request")
	r3()
	r2()
}

func TestSemaphoreLimiter_ReleaseIsIdempotentPerSlot(t *testing.T) {
	l := NewLimiter(1)
	r, ok := l.Acquire()
	require.True(t, ok)
	_, ok = l.Acquire()
	require.False(t, ok)
	r()
	r2, ok := l.Acquire()
	require.True(t, ok, "slot must be reusable after release")
	r2()
}

func TestSemaphoreLimiter_ConcurrentAcquireNeverExceedsCap(t *testing.T) {
	const cap = 8
	l := NewLimiter(cap)

	var mu sync.Mutex
	current, peak := 0, 0
	var wg sync.WaitGroup
	for range 200 {
		wg.Go(func() {
			release, ok := l.Acquire()
			if !ok {
				return
			}
			mu.Lock()
			current++
			if current > peak {
				peak = current
			}
			mu.Unlock()

			mu.Lock()
			current--
			mu.Unlock()
			release()
		})
	}
	wg.Wait()
	assert.LessOrEqual(t, peak, cap, "concurrent in-flight must never exceed the cap")
}
