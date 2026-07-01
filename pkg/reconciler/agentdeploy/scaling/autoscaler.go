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
	if ad.Spec.Scale.Disabled {
		return nil
	}
	store, ok := a.registry.Get(k)
	if !ok {
		return nil // not sampled yet
	}

	now := a.clock()
	hist := store.History(now)
	if len(hist) == 0 {
		return nil // no data; leave spec untouched
	}

	latest := hist[len(hist)-1]
	// Staleness guard: if the freshest sample is too old (the Sampler stalled or
	// the daemon is unreachable), don't scale on outdated load.
	if age := now.Sub(latest.Timestamp); age > a.maxSampleAge {
		a.logger.Debugw("Skipping scale: stale metrics",
			zap.String("agentDeploy", ad.Name), zap.Duration("sampleAge", age))
		return nil
	}

	current := currentReplicas(&ad)
	dec := Decide(Inputs{
		SpecifiedReplicas: current,
		ReadyReplicas:     int32(ad.Status.ReadyReplicas),
		History:           hist,
		Current:           latest,
		Spec:              ad.Spec.Scale,
		Now:               now,
		LastScaledAt:      ad.Status.LastScaledAt.Time,
	})
	a.logger.Debugw("Scaling decision",
		zap.String("agentDeploy", ad.Name),
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
	a.logger.Infow("Scaled AgentDeploy",
		zap.String("agentDeploy", ad.Name),
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
	if ad.Status.Replicas > 0 {
		return int32(ad.Status.Replicas)
	}
	return specReplicas(ad)
}

// specReplicas is the AgentDeploy's spec replica count, defaulting to 1.
func specReplicas(ad *kmv1.AgentDeploy) int32 {
	if ad.Spec.Replicas != nil {
		return *ad.Spec.Replicas
	}
	return 1
}
