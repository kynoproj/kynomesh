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

	"k8s.io/apimachinery/pkg/types"
)

// WatchSet is the single source of truth for which AgentDeploys the autoscaling
// components act on. The AgentDeploy controller drives it (Track / Forget); the
// Sampler and Autoscaler both iterate it via Snapshot. Safe for concurrent use.
type WatchSet struct {
	registry *Registry
	metrics  *Metrics
	mu       sync.RWMutex
	keys     map[types.NamespacedName]struct{}
}

// NewWatchSet returns an empty WatchSet.
func NewWatchSet(reg *Registry, metrics *Metrics) *WatchSet {
	return &WatchSet{
		registry: reg,
		metrics:  metrics,
		keys:     make(map[types.NamespacedName]struct{}),
	}
}

// Track adds an AgentDeploy to the set (idempotent). The controller calls it for
// every reconciled AgentDeploy — sampling continues even while scaling is
// disabled (the Autoscaler honors Scale.Disabled, the Sampler does not).
func (w *WatchSet) Track(k types.NamespacedName) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.keys[k] = struct{}{}
}

// Forget removes an AgentDeploy from the set and drops its in-memory history.
// The controller calls it on delete; the components call it as an in-band safety
// net when the AgentDeploy is already gone. The backing ConfigMap is left to
// owner-reference garbage collection.
func (w *WatchSet) Forget(k types.NamespacedName) {
	w.mu.Lock()
	delete(w.keys, k)
	w.mu.Unlock()
	w.registry.Forget(k)
	w.metrics.Delete(k)
}

// Contains reports whether the key is in the set.
func (w *WatchSet) Contains(k types.NamespacedName) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	_, ok := w.keys[k]
	return ok
}

// Len is the size of the set.
func (w *WatchSet) Len() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return len(w.keys)
}

// Snapshot returns a copy of the current keys for a component to iterate without
// holding the lock or racing concurrent Track/Forget.
func (w *WatchSet) Snapshot() []types.NamespacedName {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]types.NamespacedName, 0, len(w.keys))
	for k := range w.keys {
		out = append(out, k)
	}
	return out
}
