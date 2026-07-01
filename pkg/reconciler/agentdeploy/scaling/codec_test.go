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
	"github.com/stretchr/testify/require"
)

func TestCodecRoundTrip(t *testing.T) {
	recs := []record{
		{sample: Sample{Timestamp: time.Unix(0, 1234567890).UTC(), Replicas: 3, InflightPerRep: 12.5, RatePerRep: 200.25}, count: 1},
		{sample: Sample{Timestamp: time.Unix(0, 99).UTC(), Replicas: 1, InflightPerRep: 0.5, RatePerRep: 1.5}, count: 4},
	}
	const hash = "abc123def"

	gotHash, got, err := decodeHistory(encodeHistory(hash, recs))
	require.NoError(t, err)
	assert.Equal(t, hash, gotHash, "spec hash round-trips")
	require.Len(t, got, 2)
	for i, want := range recs {
		assert.Equal(t, want.sample.Timestamp.UnixNano(), got[i].sample.Timestamp.UnixNano(), "timestamp")
		assert.Equal(t, want.sample.Replicas, got[i].sample.Replicas, "replicas")
		assert.Equal(t, want.sample.InflightPerRep, got[i].sample.InflightPerRep, "inflight")
		assert.Equal(t, want.sample.RatePerRep, got[i].sample.RatePerRep, "rate")
		assert.Equal(t, want.count, got[i].count, "count")
	}
}

func TestCodecEmpty(t *testing.T) {
	gotHash, got, err := decodeHistory(nil)
	require.NoError(t, err)
	assert.Empty(t, gotHash)
	assert.Empty(t, got)

	gotHash, got, err = decodeHistory(encodeHistory("", nil))
	require.NoError(t, err)
	assert.Empty(t, gotHash)
	assert.Empty(t, got)
}

func TestCodecVersionMismatch(t *testing.T) {
	blob := encodeHistory("h", []record{{sample: Sample{Timestamp: time.Unix(0, 1).UTC(), Replicas: 1, InflightPerRep: 1, RatePerRep: 1}, count: 1}})
	blob[0] = 0xFF
	_, _, err := decodeHistory(blob)
	assert.Error(t, err)
}

func TestCodecTruncated(t *testing.T) {
	blob := encodeHistory("h", []record{{sample: Sample{Timestamp: time.Unix(0, 1).UTC(), Replicas: 1, InflightPerRep: 1, RatePerRep: 1}, count: 1}})
	_, _, err := decodeHistory(blob[:len(blob)-1])
	assert.Error(t, err)
}
