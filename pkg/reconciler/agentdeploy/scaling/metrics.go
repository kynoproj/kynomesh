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
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"k8s.io/apimachinery/pkg/types"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

// Metrics holds the autoscaling Prometheus collectors, labeled by namespace,
// AgentSet, and the short agentDeploy (agent) name. Because those label values
// aren't recoverable from an AgentDeploy's object key alone, Metrics remembers
// each key's label set so Delete can drop all its series when the AgentDeploy is
// forgotten — avoiding series leaks at scale. All methods are nil-safe.
type Metrics struct {
	knee            *prometheus.GaugeVec
	confidence      *prometheus.GaugeVec
	desiredReplicas *prometheus.GaugeVec
	currentReplicas *prometheus.GaugeVec
	samplesTotal    *prometheus.CounterVec
	scaleEvents     *prometheus.CounterVec

	mu     sync.Mutex
	labels map[types.NamespacedName]prometheus.Labels
}

var (
	baseLabels  = []string{"namespace", "agentSet", "agentDeploy"}
	scaleLabels = []string{"namespace", "agentSet", "agentDeploy", "direction"}
)

const (
	directionUp   = "up"
	directionDown = "down"
)

// NewMetrics registers the autoscaling metrics on the supplied registry.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	return &Metrics{
		knee: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
			Name: "autoscaler_knee_per_replica",
			Help: "Learned per-replica saturation knee (in-flight requests) for an AgentDeploy.",
		}, baseLabels),
		confidence: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
			Name: "autoscaler_confidence",
			Help: "Confidence in the learned knee, 0 to 1.",
		}, baseLabels),
		desiredReplicas: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
			Name: "autoscaler_desired_replicas",
			Help: "Replica count the autoscaler last computed for an AgentDeploy.",
		}, baseLabels),
		currentReplicas: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
			Name: "autoscaler_current_replicas",
			Help: "Replica count the autoscaler last scaled from (observed running replicas).",
		}, baseLabels),
		samplesTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "autoscaler_samples_total",
			Help: "Load samples recorded for an AgentDeploy.",
		}, baseLabels),
		scaleEvents: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "autoscaler_scale_events_total",
			Help: "Replica scale operations applied, by direction.",
		}, scaleLabels),
		labels: make(map[types.NamespacedName]prometheus.Labels),
	}
}

// remember records the AgentDeploy's label set (keyed by its object key) so
// Delete can find it later, and returns the base label values in order.
func (m *Metrics) remember(ad *kmv1.AgentDeploy) (namespace, agentSet, agentDeploy string) {
	namespace, agentSet, agentDeploy = ad.Namespace, ad.Spec.AgentSetName, ad.Spec.Name
	m.mu.Lock()
	m.labels[types.NamespacedName{Namespace: ad.Namespace, Name: ad.Name}] = prometheus.Labels{
		"namespace": namespace, "agentSet": agentSet, "agentDeploy": agentDeploy,
	}
	m.mu.Unlock()
	return
}

// RecordSample counts one recorded load sample.
func (m *Metrics) RecordSample(ad *kmv1.AgentDeploy) {
	if m == nil {
		return
	}
	ns, as, adn := m.remember(ad)
	m.samplesTotal.WithLabelValues(ns, as, adn).Inc()
}

// ObserveDecision publishes the estimate and replica counts from one decision.
func (m *Metrics) ObserveDecision(ad *kmv1.AgentDeploy, est Estimate, current, desired int32) {
	if m == nil {
		return
	}
	ns, as, adn := m.remember(ad)
	m.knee.WithLabelValues(ns, as, adn).Set(est.KneePerReplica)
	m.confidence.WithLabelValues(ns, as, adn).Set(est.Confidence)
	m.currentReplicas.WithLabelValues(ns, as, adn).Set(float64(current))
	m.desiredReplicas.WithLabelValues(ns, as, adn).Set(float64(desired))
}

// RecordScale counts one applied scale operation.
func (m *Metrics) RecordScale(ad *kmv1.AgentDeploy, up bool) {
	if m == nil {
		return
	}
	ns, as, adn := m.remember(ad)
	dir := directionDown
	if up {
		dir = directionUp
	}
	m.scaleEvents.WithLabelValues(ns, as, adn, dir).Inc()
}

// Delete removes all series for an AgentDeploy (by object key), called when it
// is forgotten so deleted objects don't leak metric series.
func (m *Metrics) Delete(k types.NamespacedName) {
	if m == nil {
		return
	}
	m.mu.Lock()
	l, ok := m.labels[k]
	delete(m.labels, k)
	m.mu.Unlock()
	if !ok {
		return
	}
	// Partial match on the base labels also clears the direction-labeled
	// scale-event series.
	m.knee.DeletePartialMatch(l)
	m.confidence.DeletePartialMatch(l)
	m.desiredReplicas.DeletePartialMatch(l)
	m.currentReplicas.DeletePartialMatch(l)
	m.samplesTotal.DeletePartialMatch(l)
	m.scaleEvents.DeletePartialMatch(l)
}
