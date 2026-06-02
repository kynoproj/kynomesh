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

type Language string

const (
	Go     Language = "go"
	Python Language = "python"
	Java   Language = "java"
	Rust   Language = "rust"
)

type Protocol string

const (
	UDS Protocol = "uds"
	TCP Protocol = "tcp"
)

// ServerInfo is the information about the agent server
type ServerInfo struct {
	Protocol Protocol          `json:"protocol"`
	Language Language          `json:"language"`
	Version  string            `json:"version"`
	Metadata map[string]string `json:"metadata"`
}
