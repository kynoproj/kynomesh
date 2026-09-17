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
	"time"

	"go.uber.org/zap"
)

const (
	defaultWorkers      = 20
	defaultTaskInterval = 60 * time.Second
)

type Option[T comparable] func(*WorkSet[T])

// WithTaskInterval sets the task interval
func WithTaskInterval[T comparable](d time.Duration) Option[T] {
	return func(o *WorkSet[T]) {
		o.taskInterval = d
	}
}

// WithWorkers sets the workers
func WithWorkers[T comparable](i int) Option[T] {
	return func(o *WorkSet[T]) {
		o.workers = i
	}
}

// WithLogger sets the logger
func WithLogger[T comparable](logger *zap.SugaredLogger) Option[T] {
	return func(o *WorkSet[T]) {
		o.logger = logger
	}
}
