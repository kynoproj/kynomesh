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
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"

	pb "github.com/kynoproj/kynomesh/pkg/apis/proto/daemon"
)

// jsonMarshaller decodes the daemon's JSON responses using grpc-
// gateway's JSONPb. Required because the response carries well-known
// wrapper types (DoubleValue, Int64Value) and map fields that the
// stdlib decoder would mishandle.
var jsonMarshaller = new(runtime.JSONPb)

// defaultRESTTimeout caps a single REST call.
const defaultRESTTimeout = 2 * time.Second

type restClient struct {
	baseURL    string
	httpClient *http.Client
}

var _ DaemonClient = (*restClient)(nil)

// NewRESTClient builds a REST DaemonClient targeting address
// (host:port or full URL). If no scheme is present, "https://" is
// assumed — the daemon only listens on TLS.
func NewRESTClient(address string) (DaemonClient, error) {
	if !strings.HasPrefix(address, "https://") && !strings.HasPrefix(address, "http://") {
		address = "https://" + address
	}
	return &restClient{
		baseURL: strings.TrimRight(address, "/"),
		httpClient: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true, //nolint:gosec
					MinVersion:         tls.VersionTLS12,
				},
			},
			Timeout: defaultRESTTimeout,
		},
	}, nil
}

// Close is a no-op for REST. http.Client has no owned long-lived
// resources to release; idle connections close naturally.
func (c *restClient) Close() error { return nil }

func (c *restClient) GetAgentDeployMetrics(ctx context.Context, name string, lookbackSeconds int64) (*pb.AgentDeployMetrics, error) {
	u := fmt.Sprintf("%s/api/v1/agentdeploys/%s/metrics", c.baseURL, url.PathEscape(name))
	if lookbackSeconds != 0 {
		u = fmt.Sprintf("%s?lookback_seconds=%d", u, lookbackSeconds)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get agentdeploy metrics: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, err := unmarshalResponse[pb.GetAgentDeployMetricsResponse](resp)
	if err != nil {
		return nil, err
	}
	return out.GetMetrics(), nil
}

// unmarshalResponse decodes a 2xx JSON response into a value of type
// T using grpc-gateway's protobuf-aware JSON marshaller. Non-2xx
// status codes surface as errors carrying the body for debug.
func unmarshalResponse[T any](r *http.Response) (*T, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	if r.StatusCode < 200 || r.StatusCode >= 300 {
		return nil, fmt.Errorf("unexpected status %d: %s", r.StatusCode, strings.TrimSpace(string(body)))
	}
	var t T
	if err := jsonMarshaller.Unmarshal(body, &t); err != nil {
		return nil, fmt.Errorf("decode %T: %w", t, err)
	}
	return &t, nil
}
