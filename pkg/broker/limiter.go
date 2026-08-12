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

// Limiter decides whether a new in-flight request may be admitted. It is the
// single seam the HTTP and gRPC proxy paths call for admission control; the
// enforcement wiring never changes across limiter implementations, only the
// admission policy behind this interface does (e.g. pod-local now, DNS-count
// or a distributed backend later).
type Limiter interface {
	// Acquire reserves one in-flight slot. On success it returns a release
	// func (call exactly once when the request completes) and ok=true. When at
	// capacity it returns a nil release and ok=false, and the caller must reject
	// the request. A nil Limiter, or one representing "unlimited", always admits.
	Acquire() (release func(), ok bool)
}

// noopRelease is returned whenever admission succeeds without holding a real
// slot (unlimited limiter), so callers can always defer release unconditionally.
func noopRelease() {}

// unlimited admits every request. It is used when no max-in-flight cap is
// configured, so the proxy path can call Acquire unconditionally.
type unlimited struct{}

func (unlimited) Acquire() (func(), bool) { return noopRelease, true }

// semaphoreLimiter caps concurrent in-flight requests at a fixed count using a
// buffered channel as a counting semaphore. It is pod-local: the cap applies to
// this broker instance only.
type semaphoreLimiter struct {
	slots chan struct{}
}

// NewLimiter returns a Limiter capping in-flight requests at maxInFlight. A
// maxInFlight of 0 or negative means unlimited.
func NewLimiter(maxInFlight int) Limiter {
	if maxInFlight <= 0 {
		return unlimited{}
	}
	return &semaphoreLimiter{slots: make(chan struct{}, maxInFlight)}
}

// Acquire is non-blocking: it takes a slot if one is free, otherwise reports
// ok=false immediately so the caller can shed load rather than queue.
func (l *semaphoreLimiter) Acquire() (func(), bool) {
	select {
	case l.slots <- struct{}{}:
		return func() { <-l.slots }, true
	default:
		return nil, false
	}
}
