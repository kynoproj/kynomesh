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
	"time"

	"go.uber.org/zap"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

const (
	defaultScaleInterval = 30 * time.Second
	// defaultMaxSampleAge bounds how stale the freshest sample may be before the
	// Autoscaler declines to act — guards against scaling on outdated load when
	// the Sampler has stalled or the daemon is unreachable.
	defaultMaxSampleAge = 2 * time.Minute
)

// Autoscaler computes desired replicas and patches spec.replicas, leaving the
// agentdeploy controller to realize the change.
type Autoscaler struct {
	client       client.Client
	watch        *WatchSet
	registry     *Registry
	logger       *zap.SugaredLogger
	workers      int
	taskInterval time.Duration
	maxSampleAge time.Duration
	clock        func() time.Time
	metrics      *Metrics

	runner *runner
}

// AutoscalerOption configures an Autoscaler.
type AutoscalerOption func(*Autoscaler)

func WithScaleWorkers(n int) AutoscalerOption { return func(a *Autoscaler) { a.workers = n } }
func WithScaleInterval(d time.Duration) AutoscalerOption {
	return func(a *Autoscaler) { a.taskInterval = d }
}
func WithMaxSampleAge(d time.Duration) AutoscalerOption {
	return func(a *Autoscaler) { a.maxSampleAge = d }
}
func WithAutoscalerMetrics(m *Metrics) AutoscalerOption {
	return func(a *Autoscaler) { a.metrics = m }
}
func WithAutoscalerClock(f func() time.Time) AutoscalerOption {
	return func(a *Autoscaler) { a.clock = f }
}

// NewAutoscaler builds an Autoscaler over the shared WatchSet and Registry.
func NewAutoscaler(c client.Client, watch *WatchSet, reg *Registry, logger *zap.SugaredLogger, opts ...AutoscalerOption) *Autoscaler {
	a := &Autoscaler{
		client:       c,
		watch:        watch,
		registry:     reg,
		logger:       logger,
		workers:      defaultWorkers,
		taskInterval: defaultScaleInterval,
		maxSampleAge: defaultMaxSampleAge,
		clock:        time.Now,
	}
	for _, o := range opts {
		o(a)
	}
	a.runner = &runner{
		name:         "autoscaler",
		watch:        watch,
		process:      a.scaleKey,
		workers:      a.workers,
		taskInterval: a.taskInterval,
		logger:       logger,
	}
	return a
}

// Start runs the scaling runner until ctx is cancelled.
func (a *Autoscaler) Start(ctx context.Context) error { return a.runner.start(ctx) }

// scaleKey evaluates one AgentDeploy and patches spec.replicas when a change is
// warranted.
func (a *Autoscaler) scaleKey(ctx context.Context, k types.NamespacedName) error {
	var ad kmv1.AgentDeploy
	if err := a.client.Get(ctx, k, &ad); err != nil {
		if apierrors.IsNotFound(err) {
			a.watch.Forget(k)
			return nil
		}
		return fmt.Errorf("get agentdeploy: %w", err)
	}
	log := a.logger.With(zap.String("namespace", ad.Namespace),
		zap.String("agentSet", ad.Spec.AgentSetName),
		zap.String("agentDeploy", ad.Spec.Name))
	if !ad.DeletionTimestamp.IsZero() {
		a.watch.Forget(k)
		log.Debug("AgentDeploy being deleted")
		return nil
	}
	if ad.Spec.Scale.Disabled {
		return nil
	}
	if ad.Status.Phase != kmv1.AgentDeployPhaseRunning {
		log.Infow("Skipping scale: AgentDeploy not in Running phase")
		return nil
	}
	if ad.Status.UpdateHash != ad.Status.CurrentHash && ad.Status.UpdateHash != "" {
		log.Info("Skipping scale: AgentDeploy is updating")
		return nil
	}
	if ad.Status.Replicas != ad.Status.DesiredReplicas {
		log.Infow("Skipping scale: reconcile in flight, replicas mismatch",
			zap.Uint32("currentReplicas", ad.Status.Replicas),
			zap.Uint32("desiredReplicas", ad.Status.DesiredReplicas))
		return nil
	}
	if ad.Status.ReadyReplicas == 0 {
		log.Infow("Skipping scale: no ready replicas")
		return nil
	}
	secondsSinceLastScale := time.Since(ad.Status.LastScaledAt.Time).Seconds()
	scaleDownCooldown := float64(getOr(ad.Spec.Scale.ScaleDownCooldownSeconds, defaultScaleDownCooldownSec))
	scaleUpCooldown := float64(getOr(ad.Spec.Scale.ScaleUpCooldownSeconds, defaultScaleUpCooldownSec))
	if secondsSinceLastScale < scaleDownCooldown && secondsSinceLastScale < scaleUpCooldown {
		// Skip scaling without needing further calculation
		log.Infow("Skipping scale: Cooldown period")
		return nil
	}

	store, ok := a.registry.Get(k)
	if !ok {
		log.Infow("Skipping scale: no sampled metrics yet")
		return nil
	}

	now := a.clock()
	hist := store.History(now)
	if len(hist) == 0 {
		log.Infow("Skipping scale: no historic metrics")
		return nil // no data; leave spec untouched
	}

	latest := hist[len(hist)-1]
	// Staleness guard: if the freshest sample is too old.
	if age := now.Sub(latest.Timestamp); age > a.maxSampleAge {
		log.Infow("Skipping scale: stale metrics", zap.Duration("sampleAge", age))
		return nil
	}

	current := currentReplicas(&ad)
	dec := Decide(Inputs{
		CurrentReplicas: current,
		ReadyReplicas:   int32(ad.Status.ReadyReplicas),
		History:         hist,
		Current:         latest,
		Spec:            ad.Spec.Scale,
		MaxInFlight:     maxInFlightOf(&ad),
		Now:             now,
		LastScaledAt:    ad.Status.LastScaledAt.Time,
	})
	a.metrics.ObserveDecision(&ad, dec.Estimate, current, dec.DesiredReplicas)
	log.Debugw("Scaling decision",
		zap.Int32("current", current),
		zap.Int32("desired", dec.DesiredReplicas),
		zap.String("reason", string(dec.Reason)),
		zap.Bool("skip", dec.Skip),
		zap.Float64("kneePerReplica", dec.Estimate.KneePerReplica),
		zap.Float64("confidence", dec.Estimate.Confidence),
		zap.Bool("lowerBound", dec.Estimate.IsLowerBound))
	if dec.Skip || dec.DesiredReplicas == current {
		return nil
	}
	if err := a.applyReplicas(ctx, &ad, dec.DesiredReplicas); err != nil {
		return fmt.Errorf("patch replicas: %w", err)
	}
	a.metrics.RecordScale(&ad, dec.DesiredReplicas > current)
	log.Infow("Scaled AgentDeploy",
		zap.Int32("from", current),
		zap.Int32("to", dec.DesiredReplicas),
		zap.String("reason", string(dec.Reason)),
		zap.Float64("kneePerReplica", dec.Estimate.KneePerReplica),
		zap.Float64("confidence", dec.Estimate.Confidence))
	return nil
}

// applyReplicas patches spec.replicas, leaving everything else untouched.
func (a *Autoscaler) applyReplicas(ctx context.Context, ad *kmv1.AgentDeploy, desired int32) error {
	patch := client.MergeFrom(ad.DeepCopy())
	ad.Spec.Replicas = &desired
	return a.client.Patch(ctx, ad, patch)
}

// currentReplicas is the running replica count the decision scales from.
func currentReplicas(ad *kmv1.AgentDeploy) int32 {
	return int32(ad.Status.Replicas)
}

// maxInFlightOf returns the fleet-wide rate-limit cap from the spec, or 0 when
// unset (no cap).
func maxInFlightOf(ad *kmv1.AgentDeploy) int32 {
	if ad.Spec.RateLimit == nil || ad.Spec.RateLimit.MaxInFlight == nil {
		return 0
	}
	return *ad.Spec.RateLimit.MaxInFlight
}
