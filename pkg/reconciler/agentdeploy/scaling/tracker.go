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
	"k8s.io/apimachinery/pkg/types"

	"github.com/kynoproj/kynomesh/pkg/reconciler/agentdeploy/scaling/sampling"
)

// Tracker fans Track/Forget out to the Sampler and Autoscaler, each of which
// runs its own WorkSet. The agentdeploy controller drives it to keep both
// components' watch sets in sync with the live AgentDeploys.
type Tracker struct {
	sampler    *sampling.Sampler
	autoscaler *Autoscaler
}

// NewTracker returns a Tracker over the given Sampler and Autoscaler.
func NewTracker(sampler *sampling.Sampler, autoscaler *Autoscaler) *Tracker {
	return &Tracker{sampler: sampler, autoscaler: autoscaler}
}

// Track adds an AgentDeploy to both the Sampler's and Autoscaler's WorkSets.
func (t *Tracker) Track(k types.NamespacedName) {
	t.sampler.Track(k)
	t.autoscaler.Track(k)
}

// Forget removes an AgentDeploy from both WorkSets. Either Forget already
// drops the Registry entry and metric series, so the second call is a no-op.
func (t *Tracker) Forget(k types.NamespacedName) {
	t.sampler.Forget(k)
	t.autoscaler.Forget(k)
}
