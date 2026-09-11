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
	"os"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
	pb "github.com/kynoproj/kynomesh/pkg/apis/proto/daemon"
	"github.com/kynoproj/kynomesh/pkg/daemon/server/rater"
)

// Querier is the subset of *rater.Rater that the service needs. It
// exists so unit tests can stub responses without spinning up a real
// rater.
type Querier interface {
	GetMetrics(name string, lookbackSeconds int64) (*rater.WindowedResult, error)

	// GetPeerCardDrift returns the current drift-comparison state for
	// name's peers: for each peer, the daemon's own stability-gated
	// AgentCard hash alongside what each of name's live pods currently
	// reports for that peer. See rater.PeerCardDrift.
	GetPeerCardDrift(name string) (map[string]rater.PeerCardDrift, error)
}

// Service implements pb.DaemonServiceServer over a Querier.
type Service struct {
	pb.UnimplementedDaemonServiceServer
	q Querier
}

var _ pb.DaemonServiceServer = (*Service)(nil)

func NewService(q Querier) *Service { return &Service{q: q} }

// GetAgentDeployMetrics translates a Querier result into the protobuf
// response shape:
//
//   - NotFound when the AgentDeploy is unknown.
//   - Unavailable when no samples have accumulated yet.
//   - OK with a populated AgentDeployMetrics otherwise. Empty per-
//     window maps inside the response mean "data exists but we
//     couldn't compute that specific window."
func (s *Service) GetAgentDeployMetrics(_ context.Context, req *pb.GetAgentDeployMetricsRequest) (*pb.GetAgentDeployMetricsResponse, error) {
	res, err := s.q.GetMetrics(req.GetName(), req.GetLookbackSeconds())
	switch {
	case errors.Is(err, rater.ErrUnknownAgentDeploy):
		return nil, status.Errorf(codes.NotFound, "unknown AgentDeploy %q", req.GetName())
	case errors.Is(err, rater.ErrNoData):
		return nil, status.Errorf(codes.Unavailable, "no samples yet for %q", req.GetName())
	case err != nil:
		return nil, status.Errorf(codes.Internal, "compute metrics: %v", err)
	}

	pbByTransport := make(map[string]*pb.TransportWindowedMetrics, len(res.PerTransport))
	for t, v := range res.PerTransport {
		pbByTransport[t] = &pb.TransportWindowedMetrics{
			ProcessingRates:    mapFloatToDV(v.ProcessingRates),
			StreamMessageRates: mapFloatToDV(v.StreamMessageRates),
			Inflights:          mapFloatToDV(v.Inflights),
		}
	}

	return &pb.GetAgentDeployMetricsResponse{
		Metrics: &pb.AgentDeployMetrics{
			AgentSet:                     os.Getenv(kmv1.EnvAgentSetName),
			AgentDeploy:                  req.GetName(),
			ProcessingRates:              mapFloatToDV(res.Total.ProcessingRates),
			StreamMessageRates:           mapFloatToDV(res.Total.StreamMessageRates),
			Inflights:                    mapFloatToDV(res.Total.Inflights),
			ByTransport:                  pbByTransport,
			CustomWindowEffectiveSeconds: optionalInt64(res.CustomWindowEffectiveSec),
		},
	}, nil
}

// GetPeerCardDrift translates a Querier result into the protobuf
// response shape:
//
//   - NotFound when the AgentDeploy is unknown.
//   - OK with a populated GetPeerCardDriftResponse otherwise. A peer with
//     no reported hashes from any pod is still present in the map (empty
//     ReportedHashes) — see rater.PeerCardDrift.
func (s *Service) GetPeerCardDrift(_ context.Context, req *pb.GetPeerCardDriftRequest) (*pb.GetPeerCardDriftResponse, error) {
	res, err := s.q.GetPeerCardDrift(req.GetName())
	switch {
	case errors.Is(err, rater.ErrUnknownAgentDeploy):
		return nil, status.Errorf(codes.NotFound, "unknown AgentDeploy %q", req.GetName())
	case err != nil:
		return nil, status.Errorf(codes.Internal, "compute peer card drift: %v", err)
	}

	peers := make(map[string]*pb.PeerCardDrift, len(res))
	for peer, drift := range res {
		reported := make(map[string]*pb.ReportedHash, len(drift.ReportedHashes))
		for pod, rh := range drift.ReportedHashes {
			reported[pod] = &pb.ReportedHash{
				Hash:       rh.Hash,
				ObservedAt: optionalTimestamp(rh.ObservedAt),
			}
		}
		peers[peer] = &pb.PeerCardDrift{
			LatestHash:           drift.LatestHash,
			LatestHashObservedAt: optionalTimestamp(drift.LatestHashObservedAt),
			ReportedHashes:       reported,
		}
	}
	return &pb.GetPeerCardDriftResponse{Peers: peers}, nil
}

// mapFloatToDV wraps each value in DoubleValue. A nil input yields a
// nil map, distinguishing "no data computed" from "empty map written."
func mapFloatToDV(m map[string]float64) map[string]*wrapperspb.DoubleValue {
	if m == nil {
		return nil
	}
	out := make(map[string]*wrapperspb.DoubleValue, len(m))
	for k, v := range m {
		out[k] = wrapperspb.Double(v)
	}
	return out
}

// optionalInt64 returns nil for zero so the gRPC client sees an
// explicitly-absent field rather than a "0 means absent" sentinel.
func optionalInt64(v int64) *wrapperspb.Int64Value {
	if v == 0 {
		return nil
	}
	return wrapperspb.Int64(v)
}

// optionalTimestamp returns nil for the zero time so the gRPC client sees
// an explicitly-absent field ("not yet observed") rather than the Unix
// epoch.
func optionalTimestamp(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}
