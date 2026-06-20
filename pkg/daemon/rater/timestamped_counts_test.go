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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTimestampedCounts_UpdateAndSnapshot(t *testing.T) {
	tc := NewTimestampedCounts(100)
	tc.Update("pod-0", &PodSample{
		CounterByTransport: map[string]float64{"rest": 10, "grpc": 5},
		GaugeByTransport:   map[string]float64{"rest": 2, "grpc": 1},
	})
	snap := tc.Snapshot()
	assert.Equal(t, int64(100), tc.Timestamp())
	assert.Len(t, snap, 1)
	assert.Equal(t, float64(10), snap["pod-0"].CounterByTransport["rest"])
	assert.Equal(t, float64(2), snap["pod-0"].GaugeByTransport["rest"])
}

func TestTimestampedCounts_NilUpdateIgnored(t *testing.T) {
	tc := NewTimestampedCounts(100)
	tc.Update("pod-0", &PodSample{
		CounterByTransport: map[string]float64{"rest": 10},
	})
	tc.Update("pod-0", nil) // simulates a failed scrape
	snap := tc.Snapshot()
	// Previous value must remain — otherwise the next successful scrape
	// would produce a wildly inflated counter delta.
	assert.Equal(t, float64(10), snap["pod-0"].CounterByTransport["rest"])
}

func TestTimestampedCounts_SnapshotIsCopy(t *testing.T) {
	tc := NewTimestampedCounts(100)
	tc.Update("pod-0", &PodSample{
		CounterByTransport: map[string]float64{"rest": 10},
	})
	snap := tc.Snapshot()
	delete(snap, "pod-0")
	assert.Len(t, tc.Snapshot(), 1, "mutating returned snapshot must not affect storage")
}
