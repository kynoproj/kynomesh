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

// DriftReload controls whether to restart this AgentDeploy's pods when
// the daemon detects they're serving a stale cached AgentCard for one
// of their peers.
type DriftReload struct {
	// Enabled turns on automatic reload-on-drift for this AgentDeploy.
	// +optional
	Enabled *bool `json:"enabled,omitempty" protobuf:"varint,1,opt,name=enabled"`
}

// IsEnabled reports whether automatic reload-on-drift is turned on,
// tolerating a nil DriftReload or a nil Enabled (both mean "off").
func (d *DriftReload) IsEnabled() bool {
	return d != nil && d.Enabled != nil && *d.Enabled
}
