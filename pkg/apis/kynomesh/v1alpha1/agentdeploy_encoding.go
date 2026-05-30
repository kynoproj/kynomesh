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

package v1alpha1

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	sharedutil "github.com/kynoproj/kynomesh/pkg/shared/util"
)

// EncodeAgentDeploy returns the base64-encoded JSON of ad.SimpleCopy().
// It's the producer side of the EnvAgentDeployObject contract: the
// reconciler stamps the encoded blob onto the broker container, and the
// broker decodes it at startup via DecodeAgentDeploy.
func EncodeAgentDeploy(ad *AgentDeploy) string {
	simple := ad.SimpleCopy()
	return base64.StdEncoding.EncodeToString([]byte(sharedutil.MustJSON(simple)))
}

// DecodeAgentDeploy reverses EncodeAgentDeploy. It accepts the literal
// value of the EnvAgentDeployObject env var and returns the embedded
// AgentDeploy. Empty input is treated as an error so callers can
// distinguish "not configured" from "configured-but-broken" by checking
// the env var presence before calling.
func DecodeAgentDeploy(encoded string) (*AgentDeploy, error) {
	if encoded == "" {
		return nil, fmt.Errorf("empty %s payload", EnvAgentDeployObject)
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("base64 decode %s: %w", EnvAgentDeployObject, err)
	}
	var ad AgentDeploy
	if err := json.Unmarshal(raw, &ad); err != nil {
		return nil, fmt.Errorf("json unmarshal %s: %w", EnvAgentDeployObject, err)
	}
	return &ad, nil
}
