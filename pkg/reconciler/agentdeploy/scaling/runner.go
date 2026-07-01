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
	"k8s.io/apimachinery/pkg/types"
)

// processFunc handles one AgentDeploy per invocation.
type processFunc func(ctx context.Context, k types.NamespacedName) error

// runner is a paced round-robin worker pool over a WatchSet. It hands each
// watched AgentDeploy to process roughly once per taskInterval, spread
// continuously (one hand-out every taskInterval/N) rather than in a burst, with
// a bounded pool of workers and channel backpressure when process is slow.
//
// Both the Sampler and the Autoscaler are runners over the same WatchSet, each
// with its own cadence, pool size, and process function.
type runner struct {
	name         string
	watch        *WatchSet
	process      processFunc
	workers      int
	taskInterval time.Duration
	logger       *zap.SugaredLogger
}

// start runs the worker pool and the assigner loop until ctx is cancelled.
func (r *runner) start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	keyCh := make(chan types.NamespacedName)
	for i := 1; i <= r.workers; i++ {
		go r.worker(ctx, i, keyCh)
	}
	r.logger.Infow("Runner started",
		zap.String("runner", r.name), zap.Int("workers", r.workers),
		zap.Duration("taskInterval", r.taskInterval))

	// batch is a snapshot of the watch set that we round-robin through; it is
	// refreshed at each cycle boundary so membership changes are picked up.
	var batch []types.NamespacedName
	idx := 0
	for {
		if idx >= len(batch) {
			batch = r.watch.Snapshot()
			idx = 0
		}
		if len(batch) == 0 {
			if !sleep(ctx, r.taskInterval) {
				return nil
			}
			continue
		}
		k := batch[idx]
		idx++
		select {
		case keyCh <- k:
		case <-ctx.Done():
			return nil
		}
		// Pace so a full cycle takes ~taskInterval regardless of set size.
		if !sleep(ctx, r.taskInterval/time.Duration(len(batch))) {
			return nil
		}
	}
}

// worker consumes keys and processes each one; failures are logged, not fatal.
func (r *runner) worker(ctx context.Context, id int, keyCh <-chan types.NamespacedName) {
	for {
		select {
		case <-ctx.Done():
			return
		case k := <-keyCh:
			if err := r.process(ctx, k); err != nil {
				r.logger.Warnw("Process failed",
					zap.String("runner", r.name), zap.Int("worker", id),
					zap.String("agentDeploy", k.Name), zap.Error(err))
			}
		}
	}
}

// sleep waits for d or ctx cancellation, returning false if cancelled. A
// non-positive d is floored to 1ms so a large set never busy-loops.
func sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		d = time.Millisecond
	}
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}
