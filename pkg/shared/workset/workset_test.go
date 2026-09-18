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
	"errors"
	"sort"
	"sync"
	"testing"
	"time"
)

func TestTrackAndContains(t *testing.T) {
	w := NewWorkSet("test", noopProcess)

	if w.Contains("a") {
		t.Fatal("Contains(\"a\") = true before Track")
	}

	w.Track("a")

	if !w.Contains("a") {
		t.Fatal("Contains(\"a\") = false after Track")
	}
}

func TestTrackIsIdempotent(t *testing.T) {
	w := NewWorkSet("test", noopProcess)

	w.Track("a")
	w.Track("a")

	if got := w.Len(); got != 1 {
		t.Errorf("Len() = %d, want 1 after tracking the same key twice", got)
	}
}

func TestForgetRemovesKey(t *testing.T) {
	w := NewWorkSet("test", noopProcess)

	w.Track("a")
	w.Forget("a")

	if w.Contains("a") {
		t.Error("Contains(\"a\") = true after Forget")
	}
	if got := w.Len(); got != 0 {
		t.Errorf("Len() = %d, want 0 after Forget", got)
	}
}

func TestForgetUnknownKeyIsNoop(t *testing.T) {
	w := NewWorkSet("test", noopProcess)

	w.Forget("missing")

	if got := w.Len(); got != 0 {
		t.Errorf("Len() = %d, want 0", got)
	}
}

func TestLen(t *testing.T) {
	w := NewWorkSet("test", noopProcess)

	w.Track("a")
	w.Track("b")
	w.Track("c")

	if got := w.Len(); got != 3 {
		t.Errorf("Len() = %d, want 3", got)
	}
}

func TestSnapshotReturnsAllKeys(t *testing.T) {
	w := NewWorkSet("test", noopProcess)

	w.Track("a")
	w.Track("b")

	got := w.Snapshot()
	sort.Strings(got)

	want := []string{"a", "b"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("Snapshot() = %v, want %v", got, want)
	}
}

func TestSnapshotIsIndependentCopy(t *testing.T) {
	w := NewWorkSet("test", noopProcess)
	w.Track("a")

	snap := w.Snapshot()
	w.Track("b")

	if len(snap) != 1 {
		t.Errorf("Snapshot() mutated by later Track; got %v", snap)
	}
}

func TestSnapshotEmptySet(t *testing.T) {
	w := NewWorkSet("test", noopProcess)

	got := w.Snapshot()

	if len(got) != 0 {
		t.Errorf("Snapshot() = %v, want empty", got)
	}
}

func TestStartProcessesTrackedKeys(t *testing.T) {
	var mu sync.Mutex
	seen := make(map[string]int)
	process := func(_ context.Context, key string) error {
		mu.Lock()
		seen[key]++
		mu.Unlock()
		return nil
	}

	w := NewWorkSet("test", process,
		WithWorkers[string](2),
		WithTaskInterval[string](10*time.Millisecond),
	)
	w.Track("a")
	w.Track("b")

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- w.Start(ctx) }()

	<-ctx.Done()
	if err := <-done; err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if seen["a"] == 0 {
		t.Error("key \"a\" was never processed")
	}
	if seen["b"] == 0 {
		t.Error("key \"b\" was never processed")
	}
}

func TestStartReturnsOnContextCancel(t *testing.T) {
	w := NewWorkSet("test", noopProcess, WithTaskInterval[string](time.Second))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Start(ctx) }()

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start() returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Start() did not return after context cancellation")
	}
}

func TestStartWithEmptySetWaitsWithoutProcessing(t *testing.T) {
	called := false
	process := func(context.Context, string) error {
		called = true
		return nil
	}
	w := NewWorkSet("test", process, WithTaskInterval[string](5*time.Millisecond))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}
	if called {
		t.Error("process was called on an empty watch set")
	}
}

func TestStartLogsProcessErrorWithoutCrashing(t *testing.T) {
	process := func(context.Context, string) error {
		return errors.New("boom")
	}
	w := NewWorkSet("test", process,
		WithWorkers[string](1),
		WithTaskInterval[string](5*time.Millisecond),
	)
	w.Track("a")

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start() returned error: %v", err)
	}
}

func TestTrackForgetConcurrentAccess(t *testing.T) {
	w := NewWorkSet("test", func(context.Context, int) error { return nil })

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(key int) {
			defer wg.Done()
			w.Track(key)
			w.Contains(key)
			w.Len()
			w.Snapshot()
			w.Forget(key)
		}(i)
	}
	wg.Wait()

	if got := w.Len(); got != 0 {
		t.Errorf("Len() = %d, want 0 after all keys tracked and forgotten", got)
	}
}
