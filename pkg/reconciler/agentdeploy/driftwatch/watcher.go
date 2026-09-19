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

// Package driftwatch polls each drift-reload-enabled AgentDeploy's peer
// AgentCard drift state (reported by the per-AgentSet daemon, #214) and
// deletes exactly the pods still serving a stale cached peer AgentCard, so
// they restart and re-resolve. See
// docs/development/specifications/agentcard-drift-reload.md.
package driftwatch

import (
	"context"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
	pb "github.com/kynoproj/kynomesh/pkg/apis/proto/daemon"
	"github.com/kynoproj/kynomesh/pkg/reconciler/agentdeploy"
	"github.com/kynoproj/kynomesh/pkg/shared/workset"
)

const (
	defaultWorkers      = 16
	defaultTaskInterval = 60 * time.Second
	// defaultScrapeTimeout caps a single daemon call so one hung daemon can't
	// tie up a worker indefinitely.
	defaultScrapeTimeout = 10 * time.Second
	// defaultReapInterval is how often cached per-AgentSet daemon clients no
	// longer referenced by any AgentDeploy are closed and evicted.
	defaultReapInterval = 10 * time.Minute
)

// Watcher polls each tracked AgentDeploy's peer drift state and deletes the
// stale pods so they restart and re-resolve their peer client. It runs its
// own WorkSet, mirroring sampling.Sampler's shape.
type Watcher struct {
	client        client.Client
	watch         *workset.WorkSet[types.NamespacedName]
	dial          DaemonDialer
	logger        *zap.SugaredLogger
	workers       int
	taskInterval  time.Duration
	scrapeTimeout time.Duration
	reapInterval  time.Duration

	mu      sync.Mutex
	sources map[string]DriftSource // keyed by namespace/agentset
}

// WatcherOption configures a Watcher.
type WatcherOption func(*Watcher)

func WithWorkers(n int) WatcherOption                { return func(w *Watcher) { w.workers = n } }
func WithTaskInterval(d time.Duration) WatcherOption { return func(w *Watcher) { w.taskInterval = d } }
func WithScrapeTimeout(d time.Duration) WatcherOption {
	return func(w *Watcher) { w.scrapeTimeout = d }
}
func WithReapInterval(d time.Duration) WatcherOption {
	return func(w *Watcher) { w.reapInterval = d }
}

// NewWatcher builds a Watcher with its own WorkSet. dial defaults to
// GRPCDaemonDialer when nil.
func NewWatcher(c client.Client, dial DaemonDialer, logger *zap.SugaredLogger, opts ...WatcherOption) *Watcher {
	if dial == nil {
		dial = GRPCDaemonDialer
	}
	w := &Watcher{
		client:        c,
		dial:          dial,
		logger:        logger,
		workers:       defaultWorkers,
		taskInterval:  defaultTaskInterval,
		scrapeTimeout: defaultScrapeTimeout,
		reapInterval:  defaultReapInterval,
		sources:       make(map[string]DriftSource),
	}
	for _, o := range opts {
		o(w)
	}
	w.watch = workset.NewWorkSet("driftwatch", w.reconcileDrift,
		workset.WithWorkers[types.NamespacedName](w.workers),
		workset.WithTaskInterval[types.NamespacedName](w.taskInterval),
		workset.WithLogger[types.NamespacedName](logger),
	)
	return w
}

// Track adds an AgentDeploy to the watcher's WorkSet.
func (w *Watcher) Track(k types.NamespacedName) { w.watch.Track(k) }

// Forget removes an AgentDeploy from the watcher's WorkSet.
func (w *Watcher) Forget(k types.NamespacedName) { w.watch.Forget(k) }

// Start runs the drift-watching WorkSet.
func (w *Watcher) Start(ctx context.Context) error {
	go w.runReaper(ctx)
	err := w.watch.Start(ctx)
	w.closeAllSources()
	return err
}

// runReaper periodically closes cached daemon clients no longer referenced by
// any AgentDeploy.
func (w *Watcher) runReaper(ctx context.Context) {
	t := time.NewTicker(w.reapInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.reapSources(ctx)
		}
	}
}

// reapSources closes and evicts any cached per-AgentSet daemon client whose
// AgentSet is no longer referenced by a live AgentDeploy.
func (w *Watcher) reapSources(ctx context.Context) {
	var list kmv1.AgentDeployList
	if err := w.client.List(ctx, &list); err != nil {
		w.logger.Errorw("List AgentDeploys for source reaping failed", zap.Error(err))
		return
	}
	live := make(map[string]bool, len(list.Items))
	for i := range list.Items {
		ad := &list.Items[i]
		live[ad.Namespace+"/"+ad.Spec.AgentSetName] = true
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	for ck, src := range w.sources {
		if live[ck] {
			continue
		}
		closeSource(ck, src, w.logger)
		delete(w.sources, ck)
	}
}

// closeAllSources closes and clears every cached daemon client (shutdown).
func (w *Watcher) closeAllSources() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for ck, src := range w.sources {
		closeSource(ck, src, w.logger)
		delete(w.sources, ck)
	}
}

// closeSource closes a daemon client if it is closeable, logging any error.
func closeSource(key string, src DriftSource, logger *zap.SugaredLogger) {
	c, ok := src.(io.Closer)
	if !ok {
		return
	}
	if err := c.Close(); err != nil {
		logger.Warnw("Close daemon client failed", zap.String("agentSet", key), zap.Error(err))
	}
}

// sourceFor returns a (cached) drift source for the AgentDeploy's AgentSet
// daemon — one client per AgentSet, shared across its AgentDeploys.
func (w *Watcher) sourceFor(ad *kmv1.AgentDeploy) (DriftSource, error) {
	ck := ad.Namespace + "/" + ad.Spec.AgentSetName
	w.mu.Lock()
	defer w.mu.Unlock()
	if src, ok := w.sources[ck]; ok {
		return src, nil
	}
	src, err := w.dial(ad.Namespace, ad.Spec.AgentSetName)
	if err != nil {
		return nil, err
	}
	w.sources[ck] = src
	return src, nil
}

// reconcileDrift evaluates one AgentDeploy's peer drift state and deletes
// exactly the pods reporting a stale peer AgentCard hash, so they restart
// and re-resolve.
func (w *Watcher) reconcileDrift(ctx context.Context, k types.NamespacedName) error {
	var ad kmv1.AgentDeploy
	if err := w.client.Get(ctx, k, &ad); err != nil {
		if apierrors.IsNotFound(err) {
			w.Forget(k)
			return nil
		}
		return fmt.Errorf("get agentdeploy: %w", err)
	}
	log := w.logger.With(zap.String("namespace", ad.Namespace),
		zap.String("agentSet", ad.Spec.AgentSetName),
		zap.String("agentDeploy", ad.Spec.Name))
	if !ad.DeletionTimestamp.IsZero() {
		w.Forget(k)
		log.Debug("AgentDeploy being deleted")
		return nil
	}
	if !ad.Spec.DriftReload.IsEnabled() {
		w.Forget(k)
		return nil
	}
	// Defer to an active rollout.
	if ad.Status.UpdateHash != ad.Status.CurrentHash && ad.Status.UpdateHash != "" {
		return nil
	}

	src, err := w.sourceFor(&ad)
	if err != nil {
		return fmt.Errorf("dial daemon: %w", err)
	}
	scrapeCtx, cancel := context.WithTimeout(ctx, w.scrapeTimeout)
	defer cancel()
	drift, err := src.GetPeerCardDrift(scrapeCtx, ad.Spec.Name)
	if err != nil {
		return fmt.Errorf("get peer card drift: %w", err)
	}

	stale := stalePodNames(drift)
	if len(stale) == 0 {
		return nil
	}

	pods, err := agentdeploy.ListOwnedPods(ctx, w.client, &ad)
	if err != nil {
		return fmt.Errorf("list owned pods: %w", err)
	}
	desired := agentdeploy.DesiredReplicas(&ad)
	maxUnavailable := agentdeploy.ResolveMaxUnavailable(&ad, desired)

	toDelete := make([]*corev1.Pod, 0, len(stale))
	for _, p := range pods {
		if !p.DeletionTimestamp.IsZero() {
			continue
		}
		if _, ok := stale[p.Name]; ok {
			toDelete = append(toDelete, p)
		}
	}
	sort.Slice(toDelete, func(i, j int) bool { return toDelete[i].Name < toDelete[j].Name })
	if len(toDelete) > maxUnavailable {
		toDelete = toDelete[:maxUnavailable]
	}

	for _, p := range toDelete {
		if err := w.client.Delete(ctx, p); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete stale pod %s: %w", p.Name, err)
		}
		log.Infow("Deleted pod serving stale peer AgentCard", zap.String("podName", p.Name))
	}
	return nil
}

// stalePodNames returns the union, across all peers, of pod names whose
// reported AgentCard hash for that peer no longer matches the daemon's
// latest stable hash. A peer with no stabilized LatestHash yet is skipped
// (treated as unknown, not stale). A pod absent from a peer's
// ReportedHashes is likewise unknown for that peer, not stale.
func stalePodNames(drift map[string]*pb.PeerCardDrift) map[string]struct{} {
	stale := map[string]struct{}{}
	for _, peerDrift := range drift {
		if peerDrift.GetLatestHash() == "" {
			continue
		}
		for podName, rh := range peerDrift.GetReportedHashes() {
			if rh.GetHash() != peerDrift.GetLatestHash() {
				stale[podName] = struct{}{}
			}
		}
	}
	return stale
}
