//go:build e2e

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

// Package scaling holds the autoscaling e2e suite.
package scaling_e2e

import (
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	. "github.com/kynoproj/kynomesh/test/fixtures"
)

type ScalingSuite struct {
	E2ESuite
}

// TestAutoscalingUpAndDown exercises the full autoscaling lifecycle on a single
// slow agent (each request held open ~5s):
//
//  1. Sustained concurrent load holds in-flight occupancy high on one replica,
//     overshooting the cold-start per-replica target enough to trip the surge
//     fast-path — spec.replicas scales up.
//  2. Load stops; in-flight drains to zero, so the deployment drifts back to min
//     (short cooldowns in the manifest keep this within the test budget).
func (s *ScalingSuite) TestAutoscalingUpAndDown() {
	const (
		localPort   = 8490
		concurrency = 20
		scaleUpWait = 4 * time.Minute
		scaleDnWait = 4 * time.Minute
	)

	s.Given().AgentSet("@testdata/slow-agent.yaml").
		When().
		CreateAgentSetAndWait().
		Expect().
		AgentSetRunning().
		AgentPodsRunning(1).
		When().
		WaitForAgentServicesReady().
		AgentSetEntryPortForward(localPort).
		GenerateLoad(localPort, concurrency).
		Expect().
		// Under surge load, the deployment scales up past its min of 1.
		AgentDeployScaledUp("slow", 2, scaleUpWait).
		When().
		// Drop the load and let in-flight drain before asserting scale-down.
		StopLoad().
		Expect().
		AgentDeployScaledDown("slow", 1, scaleDnWait).
		When().
		TerminateAllPodPortForwards().
		DeleteAgentSetAndWait().
		Expect().
		AgentSetDeleted(2 * time.Minute)
}

func TestScalingSuite(t *testing.T) {
	suite.Run(t, new(ScalingSuite))
}
