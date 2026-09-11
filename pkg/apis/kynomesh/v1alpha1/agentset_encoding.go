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

// EncodeAgentSet returns the base64-encoded JSON of as.SimpleCopy().
func EncodeAgentSet(as *AgentSet) string {
	simple := as.SimpleCopy()
	return base64.StdEncoding.EncodeToString([]byte(sharedutil.MustJSON(simple)))
}

// DecodeAgentSet reverses EncodeAgentSet.
func DecodeAgentSet(encoded string) (*AgentSet, error) {
	if encoded == "" {
		return nil, fmt.Errorf("empty %s payload", EnvAgentSetObject)
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("base64 decode %s: %w", EnvAgentSetObject, err)
	}
	var as AgentSet
	if err := json.Unmarshal(raw, &as); err != nil {
		return nil, fmt.Errorf("json unmarshal %s: %w", EnvAgentSetObject, err)
	}
	return &as, nil
}
