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
	"context"
	"errors"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubResolver struct {
	ips []string
	err error
}

func (s stubResolver) LookupHost(_ context.Context, _ string) ([]string, error) {
	return s.ips, s.err
}

// ips returns n placeholder addresses; discovery.Discover only uses the count.
func ips(n int) []string {
	out := make([]string, n)
	for i := range n {
		out[i] = "10.0.0.1"
	}
	return out
}

func TestSliceFor(t *testing.T) {
	cases := []struct {
		name        string
		maxInFlight int
		replicas    int
		want        int
	}{
		{name: "single replica gets the whole cap", maxInFlight: 20, replicas: 1, want: 20},
		{name: "zero replicas leaves the full cap", maxInFlight: 20, replicas: 0, want: 20},
		{name: "even split", maxInFlight: 20, replicas: 4, want: 5},
		{name: "floors the division", maxInFlight: 20, replicas: 3, want: 6},
		{name: "never below 1 when replicas exceed cap", maxInFlight: 2, replicas: 8, want: 1},
		{name: "exactly one each", maxInFlight: 5, replicas: 5, want: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, sliceFor(tc.maxInFlight, tc.replicas))
		})
	}
}

func TestDNSCountLimiter_StartsAtFullCapBeforeFirstRead(t *testing.T) {
	l, _ := NewDNSCountLimiter(3, "greeter", "default", stubResolver{ips: ips(3)})
	// No recount yet: the whole cap is enforceable locally.
	releases := make([]func(), 0, 3)
	for range 3 {
		r, ok := l.Acquire()
		require.True(t, ok)
		releases = append(releases, r)
	}
	_, ok := l.Acquire()
	assert.False(t, ok, "must still be bounded by the full cap before the first DNS read")
	for _, r := range releases {
		r()
	}
}

func TestDNSCountLimiter_RecountNarrowsSlice(t *testing.T) {
	l, _ := NewDNSCountLimiter(12, "greeter", "default", stubResolver{ips: ips(4)})
	dl := l.(*dnsCountLimiter)

	dl.recount(context.Background())
	assert.Equal(t, 3, dl.sem.limitValue(), "12 cap / 4 replicas = slice of 3")

	r1, ok := l.Acquire()
	require.True(t, ok)
	r2, ok := l.Acquire()
	require.True(t, ok)
	r3, ok := l.Acquire()
	require.True(t, ok)
	_, ok = l.Acquire()
	assert.False(t, ok, "slice of 3 must reject the 4th concurrent request")
	r1()
	r2()
	r3()
}

func TestDNSCountLimiter_RebalancesOnScale(t *testing.T) {
	res := &mutableResolver{ips: ips(2)}
	l, _ := NewDNSCountLimiter(12, "greeter", "default", res)
	dl := l.(*dnsCountLimiter)

	dl.recount(context.Background())
	assert.Equal(t, 6, dl.sem.limitValue(), "12 / 2 = 6")

	res.set(ips(6)) // scale up
	dl.recount(context.Background())
	assert.Equal(t, 2, dl.sem.limitValue(), "12 / 6 = 2 after scale-up")

	res.set(ips(3)) // scale down
	dl.recount(context.Background())
	assert.Equal(t, 4, dl.sem.limitValue(), "12 / 3 = 4 after scale-down")
}

func TestDNSCountLimiter_KeepsSliceOnDNSError(t *testing.T) {
	res := &mutableResolver{ips: ips(4)}
	l, _ := NewDNSCountLimiter(12, "greeter", "default", res)
	dl := l.(*dnsCountLimiter)

	dl.recount(context.Background())
	require.Equal(t, 3, dl.sem.limitValue())

	res.setErr(errors.New("network unreachable"))
	dl.recount(context.Background())
	assert.Equal(t, 3, dl.sem.limitValue(), "a DNS error must keep the previous slice, not reset it")
}

func TestDNSCountLimiter_NoReadyPodsLeavesFullCap(t *testing.T) {
	// NXDOMAIN (no ready pods) is treated as zero replicas -> full cap kept.
	res := stubResolver{err: &net.DNSError{Err: "no such host", IsNotFound: true}}
	l, _ := NewDNSCountLimiter(9, "greeter", "default", res)
	dl := l.(*dnsCountLimiter)

	dl.recount(context.Background())
	assert.Equal(t, 9, dl.sem.limitValue(), "no ready pods -> keep the full cap")
}

func TestDNSCountLimiter_RunDoesImmediateRecountThenStopsOnCancel(t *testing.T) {
	l, start := NewDNSCountLimiter(12, "greeter", "default", stubResolver{ips: ips(4)})
	dl := l.(*dnsCountLimiter)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled: run must do its one immediate recount, then return
	start(ctx)

	assert.Equal(t, 3, dl.sem.limitValue(), "run must recount once before honoring cancellation")
}

// mutableResolver is a stub whose response can change between recounts, to
// exercise rebalancing on scale events.
type mutableResolver struct {
	ips []string
	err error
}

func (m *mutableResolver) LookupHost(_ context.Context, _ string) ([]string, error) {
	return m.ips, m.err
}
func (m *mutableResolver) set(ips []string) { m.ips, m.err = ips, nil }
func (m *mutableResolver) setErr(err error) { m.err = err }
