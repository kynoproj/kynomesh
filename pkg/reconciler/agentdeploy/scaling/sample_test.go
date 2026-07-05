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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// sample builds a Sample with the given per-replica concurrency and rate.
func sample(ts time.Time, replicas int32, inflight, rate float64) Sample {
	return Sample{
		Timestamp:      ts,
		Replicas:       replicas,
		InflightPerRep: inflight,
		RatePerRep:     rate,
	}
}

func TestSampleValid(t *testing.T) {
	base := time.Now()
	assert.True(t, sample(base, 2, 10, 100).valid())
	assert.False(t, sample(base, 0, 10, 100).valid())
	assert.False(t, sample(base, 1, 0, 100).valid())
	assert.False(t, sample(base, 1, 10, 0).valid())
}

func TestSanitizeDropsInvalid(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	history := []Sample{
		sample(base, 2, 10, 100),                     // keep
		sample(base.Add(15*time.Second), 2, 0, 100),  // invalid: zero inflight
		sample(base.Add(30*time.Second), 2, 12, 0),   // invalid: zero rate
		sample(base.Add(45*time.Second), 2, 12, 120), // keep
	}
	clean := sanitize(history)
	assert.Len(t, clean, 2)
	assert.Equal(t, 10.0, clean[0].InflightPerRep)
	assert.Equal(t, 12.0, clean[1].InflightPerRep)
}

func TestSanitizeSkipsWarmupAfterScale(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	history := []Sample{
		sample(base, 2, 10, 100),                      // keep (steady at 2)
		sample(base.Add(30*time.Second), 3, 10, 100),  // scale event → within warmup, drop
		sample(base.Add(60*time.Second), 3, 10, 100),  // still within 60s warmup, drop
		sample(base.Add(100*time.Second), 3, 11, 110), // past warmup, keep
	}
	clean := sanitize(history)
	assert.Len(t, clean, 2)
	assert.Equal(t, int32(2), clean[0].Replicas)
	assert.Equal(t, 11.0, clean[1].InflightPerRep)
}

func TestSanitizeSortsByTime(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	history := []Sample{
		sample(base.Add(45*time.Second), 2, 12, 120),
		sample(base, 2, 10, 100),
	}
	clean := sanitize(history)
	assert.Len(t, clean, 2)
	assert.True(t, clean[0].Timestamp.Before(clean[1].Timestamp))
}

func TestSanitizeDedupsSameTimestamp(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	history := []Sample{
		sample(base, 2, 10, 100),                     // superseded by the next same-instant reading
		sample(base, 2, 15, 150),                     // last write for base wins
		sample(base.Add(30*time.Second), 2, 12, 120), // distinct instant, kept
	}
	clean := sanitize(history)
	assert.Len(t, clean, 2)
	assert.Equal(t, 15.0, clean[0].InflightPerRep, "last reading for the shared instant wins")
	assert.Equal(t, 12.0, clean[1].InflightPerRep)
}

func TestSanitizeDedupsAcrossUnsortedInput(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Same instant delivered out of order; dedup runs after the sort.
	history := []Sample{
		sample(base.Add(30*time.Second), 2, 12, 120),
		sample(base, 2, 10, 100),
		sample(base.Add(30*time.Second), 2, 20, 200),
	}
	clean := sanitize(history)
	assert.Len(t, clean, 2)
	assert.Equal(t, 10.0, clean[0].InflightPerRep)
	assert.Equal(t, 20.0, clean[1].InflightPerRep, "last reading at t+30s wins")
}

func TestSanitizeEmpty(t *testing.T) {
	assert.Nil(t, sanitize(nil))
	assert.Nil(t, sanitize([]Sample{}))
}
