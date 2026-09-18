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
	"testing"
	"time"

	"go.uber.org/zap"
)

func noopProcess(context.Context, string) error { return nil }

func TestNewWorkSetDefaults(t *testing.T) {
	w := NewWorkSet("test", noopProcess)

	if w.workers != defaultWorkers {
		t.Errorf("workers = %d, want %d", w.workers, defaultWorkers)
	}
	if w.taskInterval != defaultTaskInterval {
		t.Errorf("taskInterval = %v, want %v", w.taskInterval, defaultTaskInterval)
	}
	if w.logger == nil {
		t.Error("logger = nil, want a no-op zap logger")
	}
	if w.keys == nil {
		t.Error("keys = nil, want an initialized map")
	}
}

func TestWithTaskInterval(t *testing.T) {
	w := NewWorkSet("test", noopProcess, WithTaskInterval[string](5*time.Second))

	if w.taskInterval != 5*time.Second {
		t.Errorf("taskInterval = %v, want %v", w.taskInterval, 5*time.Second)
	}
}

func TestWithWorkers(t *testing.T) {
	w := NewWorkSet("test", noopProcess, WithWorkers[string](3))

	if w.workers != 3 {
		t.Errorf("workers = %d, want %d", w.workers, 3)
	}
}

func TestWithLogger(t *testing.T) {
	logger := zap.NewExample().Sugar()
	w := NewWorkSet("test", noopProcess, WithLogger[string](logger))

	if w.logger != logger {
		t.Error("logger was not set to the provided logger")
	}
}

func TestOptionsAppliedInOrder(t *testing.T) {
	w := NewWorkSet("test", noopProcess,
		WithWorkers[string](1),
		WithWorkers[string](7),
	)

	if w.workers != 7 {
		t.Errorf("workers = %d, want %d (last option should win)", w.workers, 7)
	}
}
