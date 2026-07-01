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
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

// codecVersion is the leading byte of every encoded blob; bump it on any wire
// change so old data is rejected rather than misread.
const codecVersion byte = 2

// recordSize is the fixed on-wire size of one record:
// timestamp(8) + replicas(4) + inflight(8) + rate(8) + count(2).
const recordSize = 8 + 4 + 8 + 8 + 2

// blob layout: version(1) | specHashLen(2) | specHash(N) | recordCount(4) | records.
const (
	versionSize = 1
	hashLenSize = 2
	countSize   = 4
)

// encodeHistory serializes the store's spec hash and records to a compact,
// versioned big-endian blob. The spec hash is stored once (not per record) —
// it identifies the pod spec the whole window was collected under.
func encodeHistory(specHash string, records []record) []byte {
	buf := make([]byte, versionSize+hashLenSize+len(specHash)+countSize+len(records)*recordSize)
	buf[0] = codecVersion
	off := 1
	binary.BigEndian.PutUint16(buf[off:], uint16(len(specHash)))
	off += hashLenSize
	off += copy(buf[off:], specHash)
	binary.BigEndian.PutUint32(buf[off:], uint32(len(records)))
	off += countSize
	for _, r := range records {
		binary.BigEndian.PutUint64(buf[off:], uint64(r.sample.Timestamp.UnixNano()))
		off += 8
		binary.BigEndian.PutUint32(buf[off:], uint32(r.sample.Replicas))
		off += 4
		binary.BigEndian.PutUint64(buf[off:], math.Float64bits(r.sample.InflightPerRep))
		off += 8
		binary.BigEndian.PutUint64(buf[off:], math.Float64bits(r.sample.RatePerRep))
		off += 8
		binary.BigEndian.PutUint16(buf[off:], r.count)
		off += 2
	}
	return buf
}

// decodeHistory parses a blob produced by encodeHistory. An empty input yields
// no records; a version mismatch or length inconsistency is an error.
func decodeHistory(b []byte) (string, []record, error) {
	if len(b) == 0 {
		return "", nil, nil
	}
	if b[0] != codecVersion {
		return "", nil, fmt.Errorf("scaling: unknown history codec version %d", b[0])
	}
	if len(b) < versionSize+hashLenSize {
		return "", nil, fmt.Errorf("scaling: history blob too short: %d bytes", len(b))
	}
	off := 1
	hashLen := int(binary.BigEndian.Uint16(b[off:]))
	off += hashLenSize
	if len(b) < off+hashLen+countSize {
		return "", nil, fmt.Errorf("scaling: history blob truncated in header")
	}
	specHash := string(b[off : off+hashLen])
	off += hashLen
	n := int(binary.BigEndian.Uint32(b[off:]))
	off += countSize
	if want := off + n*recordSize; len(b) != want {
		return "", nil, fmt.Errorf("scaling: history blob length %d, want %d for %d records", len(b), want, n)
	}
	out := make([]record, n)
	for i := range n {
		ts := int64(binary.BigEndian.Uint64(b[off:]))
		off += 8
		replicas := int32(binary.BigEndian.Uint32(b[off:]))
		off += 4
		inflight := math.Float64frombits(binary.BigEndian.Uint64(b[off:]))
		off += 8
		rate := math.Float64frombits(binary.BigEndian.Uint64(b[off:]))
		off += 8
		count := binary.BigEndian.Uint16(b[off:])
		off += 2
		out[i] = record{
			sample: Sample{
				Timestamp:      time.Unix(0, ts).UTC(),
				Replicas:       replicas,
				InflightPerRep: inflight,
				RatePerRep:     rate,
			},
			count: count,
		}
	}
	return specHash, out, nil
}
