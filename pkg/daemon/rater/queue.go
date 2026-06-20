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

package rater

import "sync"

// OverflowQueue is a fixed-capacity FIFO. Append on a full queue drops
// the oldest element. Items returns a snapshot copy safe to iterate
// without holding the lock.
type OverflowQueue[T any] struct {
	mu    sync.RWMutex
	items []T
	cap   int
}

func NewOverflowQueue[T any](capacity int) *OverflowQueue[T] {
	if capacity <= 0 {
		capacity = 1
	}
	return &OverflowQueue[T]{
		items: make([]T, 0, capacity),
		cap:   capacity,
	}
}

func (q *OverflowQueue[T]) Append(v T) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) >= q.cap {
		q.items = append(q.items[:0], q.items[1:]...)
	}
	q.items = append(q.items, v)
}

func (q *OverflowQueue[T]) Items() []T {
	q.mu.RLock()
	defer q.mu.RUnlock()
	out := make([]T, len(q.items))
	copy(out, q.items)
	return out
}

func (q *OverflowQueue[T]) Length() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.items)
}
