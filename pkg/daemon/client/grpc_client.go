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
	"crypto/tls"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	pb "github.com/kynoproj/kynomesh/pkg/apis/proto/daemon"
)

type grpcClient struct {
	conn   *grpc.ClientConn
	client pb.DaemonServiceClient
}

var _ DaemonClient = (*grpcClient)(nil)

// NewGRPCClient dials address (host:port) with TLS skip-verify and
// returns a DaemonClient. Close the client when done to release
// the underlying connection.
func NewGRPCClient(address string) (DaemonClient, error) {
	creds := credentials.NewTLS(&tls.Config{
		InsecureSkipVerify: true, //nolint:gosec
		MinVersion:         tls.VersionTLS12,
	})
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, err
	}
	return &grpcClient{
		conn:   conn,
		client: pb.NewDaemonServiceClient(conn),
	}, nil
}

func (c *grpcClient) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *grpcClient) GetAgentDeployMetrics(ctx context.Context, name string, lookbackSeconds int64) (*pb.AgentDeployMetrics, error) {
	resp, err := c.client.GetAgentDeployMetrics(ctx, &pb.GetAgentDeployMetricsRequest{
		Name:            name,
		LookbackSeconds: lookbackSeconds,
	})
	if err != nil {
		return nil, err
	}
	return resp.GetMetrics(), nil
}
