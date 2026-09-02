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

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/kynoproj/kynomesh/pkg/apis/proto/daemon"
	"github.com/kynoproj/kynomesh/pkg/daemon/server/rater"
)

type stubQuerier struct {
	res *rater.WindowedResult
	err error
}

func (s stubQuerier) GetMetrics(string, int64) (*rater.WindowedResult, error) {
	return s.res, s.err
}

func TestGetAgentDeployMetrics_NotFound(t *testing.T) {
	svc := NewService(stubQuerier{err: rater.ErrUnknownAgentDeploy})
	_, err := svc.GetAgentDeployMetrics(context.Background(), &pb.GetAgentDeployMetricsRequest{Name: "missing"})
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestGetAgentDeployMetrics_Unavailable(t *testing.T) {
	svc := NewService(stubQuerier{err: rater.ErrNoData})
	_, err := svc.GetAgentDeployMetrics(context.Background(), &pb.GetAgentDeployMetricsRequest{Name: "a"})
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unavailable, st.Code())
}

func TestGetAgentDeployMetrics_InternalForUnknownError(t *testing.T) {
	svc := NewService(stubQuerier{err: errors.New("boom")})
	_, err := svc.GetAgentDeployMetrics(context.Background(), &pb.GetAgentDeployMetricsRequest{Name: "a"})
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
}

func TestGetAgentDeployMetrics_PopulatesAllWindows(t *testing.T) {
	res := &rater.WindowedResult{
		Total: rater.PerWindowValues{
			ProcessingRates: map[string]float64{
				rater.WindowKey1m: 1.0, rater.WindowKey5m: 0.5, rater.WindowKey15m: 0.3,
			},
			StreamMessageRates: map[string]float64{
				rater.WindowKey1m: 12, rater.WindowKey5m: 8, rater.WindowKey15m: 4,
			},
			Inflights: map[string]float64{
				rater.WindowKey1m: 7, rater.WindowKey5m: 6, rater.WindowKey15m: 5,
			},
		},
		PerTransport: map[string]rater.PerWindowValues{
			"rest": {
				ProcessingRates:    map[string]float64{rater.WindowKey1m: 0.8},
				StreamMessageRates: map[string]float64{rater.WindowKey1m: 11},
				Inflights:          map[string]float64{rater.WindowKey1m: 5},
			},
		},
	}
	svc := NewService(stubQuerier{res: res})
	resp, err := svc.GetAgentDeployMetrics(context.Background(), &pb.GetAgentDeployMetricsRequest{Name: "a"})
	require.NoError(t, err)
	m := resp.GetMetrics()
	require.NotNil(t, m)
	assert.Equal(t, "a", m.GetAgentDeploy())
	assert.Equal(t, 1.0, m.GetProcessingRates()[rater.WindowKey1m].GetValue())
	assert.Equal(t, 0.5, m.GetProcessingRates()[rater.WindowKey5m].GetValue())
	assert.Equal(t, 0.3, m.GetProcessingRates()[rater.WindowKey15m].GetValue())
	assert.Equal(t, float64(7), m.GetInflights()[rater.WindowKey1m].GetValue())
	// Stream message rates flow through the response separately from
	// processing rates — the two are independent signals end-to-end.
	assert.Equal(t, float64(12), m.GetStreamMessageRates()[rater.WindowKey1m].GetValue())
	assert.Equal(t, float64(8), m.GetStreamMessageRates()[rater.WindowKey5m].GetValue())
	assert.Equal(t, 0.8, m.GetByTransport()["rest"].GetProcessingRates()[rater.WindowKey1m].GetValue())
	assert.Equal(t, float64(11), m.GetByTransport()["rest"].GetStreamMessageRates()[rater.WindowKey1m].GetValue())
	// CustomWindowEffectiveSeconds is unset when caller didn't request one.
	assert.Nil(t, m.GetCustomWindowEffectiveSeconds())
}

func TestGetAgentDeployMetrics_CustomWindowEchoed(t *testing.T) {
	res := &rater.WindowedResult{
		Total: rater.PerWindowValues{
			ProcessingRates: map[string]float64{rater.WindowKey1m: 0, rater.WindowKeyCustom: 0.9},
			Inflights:       map[string]float64{rater.WindowKey1m: 0, rater.WindowKeyCustom: 4},
		},
		PerTransport:             map[string]rater.PerWindowValues{},
		CustomWindowEffectiveSec: 120,
	}
	svc := NewService(stubQuerier{res: res})
	resp, err := svc.GetAgentDeployMetrics(context.Background(), &pb.GetAgentDeployMetricsRequest{Name: "a", LookbackSeconds: 120})
	require.NoError(t, err)
	m := resp.GetMetrics()
	assert.Equal(t, int64(120), m.GetCustomWindowEffectiveSeconds().GetValue())
	assert.Equal(t, 0.9, m.GetProcessingRates()[rater.WindowKeyCustom].GetValue())
}
