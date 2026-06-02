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

package serverinfo

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// Load reads and decodes the agent server-info file at path. Returns
// (nil, nil) if the file is absent so callers can treat that as "agent
// did not advertise."
func Load(path string) (*ServerInfo, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %q: %w", path, err)
	}
	var info ServerInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return nil, fmt.Errorf("decode %q: %w", path, err)
	}
	return &info, nil
}
