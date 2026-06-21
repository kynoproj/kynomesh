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

package client

import (
	"context"
	"io"

	pb "github.com/kynoproj/kynomesh/pkg/apis/proto/daemon"
)

// DaemonClient is the controller-facing surface of the metrics
// daemon. Both NewGRPCClient and NewRESTClient return a DaemonClient
// so callers can pick a transport without changing call sites.
type DaemonClient interface {
	io.Closer

	// GetAgentDeployMetrics fetches the current windowed metrics for
	// the named AgentDeploy. A non-zero lookbackSeconds adds a
	// "custom" window in the response (clamped to the daemon's
	// retention).
	GetAgentDeployMetrics(ctx context.Context, name string, lookbackSeconds int64) (*pb.AgentDeployMetrics, error)
}
