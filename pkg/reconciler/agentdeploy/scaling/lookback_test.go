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
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestMedianRequestDuration(t *testing.T) {
	d := func(inflight, rate float64) Sample {
		return Sample{InflightPerRep: inflight, RatePerRep: rate}
	}
	tests := []struct {
		name    string
		samples []Sample
		want    time.Duration
	}{
		{"empty", nil, 0},
		{"no usable rate", []Sample{d(10, 0), d(5, 0)}, 0},
		{"single D=5s", []Sample{d(10, 2)}, 5 * time.Second},
		{"odd median", []Sample{d(2, 2), d(10, 2), d(4, 2)}, 2 * time.Second}, // Ds 1,5,2 → median 2
		{"even median", []Sample{d(2, 2), d(8, 2)}, 2500 * time.Millisecond},  // Ds 1,4 → 2.5
		{"skips zero rate", []Sample{d(10, 0), d(20, 2)}, 10 * time.Second},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, medianRequestDuration(tc.samples))
		})
	}
}

func TestClampDuration(t *testing.T) {
	lo, hi := 30*time.Second, 15*time.Minute
	assert.Equal(t, lo, clampDuration(time.Second, lo, hi))
	assert.Equal(t, hi, clampDuration(time.Hour, lo, hi))
	assert.Equal(t, time.Minute, clampDuration(time.Minute, lo, hi))
}

func TestLookbackSecondsColdStart(t *testing.T) {
	assert.Equal(t, int64(0), lookbackSeconds(nil, time.Now(), 30*time.Second),
		"no history → 0 lets the daemon use its built-in 1m window")
}

func TestLookbackSecondsAdaptive(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name           string
		inflight, rate float64
		interval       time.Duration
		want           int64
	}{
		{"mid range: 5*D", 40, 2, 30 * time.Second, 100},  // D=20s → 100s
		{"clamped to min", 4, 2, 30 * time.Second, 30},    // D=2s → 10s → floor 30s
		{"clamped to max", 600, 2, 30 * time.Second, 900}, // D=300s → 1500s → cap 15m
		// Window shorter than the scrape interval is lifted to the interval so
		// recorded history has no temporal gaps.
		{"floored at interval", 20, 2, 90 * time.Second, 90}, // D=10s → 50s → floor 90s
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ad := scalingAD("foo", 2)
			c := fake.NewClientBuilder().WithScheme(storeScheme(t)).WithObjects(ad).Build()
			store, err := NewRegistry(c).StoreFor(context.Background(), ad)
			require.NoError(t, err)
			store.Record(Sample{Timestamp: now, Replicas: 2, InflightPerRep: tc.inflight, RatePerRep: tc.rate}, "")
			assert.Equal(t, tc.want, lookbackSeconds(store, now, tc.interval))
		})
	}
}
