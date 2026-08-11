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

package cmd

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

func TestDeriveBudgets_FitsWithinGrace(t *testing.T) {
	// For any sane grace period, drain + shutdown + margin must not exceed it,
	// or the kubelet SIGKILLs mid-shutdown.
	const margin = 2 * time.Second
	for _, graceSecs := range []int{30, 60, 120, 300, 600} {
		grace := time.Duration(graceSecs) * time.Second
		b := deriveBudgets(grace)
		assert.LessOrEqual(t, b.Drain+b.Shutdown+margin, grace,
			"grace=%s: drain(%s)+shutdown(%s)+margin must fit", grace, b.Drain, b.Shutdown)
		assert.Positive(t, b.Drain, "grace=%s: drain must be positive", grace)
		assert.Positive(t, b.Shutdown, "grace=%s: shutdown must be positive", grace)
		assert.GreaterOrEqual(t, b.Drain, b.Propagation, "grace=%s: drain covers at least propagation", grace)
	}
}

func TestDeriveBudgets_Default(t *testing.T) {
	grace := time.Duration(kmv1.DefaultTerminationGracePeriodSeconds) * time.Second // 120s
	b := deriveBudgets(grace)
	// 120/10 = 12s shutdown (within [5,15]); 120/20 = 6s propagation (within [2,10]);
	// drain = 120 - 12 - 2 = 106s.
	assert.Equal(t, 12*time.Second, b.Shutdown)
	assert.Equal(t, 6*time.Second, b.Propagation)
	assert.Equal(t, 106*time.Second, b.Drain)
}

func TestDeriveBudgets_ClampsSmallGrace(t *testing.T) {
	// Tiny grace: shutdown floored at 5s, propagation floored at 2s; drain must
	// not go negative and stays >= propagation.
	b := deriveBudgets(10 * time.Second)
	assert.Equal(t, 5*time.Second, b.Shutdown)
	assert.Equal(t, 2*time.Second, b.Propagation)
	assert.GreaterOrEqual(t, b.Drain, b.Propagation)
	assert.Positive(t, b.Drain)
}

func TestDeriveBudgets_ClampsLargeGrace(t *testing.T) {
	// Huge grace: shutdown/propagation caps hold; drain absorbs the rest.
	b := deriveBudgets(3600 * time.Second)
	assert.Equal(t, 15*time.Second, b.Shutdown)
	assert.Equal(t, 10*time.Second, b.Propagation)
	assert.Equal(t, 3600*time.Second-15*time.Second-2*time.Second, b.Drain)
}

func TestResolveBudgets_FromEnv(t *testing.T) {
	t.Setenv(kmv1.EnvTerminationGraceSeconds, "300")
	b := resolveBudgets()
	assert.Equal(t, deriveBudgets(300*time.Second), b)
}

func TestResolveBudgets_DefaultsWhenUnsetOrBad(t *testing.T) {
	want := deriveBudgets(time.Duration(kmv1.DefaultTerminationGracePeriodSeconds) * time.Second)

	t.Run("unset", func(t *testing.T) {
		t.Setenv(kmv1.EnvTerminationGraceSeconds, "")
		assert.Equal(t, want, resolveBudgets())
	})
	t.Run("unparseable", func(t *testing.T) {
		t.Setenv(kmv1.EnvTerminationGraceSeconds, "not-a-number")
		assert.Equal(t, want, resolveBudgets())
	})
	t.Run("non-positive", func(t *testing.T) {
		t.Setenv(kmv1.EnvTerminationGraceSeconds, "0")
		assert.Equal(t, want, resolveBudgets())
	})
}
