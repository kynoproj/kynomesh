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

package fixtures

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// A2AResponse is the parsed result of an `a2acli send` call. a2acli prints the
// A2A SendMessageResponse (a Message) as indented JSON on stdout; this captures
// the fields the e2e assertions care about, plus the raw payload for debugging.
type A2AResponse struct {
	MessageID string    `json:"messageId"`
	Role      string    `json:"role"`
	Parts     []A2APart `json:"parts"`
	Raw       string    `json:"-"` // the raw stdout, for logging/debugging
}

// A2APart is one content part of an A2A message. Only the text part is modeled;
// the agents under test reply with text.
type A2APart struct {
	Text string `json:"text"`
}

// Text concatenates the text of every part, which is what assertions match on.
func (r A2AResponse) Text() string {
	var b strings.Builder
	for _, p := range r.Parts {
		b.WriteString(p.Text)
	}
	return b.String()
}

// SendA2AMessage sends a one-shot A2A message to a port-forwarded broker via the
// a2acli binary and returns the parsed response. localPort is the local side of
// a forward to the AgentSet's entry Service. The broker serves a self-signed TLS
// cert, so -k skips verification and --override-host keeps the Host/SNI aligned
// with the local address (matching the research-assistant example's usage).
func SendA2AMessage(localPort int, message string) (A2AResponse, error) {
	host := fmt.Sprintf("localhost:%d", localPort)
	cmd := exec.Command("a2acli",
		"-k",
		"-u", "https://"+host,
		"--override-host="+host,
		"send", message,
		"-o", "json",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return A2AResponse{Raw: stdout.String()}, fmt.Errorf("a2acli send: %w: %s", err, stderr.String())
	}
	resp := A2AResponse{Raw: stdout.String()}
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		return resp, fmt.Errorf("parse a2acli response %q: %w", stdout.String(), err)
	}
	return resp, nil
}
