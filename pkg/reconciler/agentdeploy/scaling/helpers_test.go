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
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
	pb "github.com/kynoproj/kynomesh/pkg/apis/proto/daemon"
	"github.com/kynoproj/kynomesh/pkg/reconciler/agentdeploy/scaling/history"
	"github.com/kynoproj/kynomesh/pkg/reconciler/agentdeploy/scaling/sampling"
)

func testLogger() *zap.SugaredLogger { return zap.NewNop().Sugar() }

func ptrI32(v int32) *int32   { return &v }
func ptrU32(v uint32) *uint32 { return &v }

func storeScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(s))
	require.NoError(t, kmv1.AddToScheme(s))
	return s
}

func nn(name string) types.NamespacedName {
	return types.NamespacedName{Namespace: "ns", Name: name}
}

// scalingAD builds a scaling-enabled AgentDeploy for the loop tests.
func scalingAD(name string, ready uint32) *kmv1.AgentDeploy {
	return &kmv1.AgentDeploy{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: name},
		Spec: kmv1.AgentDeploySpec{
			AbstractAgentDeploy: kmv1.AbstractAgentDeploy{Name: name},
			AgentSetName:        "set",
			Replicas:            ptrI32(1),
		},
		Status: kmv1.AgentDeployStatus{
			Phase:           kmv1.AgentDeployPhaseRunning,
			Replicas:        ready,
			DesiredReplicas: ready,
			ReadyReplicas:   ready,
		},
	}
}

// sample builds a history.Sample for tests.
func sample(ts time.Time, replicas int32, inflight, rate float64) history.Sample {
	return history.Sample{Timestamp: ts, Replicas: replicas, InflightPerRep: inflight, RatePerRep: rate}
}

// noopSource is a minimal sampling.MetricsSource for tests that don't care
// about scrape results (e.g. Tracker fan-out tests).
type noopSource struct{}

func (noopSource) GetAgentDeployMetrics(context.Context, string, int64) (*pb.AgentDeployMetrics, error) {
	return nil, nil
}

func staticDialer(src sampling.MetricsSource) sampling.DaemonDialer {
	return func(string, string) (sampling.MetricsSource, error) { return src, nil }
}
