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
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetricsRecordAndDelete(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	ad := scalingAD("foo", 2) // namespace "ns", agentSet "set", short name "foo"

	m.RecordSample(ad)
	m.RecordSample(ad)
	m.ObserveDecision(ad, Estimate{KneePerReplica: 20, Confidence: 0.8}, 3, 5)
	m.RecordScale(ad, true)  // up
	m.RecordScale(ad, false) // down

	assert.Equal(t, 2.0, testutil.ToFloat64(m.samplesTotal.WithLabelValues("ns", "set", "foo")))
	assert.Equal(t, 20.0, testutil.ToFloat64(m.knee.WithLabelValues("ns", "set", "foo")))
	assert.Equal(t, 0.8, testutil.ToFloat64(m.confidence.WithLabelValues("ns", "set", "foo")))
	assert.Equal(t, 5.0, testutil.ToFloat64(m.desiredReplicas.WithLabelValues("ns", "set", "foo")))
	assert.Equal(t, 3.0, testutil.ToFloat64(m.currentReplicas.WithLabelValues("ns", "set", "foo")))
	assert.Equal(t, 1.0, testutil.ToFloat64(m.scaleEvents.WithLabelValues("ns", "set", "foo", directionUp)))
	assert.Equal(t, 1.0, testutil.ToFloat64(m.scaleEvents.WithLabelValues("ns", "set", "foo", directionDown)))

	// Delete drops every series for the AgentDeploy (both scale directions too).
	m.Delete(nn("foo"))
	count, err := testutil.GatherAndCount(reg,
		"autoscaler_knee_per_replica", "autoscaler_confidence",
		"autoscaler_desired_replicas", "autoscaler_current_replicas",
		"autoscaler_samples_total", "autoscaler_scale_events_total")
	require.NoError(t, err)
	assert.Equal(t, 0, count, "all series removed on Delete")
}

func TestMetricsNilSafe(t *testing.T) {
	var m *Metrics
	ad := scalingAD("foo", 1)
	assert.NotPanics(t, func() {
		m.RecordSample(ad)
		m.ObserveDecision(ad, Estimate{}, 1, 1)
		m.RecordScale(ad, true)
		m.Delete(nn("foo"))
	})
}

func TestMetricsExposition(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	m.RecordScale(scalingAD("foo", 1), true)

	assert.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(`
# HELP autoscaler_scale_events_total Replica scale operations applied, by direction.
# TYPE autoscaler_scale_events_total counter
autoscaler_scale_events_total{agentDeploy="foo",agentSet="set",direction="up",namespace="ns"} 1
`), "autoscaler_scale_events_total"))
}
