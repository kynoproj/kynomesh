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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestWatchSetTrackForget(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(storeScheme(t)).Build()
	w := NewWatchSet(NewRegistry(c))

	w.Track(nn("a"))
	w.Track(nn("a")) // idempotent
	w.Track(nn("b"))
	assert.Equal(t, 2, w.Len())
	assert.True(t, w.Contains(nn("a")))
	assert.ElementsMatch(t, []string{"a", "b"}, names(w.Snapshot()))

	w.Forget(nn("a"))
	assert.Equal(t, 1, w.Len())
	assert.False(t, w.Contains(nn("a")))
}

func TestWatchSetForgetDropsRegistry(t *testing.T) {
	ad := scalingAD("foo", 2)
	c := fake.NewClientBuilder().WithScheme(storeScheme(t)).WithObjects(ad).Build()
	reg := NewRegistry(c)
	w := NewWatchSet(reg)

	w.Track(nn("foo"))
	_, err := reg.StoreFor(context.Background(), ad)
	require.NoError(t, err)

	w.Forget(nn("foo"))
	assert.False(t, w.Contains(nn("foo")), "dropped from watch set")
	_, ok := reg.Get(nn("foo"))
	assert.False(t, ok, "dropped from registry")
}

func names(keys []types.NamespacedName) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = k.Name
	}
	return out
}
