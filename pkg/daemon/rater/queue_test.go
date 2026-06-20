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

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOverflowQueue_BasicAppendAndItems(t *testing.T) {
	q := NewOverflowQueue[int](3)
	q.Append(1)
	q.Append(2)
	q.Append(3)
	assert.Equal(t, 3, q.Length())
	assert.Equal(t, []int{1, 2, 3}, q.Items())
}

func TestOverflowQueue_DropsOldestOnOverflow(t *testing.T) {
	q := NewOverflowQueue[int](2)
	q.Append(1)
	q.Append(2)
	q.Append(3)
	assert.Equal(t, []int{2, 3}, q.Items())
	q.Append(4)
	assert.Equal(t, []int{3, 4}, q.Items())
}

func TestOverflowQueue_ItemsReturnsCopy(t *testing.T) {
	q := NewOverflowQueue[int](3)
	q.Append(1)
	q.Append(2)
	items := q.Items()
	items[0] = 99
	assert.Equal(t, []int{1, 2}, q.Items())
}

func TestOverflowQueue_ZeroCapacityCoercedToOne(t *testing.T) {
	q := NewOverflowQueue[int](0)
	q.Append(1)
	q.Append(2)
	assert.Equal(t, []int{2}, q.Items())
}

func TestOverflowQueue_ConcurrentAppends(t *testing.T) {
	q := NewOverflowQueue[int](1000)
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := range 10 {
				q.Append(i*10 + j)
			}
		}(i)
	}
	wg.Wait()
	assert.Equal(t, 1000, q.Length())
}
