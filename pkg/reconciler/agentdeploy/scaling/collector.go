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
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
	pb "github.com/kynoproj/kynomesh/pkg/apis/proto/daemon"
)

// Window keys mirror the daemon rater's windows
// (pkg/daemon/server/rater). Duplicated here to keep the reconciler from
// importing the daemon server; a drift test guards the values.
const (
	windowKey1m     = "1m"
	windowKeyCustom = "custom"
)

// MetricsSource is the slice of the daemon client the collector needs. The
// concrete daemon client satisfies it; tests supply a fake.
type MetricsSource interface {
	GetAgentDeployMetrics(ctx context.Context, name string, lookbackSeconds int64) (*pb.AgentDeployMetrics, error)
}

// collectSample fetches one metrics snapshot for ad from src over the given
// averaging window (lookbackSeconds; 0 uses the daemon's default 1m window) and
// converts the daemon's fleet totals into a per-replica Sample using ad's ready
// replicas.
//
// ok=false with a nil error means "no usable sample this tick" — cold start
// (daemon reports Unavailable/NotFound), no ready replicas to normalize by, or
// the chosen window isn't computable yet. The caller should simply skip rather
// than treat it as a failure. The caller is responsible for recording the
// returned Sample.
func collectSample(ctx context.Context, src MetricsSource, ad *kmv1.AgentDeploy, now time.Time, lookback int64) (Sample, bool, error) {
	m, err := src.GetAgentDeployMetrics(ctx, ad.Spec.Name, lookback)
	if err != nil {
		switch status.Code(err) {
		case codes.Unavailable, codes.NotFound:
			// Daemon has no samples yet, or doesn't know this AgentDeploy.
			return Sample{}, false, nil
		default:
			return Sample{}, false, fmt.Errorf("get agentdeploy metrics: %w", err)
		}
	}

	window := windowKey1m
	if lookback > 0 {
		window = windowKeyCustom
	}
	totalInflight, ok := doubleVal(m.GetInflights(), window)
	if !ok {
		return Sample{}, false, nil
	}
	totalRate, ok := doubleVal(m.GetProcessingRates(), window)
	if !ok {
		return Sample{}, false, nil
	}

	ready := ad.Status.ReadyReplicas
	if ready == 0 {
		// Can't convert fleet totals to per-replica without a divisor.
		return Sample{}, false, nil
	}

	s := Sample{
		Timestamp:      now,
		Replicas:       int32(ready),
		InflightPerRep: totalInflight / float64(ready),
		RatePerRep:     totalRate / float64(ready),
	}
	return s, true, nil
}

// doubleVal reads a window's value from a daemon metric map, reporting whether
// it was present and non-nil.
func doubleVal(m map[string]*wrapperspb.DoubleValue, window string) (float64, bool) {
	v, ok := m[window]
	if !ok || v == nil {
		return 0, false
	}
	return v.GetValue(), true
}
