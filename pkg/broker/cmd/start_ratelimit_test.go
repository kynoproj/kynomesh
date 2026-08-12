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

package cmd

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

func i32(v int32) *int32 { return &v }

func adWithCap(name, ns string, cap *int32) *kmv1.AgentDeploy {
	ad := &kmv1.AgentDeploy{}
	ad.Name = name
	ad.Namespace = ns
	if cap != nil {
		ad.Spec.RateLimit = &kmv1.RateLimit{MaxInFlight: cap}
	}
	return ad
}

func TestResolveMaxInFlight(t *testing.T) {
	adWith := func(limit *int32) *kmv1.AgentDeploy {
		ad := &kmv1.AgentDeploy{}
		if limit != nil {
			ad.Spec.RateLimit = &kmv1.RateLimit{MaxInFlight: limit}
		}
		return ad
	}
	ptr := func(v int32) *int32 { return &v }

	cases := []struct {
		name string
		ad   *kmv1.AgentDeploy
		want int
	}{
		{name: "nil agentdeploy is unlimited", ad: nil, want: 0},
		{name: "no rateLimit block is unlimited", ad: &kmv1.AgentDeploy{}, want: 0},
		{name: "rateLimit with nil maxInFlight is unlimited", ad: adWith(nil), want: 0},
		{name: "explicit zero is unlimited", ad: adWith(ptr(0)), want: 0},
		{name: "positive cap is honored", ad: adWith(ptr(20)), want: 20},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, resolveMaxInFlight(tc.ad))
		})
	}
}

func TestBuildLimiter_UnlimitedAdmitsEverything(t *testing.T) {
	l := buildLimiter(context.Background(), adWithCap("greeter", "default", nil))
	for range 100 {
		release, ok := l.Acquire()
		require.True(t, ok)
		release()
	}
}

func TestBuildLimiter_PodLocalWhenNoAgentDeployIdentity(t *testing.T) {
	// A cap but no name/namespace (local-dev): pod-local cap, no DNS loop.
	l := buildLimiter(context.Background(), adWithCap("", "", i32(2)))
	r1, ok := l.Acquire()
	require.True(t, ok)
	r2, ok := l.Acquire()
	require.True(t, ok)
	_, ok = l.Acquire()
	assert.False(t, ok, "pod-local cap of 2 must reject the 3rd concurrent request")
	r1()
	r2()
}

func TestBuildLimiter_FleetLimiterEnforcesCapBeforeDNSNarrows(t *testing.T) {
	// In-cluster (name+namespace present): a DNS-count limiter. Inject a stub
	// resolver via the seam and a pre-cancelled ctx so the loop does its one
	// immediate recount against the stub, then stops — no real DNS, no races.
	restore := dnsResolver
	t.Cleanup(func() { dnsResolver = restore })
	dnsResolver = stubResolver{ips: []string{"10.0.0.1"}} // 1 replica -> full cap

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	l := buildLimiter(ctx, adWithCap("greeter", "default", i32(2)))
	r1, ok := l.Acquire()
	require.True(t, ok)
	r2, ok := l.Acquire()
	require.True(t, ok)
	_, ok = l.Acquire()
	assert.False(t, ok, "fleet limiter must enforce the cap even before the first DNS read")
	r1()
	r2()
}

type stubResolver struct{ ips []string }

func (s stubResolver) LookupHost(context.Context, string) ([]string, error) { return s.ips, nil }
