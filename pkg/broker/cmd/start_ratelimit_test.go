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

	"github.com/stretchr/testify/assert"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

func TestResolveMaxInFlight(t *testing.T) {
	adWith := func(limit *int32) *kmv1.AgentDeploy {
		ad := &kmv1.AgentDeploy{}
		if limit != nil {
			ad.Spec.RateLimit = &kmv1.RateLimit{MaxInFlight: limit}
		}
		return ad
	}
	ptr := func(v int32) *int32 { return &v }

	cases := []struct {
		name string
		ad   *kmv1.AgentDeploy
		want int
	}{
		{name: "nil agentdeploy is unlimited", ad: nil, want: 0},
		{name: "no rateLimit block is unlimited", ad: &kmv1.AgentDeploy{}, want: 0},
		{name: "rateLimit with nil maxInFlight is unlimited", ad: adWith(nil), want: 0},
		{name: "explicit zero is unlimited", ad: adWith(ptr(0)), want: 0},
		{name: "positive cap is honored", ad: adWith(ptr(20)), want: 20},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, resolveMaxInFlight(tc.ad))
		})
	}
}
