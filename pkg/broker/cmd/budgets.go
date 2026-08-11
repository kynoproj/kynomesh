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
	"os"
	"strconv"
	"time"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

// terminationBudgets are the time slices the broker carves out of the pod's
// terminationGracePeriodSeconds. The two phases run sequentially — the preStop
// drain finishes before SIGTERM, then the post-SIGTERM shutdown runs — so their
// maxima plus a safety margin must fit within the grace period, or the kubelet
// SIGKILLs mid-shutdown.
type terminationBudgets struct {
	// Propagation is a fixed initial wait in the preStop drain that lets
	// Kubernetes finish removing the pod from Service endpoints.
	Propagation time.Duration
	// Drain is the preStop poll-until-idle budget (includes Propagation).
	Drain time.Duration
	// Shutdown is the post-SIGTERM http/gRPC graceful-stop budget.
	Shutdown time.Duration
}

// deriveBudgets splits a grace period into its phases.
func deriveBudgets(grace time.Duration) terminationBudgets {
	const margin = 2 * time.Second
	propagation := clampDuration(grace/20, 2*time.Second, 10*time.Second)
	shutdown := clampDuration(grace/10, 5*time.Second, 15*time.Second)
	drain := grace - shutdown - margin
	if drain < propagation {
		// Grace period too small to carve a real drain window; fall back to at
		// least the propagation wait so preStop still sheds new traffic.
		drain = propagation
	}
	return terminationBudgets{Propagation: propagation, Drain: drain, Shutdown: shutdown}
}

// resolveBudgets reads the injected terminationGracePeriodSeconds env and
// derives the budgets. Falls back to the default grace period when the env is
// unset or unparseable.
func resolveBudgets() terminationBudgets {
	grace := time.Duration(kmv1.DefaultTerminationGracePeriodSeconds) * time.Second
	if raw := os.Getenv(kmv1.EnvTerminationGraceSeconds); raw != "" {
		if secs, err := strconv.ParseInt(raw, 10, 64); err == nil && secs > 0 {
			grace = time.Duration(secs) * time.Second
		}
	}
	return deriveBudgets(grace)
}

func clampDuration(v, lo, hi time.Duration) time.Duration {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
