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

package driftwatch

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
	pb "github.com/kynoproj/kynomesh/pkg/apis/proto/daemon"
	daemonclient "github.com/kynoproj/kynomesh/pkg/daemon/client"
)

// DriftSource is the minimal daemon-client surface this component needs.
// daemonclient.DaemonClient already satisfies this structurally.
type DriftSource interface {
	GetPeerCardDrift(ctx context.Context, name string) (map[string]*pb.PeerCardDrift, error)
}

// DaemonDialer returns a drift source for an AgentSet's daemon. Injected so
// tests can supply a fake without dialing.
type DaemonDialer func(namespace, agentSetName string) (DriftSource, error)

// GRPCDaemonDialer dials the per-AgentSet daemon's gRPC API by its in-cluster
// Service DNS name.
func GRPCDaemonDialer(namespace, agentSetName string) (DriftSource, error) {
	as := &kmv1.AgentSet{ObjectMeta: metav1.ObjectMeta{Name: agentSetName}}
	addr := fmt.Sprintf("%s.%s.svc:%d", as.DaemonName(), namespace, kmv1.DaemonAPIPort)
	return daemonclient.NewGRPCClient(addr)
}
