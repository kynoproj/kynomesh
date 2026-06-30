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
	"context"
	"sync"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

// Registry is the shared in-memory home for per-AgentDeploy load history: a map
// of NamespacedName → Store. The Sampler writes into it; the Autoscaler reads
// from it. Safe for concurrent use.
type Registry struct {
	client client.Client
	opts   []bufferOption

	mu     sync.Mutex
	stores map[types.NamespacedName]*ConfigMapStore
}

// NewRegistry returns an empty Registry. opts are passed to every Store it
// creates (e.g. buffer bounds for tests).
func NewRegistry(c client.Client, opts ...bufferOption) *Registry {
	return &Registry{
		client: c,
		opts:   opts,
		stores: make(map[types.NamespacedName]*ConfigMapStore),
	}
}

func key(ad *kmv1.AgentDeploy) types.NamespacedName {
	return types.NamespacedName{Namespace: ad.Namespace, Name: ad.Name}
}

// StoreFor returns the Store for ad, creating and hydrating (Load) it from its
// backing ConfigMap on first sight.
func (r *Registry) StoreFor(ctx context.Context, ad *kmv1.AgentDeploy) (*ConfigMapStore, error) {
	k := key(ad)

	r.mu.Lock()
	if s, ok := r.stores[k]; ok {
		r.mu.Unlock()
		return s, nil
	}
	s := NewConfigMapStore(r.client, ad, r.opts...)
	r.stores[k] = s
	r.mu.Unlock()

	// Hydrate outside the lock; a missing ConfigMap is not an error.
	if err := s.Load(ctx); err != nil {
		return s, err
	}
	return s, nil
}

// Get returns the Store for the key if one exists.
func (r *Registry) Get(k types.NamespacedName) (*ConfigMapStore, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.stores[k]
	return s, ok
}

// Forget drops the in-memory Store for a deleted AgentDeploy. The backing
// ConfigMap is garbage-collected via its owner reference.
func (r *Registry) Forget(k types.NamespacedName) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.stores, k)
}

// Keys returns the currently tracked AgentDeploy keys.
func (r *Registry) Keys() []types.NamespacedName {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]types.NamespacedName, 0, len(r.stores))
	for k := range r.stores {
		out = append(out, k)
	}
	return out
}
