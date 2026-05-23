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
	"os"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/yaml"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
	agentpkg "github.com/kynoproj/kynomesh/pkg/client/clientset/versioned/typed/kynomesh/v1alpha1"
)

// Given is the entry point of the e2e fixture DSL. It carries the kynomesh
// typed clients, a kube client and the resources under test, and exposes
// methods to load resources from raw YAML or a file.
type Given struct {
	t                 *testing.T
	agentSetClient    agentpkg.AgentSetInterface
	agentDeployClient agentpkg.AgentDeployInterface
	agentSet          *kmv1.AgentSet
	agentDeploy       *kmv1.AgentDeploy
	restConfig        *rest.Config
	kubeClient        kubernetes.Interface
}

// AgentSet creates an AgentSet based on the parameter, this may be:
//
//  1. A file name if it starts with "@"
//  2. Raw YAML.
func (g *Given) AgentSet(text string) *Given {
	g.t.Helper()
	g.agentSet = &kmv1.AgentSet{}
	g.readResource(text, g.agentSet)
	l := g.agentSet.GetLabels()
	if l == nil {
		l = map[string]string{}
	}
	l[Label] = LabelValue
	g.agentSet.SetLabels(l)
	return g
}

// WithAgentSet adopts an in-memory AgentSet, attaching the fixture label so
// cleanup helpers can find it. Useful for tests that build their spec in code
// rather than YAML.
func (g *Given) WithAgentSet(as *kmv1.AgentSet) *Given {
	g.t.Helper()
	g.agentSet = as
	l := g.agentSet.GetLabels()
	if l == nil {
		l = map[string]string{}
	}
	l[Label] = LabelValue
	g.agentSet.SetLabels(l)
	return g
}

// AgentDeploy loads an AgentDeploy based on the parameter, this may be:
//
//  1. A file name if it starts with "@"
//  2. Raw YAML.
func (g *Given) AgentDeploy(text string) *Given {
	g.t.Helper()
	g.agentDeploy = &kmv1.AgentDeploy{}
	g.readResource(text, g.agentDeploy)
	l := g.agentDeploy.GetLabels()
	if l == nil {
		l = map[string]string{}
	}
	l[Label] = LabelValue
	g.agentDeploy.SetLabels(l)
	return g
}

// WithAgentDeploy adopts an in-memory AgentDeploy, attaching the fixture label
// so cleanup helpers can find it.
func (g *Given) WithAgentDeploy(ad *kmv1.AgentDeploy) *Given {
	g.t.Helper()
	g.agentDeploy = ad
	l := g.agentDeploy.GetLabels()
	if l == nil {
		l = map[string]string{}
	}
	l[Label] = LabelValue
	g.agentDeploy.SetLabels(l)
	return g
}

func (g *Given) readResource(text string, v metav1.Object) {
	g.t.Helper()
	var file string
	if rest, ok := strings.CutPrefix(text, "@"); ok {
		file = rest
	} else {
		f, err := os.CreateTemp("", "kynomesh-e2e")
		if err != nil {
			g.t.Fatal(err)
		}
		if _, err := f.Write([]byte(text)); err != nil {
			g.t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			g.t.Fatal(err)
		}
		file = f.Name()
	}

	f, err := os.ReadFile(file)
	if err != nil {
		g.t.Fatal(err)
	}
	if err := yaml.Unmarshal(f, v); err != nil {
		g.t.Fatal(err)
	}
}

// When transitions the fixture into the action phase.
func (g *Given) When() *When {
	return &When{
		t:                 g.t,
		agentSetClient:    g.agentSetClient,
		agentDeployClient: g.agentDeployClient,
		agentSet:          g.agentSet,
		agentDeploy:       g.agentDeploy,
		restConfig:        g.restConfig,
		kubeClient:        g.kubeClient,
	}
}
