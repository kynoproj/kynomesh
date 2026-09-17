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

package workset

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
)

// ProcessFunc handles one AgentDeploy per invocation.
type ProcessFunc[T comparable] func(ctx context.Context, key T) error

type WorkSet[T comparable] struct {
	name         string
	mu           sync.RWMutex
	keys         map[T]struct{}
	process      ProcessFunc[T]
	workers      int
	taskInterval time.Duration
	logger       *zap.SugaredLogger
}

func NewWorkSet[T comparable](name string, processFunc ProcessFunc[T], opts ...Option[T]) *WorkSet[T] {
	w := &WorkSet[T]{
		name:         name,
		keys:         make(map[T]struct{}),
		process:      processFunc,
		workers:      defaultWorkers,
		taskInterval: defaultTaskInterval,
		logger:       zap.NewNop().Sugar(),
	}
	for _, o := range opts {
		o(w)
	}
	return w
}

// Track adds a key to the set (idempotent)..
func (w *WorkSet[T]) Track(key T) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.keys[key] = struct{}{}
}

// Forget removes a key from the set.
func (w *WorkSet[T]) Forget(key T) {
	w.mu.Lock()
	delete(w.keys, key)
	w.mu.Unlock()
}

// Contains reports whether the key is in the set.
func (w *WorkSet[T]) Contains(key T) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	_, ok := w.keys[key]
	return ok
}

// Len is the size of the set.
func (w *WorkSet[T]) Len() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return len(w.keys)
}

// Snapshot returns a copy of the current keys for a component to iterate without
// holding the lock or racing concurrent Track/Forget.
func (w *WorkSet[T]) Snapshot() []T {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]T, 0, len(w.keys))
	for k := range w.keys {
		out = append(out, k)
	}
	return out
}

// Start runs the worker pool and the assigner loop until ctx is cancelled.
func (w *WorkSet[T]) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	keyCh := make(chan T)
	for i := 1; i <= w.workers; i++ {
		go w.worker(ctx, i, keyCh)
	}
	w.logger.Infow("Runner started",
		zap.String("runner", w.name), zap.Int("workers", w.workers),
		zap.Duration("taskInterval", w.taskInterval))

	// batch is a snapshot of the watch set that we round-robin through; it is
	// refreshed at each cycle boundary so membership changes are picked up.
	var batch []T
	idx := 0
	for {
		if idx >= len(batch) {
			batch = w.Snapshot()
			idx = 0
		}
		if len(batch) == 0 {
			if !sleep(ctx, w.taskInterval) {
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
		if !sleep(ctx, w.taskInterval/time.Duration(len(batch))) {
			return nil
		}
	}
}

// worker consumes keys and processes each one.
func (w *WorkSet[T]) worker(ctx context.Context, id int, keyCh <-chan T) {
	for {
		select {
		case <-ctx.Done():
			return
		case k := <-keyCh:
			if err := w.process(ctx, k); err != nil {
				w.logger.Warnw("Process failed",
					zap.String("runner", w.name), zap.Int("worker", id),
					zap.Any("key", k), zap.Error(err))
			} else {
				w.logger.Debugw("Process succeeded",
					zap.String("runner", w.name), zap.Int("worker", id), zap.Any("key", k))
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
