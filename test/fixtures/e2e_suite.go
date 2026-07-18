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
	"context"
	"time"

	"github.com/stretchr/testify/suite"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
	agentversiond "github.com/kynoproj/kynomesh/pkg/client/clientset/versioned"
	agentpkg "github.com/kynoproj/kynomesh/pkg/client/clientset/versioned/typed/kynomesh/v1alpha1"
	sharedutil "github.com/kynoproj/kynomesh/pkg/shared/util"
)

const (
	Namespace      = "kynomesh-system"
	Label          = "kynomesh-e2e"
	LabelValue     = "true"
	defaultTimeout = 120 * time.Second
)

var (
	background = metav1.DeletePropagationBackground
)

type E2ESuite struct {
	suite.Suite
	restConfig        *rest.Config
	agentSetClient    agentpkg.AgentSetInterface
	agentDeployClient agentpkg.AgentDeployInterface
	kubeClient        kubernetes.Interface
	stopch            chan struct{}
}

func (s *E2ESuite) SetupSuite() {
	var err error
	s.restConfig, err = sharedutil.K8sRestConfig()
	s.CheckError(err)
	s.kubeClient, err = kubernetes.NewForConfig(s.restConfig)
	s.CheckError(err)
	s.stopch = make(chan struct{})
	s.agentSetClient = agentversiond.NewForConfigOrDie(s.restConfig).KynomeshV1alpha1().AgentSets(Namespace)
	s.agentDeployClient = agentversiond.NewForConfigOrDie(s.restConfig).KynomeshV1alpha1().AgentDeploys(Namespace)

	// Clean up resources if any
	s.deleteResources([]schema.GroupVersionResource{
		kmv1.AgentSetGroupVersionResource,
	})
	s.T().Log("Ready for testing")
}

func (s *E2ESuite) TearDownSuite() {
	s.deleteResources([]schema.GroupVersionResource{
		kmv1.AgentSetGroupVersionResource,
	})
	close(s.stopch)
}

func (s *E2ESuite) CheckError(err error) {
	s.T().Helper()
	if err != nil {
		s.T().Fatal(err)
	}
}

func (s *E2ESuite) dynamicFor(r schema.GroupVersionResource) dynamic.ResourceInterface {
	resourceInterface := dynamic.NewForConfigOrDie(s.restConfig).Resource(r)
	return resourceInterface.Namespace(Namespace)
}

func (s *E2ESuite) deleteResources(resources []schema.GroupVersionResource) {
	hasTestLabel := metav1.ListOptions{LabelSelector: Label}
	ctx := context.Background()
	for _, r := range resources {
		err := s.dynamicFor(r).DeleteCollection(ctx, metav1.DeleteOptions{PropagationPolicy: &background}, hasTestLabel)
		s.CheckError(err)
	}

	for _, r := range resources {
		for {
			list, err := s.dynamicFor(r).List(ctx, hasTestLabel)
			s.CheckError(err)
			if len(list.Items) == 0 {
				break
			}
			time.Sleep(1 * time.Second)
		}
	}
}

func (s *E2ESuite) Given() *Given {
	return &Given{
		t:                 s.T(),
		agentSetClient:    s.agentSetClient,
		agentDeployClient: s.agentDeployClient,
		restConfig:        s.restConfig,
		kubeClient:        s.kubeClient,
	}
}
