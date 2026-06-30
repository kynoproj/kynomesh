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
	"time"

	"go.uber.org/zap"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

const defaultScaleInterval = 30 * time.Second

// Autoscaler is the standalone scaling component. On a ticker it reads each
// AgentDeploy's in-memory history from the Registry, computes the desired
// replica count, and patches spec.replicas — leaving the agentdeploy controller
// to realize the change. Its Start method is registered as a leader-elected runner.
//
// It never writes status; Status.LastScaledAt (read here for cooldowns) is the
// agentdeploy controller's responsibility to stamp on an actual replica change.
type Autoscaler struct {
	client   client.Client
	registry *Registry
	logger   *zap.SugaredLogger
	interval time.Duration
	clock    func() time.Time
}

// AutoscalerOption configures an Autoscaler.
type AutoscalerOption func(*Autoscaler)

func WithScaleInterval(d time.Duration) AutoscalerOption {
	return func(a *Autoscaler) { a.interval = d }
}
func WithAutoscalerClock(f func() time.Time) AutoscalerOption {
	return func(a *Autoscaler) { a.clock = f }
}

// NewAutoscaler builds an Autoscaler reading from the shared Registry.
func NewAutoscaler(c client.Client, reg *Registry, logger *zap.SugaredLogger, opts ...AutoscalerOption) *Autoscaler {
	a := &Autoscaler{
		client:   c,
		registry: reg,
		logger:   logger,
		interval: defaultScaleInterval,
		clock:    time.Now,
	}
	for _, o := range opts {
		o(a)
	}
	return a
}

// Start runs the scaling loop until ctx is cancelled.
func (a *Autoscaler) Start(ctx context.Context) error {
	t := time.NewTicker(a.interval)
	defer t.Stop()
	a.logger.Infow("Autoscaler started", zap.Duration("interval", a.interval))
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			a.scaleOnce(ctx, a.clock())
		}
	}
}

// scaleOnce evaluates every scaling-enabled AgentDeploy once and patches
// spec.replicas where a change is warranted. Per-AgentDeploy failures are
// logged and skipped.
func (a *Autoscaler) scaleOnce(ctx context.Context, now time.Time) {
	var list kmv1.AgentDeployList
	if err := a.client.List(ctx, &list); err != nil {
		a.logger.Errorw("List AgentDeploys for scaling failed", zap.Error(err))
		return
	}

	for i := range list.Items {
		ad := &list.Items[i]
		if ad.Spec.Scale.Disabled {
			continue
		}
		store, ok := a.registry.Get(key(ad))
		if !ok {
			continue // not sampled yet
		}
		hist := store.History(now)
		if len(hist) == 0 {
			continue // no data; leave spec untouched
		}

		current := specReplicas(ad)
		dec := Decide(Inputs{
			SpecifiedReplicas: current,
			ReadyReplicas:     int32(ad.Status.ReadyReplicas),
			History:           hist,
			Current:           hist[len(hist)-1], // most recent sample
			Spec:              ad.Spec.Scale,
			Now:               now,
			LastScaledAt:      ad.Status.LastScaledAt.Time,
		})
		if dec.Skip || dec.DesiredReplicas == current {
			continue
		}
		if err := a.applyReplicas(ctx, ad, dec.DesiredReplicas); err != nil {
			a.logger.Warnw("Patch replicas failed", zap.String("agentDeploy", ad.Name), zap.Error(err))
			continue
		}
		a.logger.Infow("Scaled AgentDeploy",
			zap.String("agentDeploy", ad.Name),
			zap.Int32("from", current),
			zap.Int32("to", dec.DesiredReplicas),
			zap.String("reason", string(dec.Reason)))
	}
}

// applyReplicas patches spec.replicas, leaving everything else untouched.
func (a *Autoscaler) applyReplicas(ctx context.Context, ad *kmv1.AgentDeploy, desired int32) error {
	patch := client.MergeFrom(ad.DeepCopy())
	ad.Spec.Replicas = &desired
	return a.client.Patch(ctx, ad, patch)
}

// specReplicas is the AgentDeploy's current spec replica count, defaulting to 1.
func specReplicas(ad *kmv1.AgentDeploy) int32 {
	if ad.Spec.Replicas != nil {
		return *ad.Spec.Replicas
	}
	return 1
}
