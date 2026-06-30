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

func TestBufferEvictsByAge(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	b := newBuffer(withMaxAge(time.Minute), withMaxRecords(1000))
	b.add(sample(base, 1, 10, 100))
	b.add(sample(base.Add(30*time.Second), 1, 11, 110))
	b.add(sample(base.Add(90*time.Second), 1, 12, 120)) // cutoff = base+30s

	got := b.samples(base.Add(90 * time.Second))
	assert.Len(t, got, 2, "the base sample is older than the 1m window")
	assert.Equal(t, 11.0, got[0].InflightPerRep)
	assert.Equal(t, 12.0, got[1].InflightPerRep)
}

func TestBufferEvictsByCount(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	b := newBuffer(withMaxAge(time.Hour), withMaxRecords(2))
	b.add(sample(base, 1, 10, 100))
	b.add(sample(base.Add(time.Second), 1, 11, 110))
	b.add(sample(base.Add(2*time.Second), 1, 12, 120))

	got := b.samples(base.Add(2 * time.Second))
	assert.Len(t, got, 2, "count cap keeps the newest two")
	assert.Equal(t, 11.0, got[0].InflightPerRep)
	assert.Equal(t, 12.0, got[1].InflightPerRep)
}

func TestBufferStampsGenerationAndCount(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	b := newBuffer()
	b.setGeneration(7)
	b.add(sample(base, 2, 10, 100))

	snap := b.snapshot()
	assert.Len(t, snap, 1)
	assert.Equal(t, int64(7), snap[0].generation)
	assert.Equal(t, uint16(1), snap[0].count)
}

func TestBufferSnapshotLoadRoundTrip(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	src := newBuffer()
	src.add(sample(base, 2, 10, 100))
	src.add(sample(base.Add(15*time.Second), 2, 11, 110))

	dst := newBuffer()
	dst.load(src.snapshot())
	assert.Equal(t, src.snapshot(), dst.snapshot())
}

func TestBufferSamplesIsACopy(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	b := newBuffer()
	b.add(sample(base, 2, 10, 100))

	got := b.samples(base)
	got[0].InflightPerRep = 999
	assert.Equal(t, 10.0, b.samples(base)[0].InflightPerRep, "mutating the returned slice must not affect the buffer")
}
