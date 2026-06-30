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
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
	daemonclient "github.com/kynoproj/kynomesh/pkg/daemon/client"
)

const (
	defaultSampleInterval = 30 * time.Second
	defaultFlushInterval  = 5 * time.Minute
)

// DaemonDialer returns a metrics source for an AgentSet's daemon. Injected so
// tests can supply a fake without dialing.
type DaemonDialer func(namespace, agentSetName string) (MetricsSource, error)

// GRPCDaemonDialer dials the per-AgentSet daemon's gRPC API by its in-cluster
// Service DNS name.
func GRPCDaemonDialer(namespace, agentSetName string) (MetricsSource, error) {
	as := &kmv1.AgentSet{ObjectMeta: metav1.ObjectMeta{Name: agentSetName}}
	addr := fmt.Sprintf("%s.%s.svc:%d", as.DaemonName(), namespace, kmv1.DaemonAPIPort)
	return daemonclient.NewGRPCClient(addr)
}

// Sampler is the standalone sample-collection component. On a ticker it lists
// AgentDeploys, scrapes each one's per-AgentSet daemon, and records per-replica
// Samples into the shared Registry, flushing each Store to its ConfigMap on a
// slower cadence. Its Start method is registered as a leader-elected runner.
type Sampler struct {
	client         client.Client
	registry       *Registry
	dial           DaemonDialer
	logger         *zap.SugaredLogger
	sampleInterval time.Duration
	flushInterval  time.Duration
	clock          func() time.Time

	mu        sync.Mutex
	sources   map[string]MetricsSource // keyed by namespace/agentset
	lastFlush map[types.NamespacedName]time.Time
}

// SamplerOption configures a Sampler.
type SamplerOption func(*Sampler)

func WithSampleInterval(d time.Duration) SamplerOption {
	return func(s *Sampler) { s.sampleInterval = d }
}
func WithFlushInterval(d time.Duration) SamplerOption {
	return func(s *Sampler) { s.flushInterval = d }
}
func WithSamplerClock(f func() time.Time) SamplerOption { return func(s *Sampler) { s.clock = f } }

// NewSampler builds a Sampler. dial defaults to GRPCDaemonDialer when nil.
func NewSampler(c client.Client, reg *Registry, dial DaemonDialer, logger *zap.SugaredLogger, opts ...SamplerOption) *Sampler {
	if dial == nil {
		dial = GRPCDaemonDialer
	}
	s := &Sampler{
		client:         c,
		registry:       reg,
		dial:           dial,
		logger:         logger,
		sampleInterval: defaultSampleInterval,
		flushInterval:  defaultFlushInterval,
		clock:          time.Now,
		sources:        make(map[string]MetricsSource),
		lastFlush:      make(map[types.NamespacedName]time.Time),
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Start runs the sampling loop until ctx is cancelled.
func (s *Sampler) Start(ctx context.Context) error {
	t := time.NewTicker(s.sampleInterval)
	defer t.Stop()
	s.logger.Infow("Sampler started", zap.Duration("interval", s.sampleInterval))
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			s.sampleOnce(ctx, s.clock())
		}
	}
}

// sampleOnce scrapes every scaling-enabled AgentDeploy once and records the
// result. Per-AgentDeploy failures are logged and skipped, never fatal.
func (s *Sampler) sampleOnce(ctx context.Context, now time.Time) {
	var list kmv1.AgentDeployList
	if err := s.client.List(ctx, &list); err != nil {
		s.logger.Errorw("List AgentDeploys for sampling failed", zap.Error(err))
		return
	}

	live := make(map[types.NamespacedName]bool, len(list.Items))
	for i := range list.Items {
		ad := &list.Items[i]
		if ad.Spec.Scale.Disabled {
			continue
		}
		k := key(ad)
		live[k] = true

		src, err := s.sourceFor(ad)
		if err != nil {
			s.logger.Warnw("Dial daemon failed", zap.String("agentDeploy", ad.Name), zap.Error(err))
			continue
		}
		store, err := s.registry.StoreFor(ctx, ad)
		if err != nil {
			s.logger.Warnw("Load history failed", zap.String("agentDeploy", ad.Name), zap.Error(err))
		}
		sample, ok, err := collectSample(ctx, src, ad, now)
		if err != nil {
			s.logger.Warnw("Collect sample failed", zap.String("agentDeploy", ad.Name), zap.Error(err))
			continue
		}
		if !ok {
			continue
		}
		store.Record(sample)
		s.maybeFlush(ctx, k, store, now)
	}
	s.reap(live)
}

// sourceFor returns a (cached) metrics source for the AgentDeploy's AgentSet
// daemon — one client per AgentSet, shared across its AgentDeploys.
func (s *Sampler) sourceFor(ad *kmv1.AgentDeploy) (MetricsSource, error) {
	ck := ad.Namespace + "/" + ad.Spec.AgentSetName
	s.mu.Lock()
	defer s.mu.Unlock()
	if src, ok := s.sources[ck]; ok {
		return src, nil
	}
	src, err := s.dial(ad.Namespace, ad.Spec.AgentSetName)
	if err != nil {
		return nil, err
	}
	s.sources[ck] = src
	return src, nil
}

// maybeFlush persists the store to its ConfigMap if the flush interval has
// elapsed since the last flush for this AgentDeploy.
func (s *Sampler) maybeFlush(ctx context.Context, k types.NamespacedName, store *ConfigMapStore, now time.Time) {
	s.mu.Lock()
	last := s.lastFlush[k]
	due := now.Sub(last) >= s.flushInterval
	if due {
		s.lastFlush[k] = now
	}
	s.mu.Unlock()
	if !due {
		return
	}
	if err := store.Flush(ctx); err != nil {
		s.logger.Warnw("Flush history failed", zap.String("agentDeploy", k.Name), zap.Error(err))
	}
}

// reap forgets in-memory state for AgentDeploys no longer present.
func (s *Sampler) reap(live map[types.NamespacedName]bool) {
	for _, k := range s.registry.Keys() {
		if live[k] {
			continue
		}
		s.registry.Forget(k)
		s.mu.Lock()
		delete(s.lastFlush, k)
		s.mu.Unlock()
	}
}
