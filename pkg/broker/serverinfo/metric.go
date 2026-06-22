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

package serverinfo

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// RegisterMetric publishes a constant 1 gauge labeled with the agent
// server's protocol, language, and version so the info can be scraped.
func RegisterMetric(registry prometheus.Registerer, info ServerInfo) {
	gauge := promauto.With(registry).NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "broker_agent_server_info",
			Help: "Static info about the agent server colocated with this broker. Always 1; labels carry the values.",
		},
		[]string{"protocol", "language", "version"},
	)
	gauge.WithLabelValues(string(info.Protocol), string(info.Language), info.Version).Set(1)
}
