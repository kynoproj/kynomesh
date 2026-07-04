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
	"io"
	"sort"
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
	// defaultReapInterval is how often cached per-AgentSet daemon clients no
	// longer referenced by any AgentDeploy are closed and evicted.
	defaultReapInterval = 10 * time.Minute

	// lookbackFactor sizes the adaptive averaging window as a multiple of the
	// observed request duration (D = inflight/rate), so the rate is averaged over
	// several request completions and short-lived noise is smoothed out.
	lookbackFactor = 5
	// minAdaptiveLookback / maxAdaptiveLookback clamp the adaptive window. The
	// upper bound stays well under the daemon rater's retention so the custom
	// window is always computable.
	minAdaptiveLookback = 30 * time.Second
	maxAdaptiveLookback = 15 * time.Minute
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
	reapInterval  time.Duration
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
func WithReapInterval(d time.Duration) SamplerOption {
	return func(s *Sampler) { s.reapInterval = d }
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
		reapInterval:  defaultReapInterval,
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

// Start runs the sampling runner.
func (s *Sampler) Start(ctx context.Context) error {
	go s.runReaper(ctx)
	err := s.runner.start(ctx)
	s.flushAll()
	s.closeAllSources()
	return err
}

// runReaper periodically closes cached daemon clients no longer referenced by
// any AgentDeploy.
func (s *Sampler) runReaper(ctx context.Context) {
	t := time.NewTicker(s.reapInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.reapSources(ctx)
		}
	}
}

// reapSources closes and evicts any cached per-AgentSet daemon client whose
// AgentSet is no longer referenced by a live AgentDeploy.
func (s *Sampler) reapSources(ctx context.Context) {
	var list kmv1.AgentDeployList
	// Cheap cache read. It also works for namespace scoped installation,
	// which only caches the namespace scoped objects.
	if err := s.client.List(ctx, &list); err != nil {
		s.logger.Errorw("List AgentDeploys for source reaping failed", zap.Error(err))
		return
	}
	live := make(map[string]bool, len(list.Items))
	for i := range list.Items {
		ad := &list.Items[i]
		live[ad.Namespace+"/"+ad.Spec.AgentSetName] = true
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for ck, src := range s.sources {
		if live[ck] {
			continue
		}
		closeSource(ck, src, s.logger)
		delete(s.sources, ck)
	}
}

// closeAllSources closes and clears every cached daemon client (shutdown).
func (s *Sampler) closeAllSources() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ck, src := range s.sources {
		closeSource(ck, src, s.logger)
		delete(s.sources, ck)
	}
}

// closeSource closes a daemon client if it is closeable, logging any error.
func closeSource(key string, src MetricsSource, logger *zap.SugaredLogger) {
	c, ok := src.(io.Closer)
	if !ok {
		return
	}
	if err := c.Close(); err != nil {
		logger.Warnw("Close daemon client failed", zap.String("agentSet", key), zap.Error(err))
	}
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

	lookback := lookbackSeconds(&ad, store, s.clock())
	scrapeCtx, cancel := context.WithTimeout(ctx, s.scrapeTimeout)
	defer cancel()
	sample, ok, err := collectSample(scrapeCtx, src, &ad, s.clock(), lookback)
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

// lookbackSeconds is the averaging window for the daemon query. When the
// operator has pinned Scale.LookbackSeconds it wins; otherwise the window is
// sized adaptively from the observed request duration (D = inflight/rate) so
// slow workloads average over a longer window than fast ones. Before any
// duration can be derived (cold start) it returns 0, letting the daemon use its
// built-in 1m window.
func lookbackSeconds(ad *kmv1.AgentDeploy, store *ConfigMapStore, now time.Time) int64 {
	if v := getOr(ad.Spec.Scale.LookbackSeconds, 0); v > 0 {
		return int64(v)
	}
	d := medianRequestDuration(historyOf(store, now))
	if d <= 0 {
		return 0
	}
	lb := clampDuration(time.Duration(lookbackFactor)*d, minAdaptiveLookback, maxAdaptiveLookback)
	return int64(lb.Seconds())
}

// historyOf returns the store's recorded samples, tolerating a nil store.
func historyOf(store *ConfigMapStore, now time.Time) []Sample {
	if store == nil {
		return nil
	}
	return store.History(now)
}

// medianRequestDuration derives the typical per-request service time from
// history via Little's Law (D = inflight/rate), using the median to shrug off
// outliers. Returns 0 when no sample carries a usable rate.
func medianRequestDuration(history []Sample) time.Duration {
	ds := make([]float64, 0, len(history))
	for _, s := range history {
		if s.RatePerRep > 0 && s.InflightPerRep > 0 {
			ds = append(ds, s.InflightPerRep/s.RatePerRep) // seconds
		}
	}
	if len(ds) == 0 {
		return 0
	}
	sort.Float64s(ds)
	mid := len(ds) / 2
	med := ds[mid]
	if len(ds)%2 == 0 {
		med = (ds[mid-1] + ds[mid]) / 2
	}
	return time.Duration(med * float64(time.Second))
}

func clampDuration(v, lo, hi time.Duration) time.Duration {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
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
