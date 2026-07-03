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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
	daemonclient "github.com/kynoproj/kynomesh/pkg/daemon/client"
)

const (
	defaultWorkers = 16
	// defaultTaskInterval is the target revisit cadence per AgentDeploy.
	defaultTaskInterval  = 30 * time.Second
	defaultFlushInterval = 5 * time.Minute
	// defaultScrapeTimeout caps a single daemon scrape so one hung daemon can't
	// tie up a worker indefinitely.
	defaultScrapeTimeout = 10 * time.Second
	// shutdownFlushTimeout bounds the best-effort flush of all stores when the
	// Sampler stops (leader loss / shutdown).
	shutdownFlushTimeout = 15 * time.Second
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

// Sampler collects per-replica load. It is a runner over the shared WatchSet:
// each watched AgentDeploy's per-AgentSet daemon is scraped roughly once per
// task interval and the result recorded into the Registry, with the store
// flushed to its ConfigMap on a slower cadence. Its Start method is registered
// as a leader-elected runner.
type Sampler struct {
	client        client.Client
	watch         *WatchSet
	registry      *Registry
	dial          DaemonDialer
	logger        *zap.SugaredLogger
	workers       int
	taskInterval  time.Duration
	flushInterval time.Duration
	scrapeTimeout time.Duration
	clock         func() time.Time
	metrics       *Metrics

	mu      sync.Mutex
	sources map[string]MetricsSource // keyed by namespace/agentset

	runner *runner
}

// SamplerOption configures a Sampler.
type SamplerOption func(*Sampler)

func WithWorkers(n int) SamplerOption { return func(s *Sampler) { s.workers = n } }
func WithTaskInterval(d time.Duration) SamplerOption {
	return func(s *Sampler) { s.taskInterval = d }
}
func WithFlushInterval(d time.Duration) SamplerOption {
	return func(s *Sampler) { s.flushInterval = d }
}
func WithScrapeTimeout(d time.Duration) SamplerOption {
	return func(s *Sampler) { s.scrapeTimeout = d }
}
func WithSamplerMetrics(m *Metrics) SamplerOption       { return func(s *Sampler) { s.metrics = m } }
func WithSamplerClock(f func() time.Time) SamplerOption { return func(s *Sampler) { s.clock = f } }

// NewSampler builds a Sampler over the shared WatchSet. dial defaults to
// GRPCDaemonDialer when nil.
func NewSampler(c client.Client, watch *WatchSet, reg *Registry, dial DaemonDialer, logger *zap.SugaredLogger, opts ...SamplerOption) *Sampler {
	if dial == nil {
		dial = GRPCDaemonDialer
	}
	s := &Sampler{
		client:        c,
		watch:         watch,
		registry:      reg,
		dial:          dial,
		logger:        logger,
		workers:       defaultWorkers,
		taskInterval:  defaultTaskInterval,
		flushInterval: defaultFlushInterval,
		scrapeTimeout: defaultScrapeTimeout,
		clock:         time.Now,
		sources:       make(map[string]MetricsSource),
	}
	for _, o := range opts {
		o(s)
	}
	s.runner = &runner{
		name:         "sampler",
		watch:        watch,
		process:      s.sampleKey,
		workers:      s.workers,
		taskInterval: s.taskInterval,
		logger:       logger,
	}
	return s
}

// Start runs the sampling runner until ctx is cancelled, then flushes all
// in-memory history to its backing ConfigMaps so a leader change or shutdown
// doesn't lose the samples collected since the last periodic flush.
func (s *Sampler) Start(ctx context.Context) error {
	err := s.runner.start(ctx)
	s.flushAll()
	return err
}

// flushAll persists every tracked store on a fresh, bounded context.
// Best-effort: failures (and anything not reached before the deadline) are
// logged, not returned — the periodic flush + reload covers the small residual.
func (s *Sampler) flushAll() {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownFlushTimeout)
	defer cancel()

	sem := make(chan struct{}, s.workers)
	var wg sync.WaitGroup
	for _, k := range s.registry.Keys() {
		store, ok := s.registry.Get(k)
		if !ok {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(k types.NamespacedName, store *ConfigMapStore) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := store.Flush(ctx); err != nil {
				s.logger.Warnw("Flush on shutdown failed",
					zap.String("namespacedName", k.String()))
			}
		}(k, store)
	}
	wg.Wait()
}

// sampleKey scrapes one AgentDeploy and records the sample, regardless of
// whether scaling is enabled (history is kept warm; the Autoscaler honors
// Scale.Disabled). A deleted AgentDeploy is forgotten as an in-band safety net.
func (s *Sampler) sampleKey(ctx context.Context, k types.NamespacedName) error {
	var ad kmv1.AgentDeploy
	if err := s.client.Get(ctx, k, &ad); err != nil {
		if apierrors.IsNotFound(err) {
			s.watch.Forget(k)
			return nil
		}
		return fmt.Errorf("get agentdeploy: %w", err)
	}
	log := s.logger.With(zap.String("namespace", ad.Namespace),
		zap.String("agentSet", ad.Spec.AgentSetName),
		zap.String("agentDeploy", ad.Spec.Name))
	src, err := s.sourceFor(&ad)
	if err != nil {
		return fmt.Errorf("dial daemon: %w", err)
	}
	store, err := s.registry.StoreFor(ctx, &ad)
	if err != nil {
		log.Warnw("Load history failed", zap.Error(err))
	}

	scrapeCtx, cancel := context.WithTimeout(ctx, s.scrapeTimeout)
	defer cancel()
	sample, ok, err := collectSample(scrapeCtx, src, &ad, s.clock())
	if err != nil {
		return fmt.Errorf("collect: %w", err)
	}
	if !ok {
		return nil
	}
	// Tag the sample with the current pod-spec hash so a new deployment resets
	// the learned history.
	store.Record(sample, ad.Status.UpdateHash)
	s.metrics.RecordSample(&ad)
	log.Debugw("Recorded sample",
		zap.Float64("inflightPerReplica", sample.InflightPerRep),
		zap.Float64("ratePerReplica", sample.RatePerRep),
		zap.Int32("replicas", sample.Replicas))
	if err := store.FlushIfDue(ctx, s.clock(), s.flushInterval); err != nil {
		log.Warnw("Flush history failed", zap.Error(err))
	}
	return nil
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
