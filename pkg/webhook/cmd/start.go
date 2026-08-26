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

package cmd

import (
	"crypto/tls"
	"fmt"
	"os"
	"strconv"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
	"github.com/kynoproj/kynomesh/pkg/client/clientset/versioned"
	"github.com/kynoproj/kynomesh/pkg/shared/logging"
	sharedutil "github.com/kynoproj/kynomesh/pkg/shared/util"
	"github.com/kynoproj/kynomesh/pkg/version"
	"github.com/kynoproj/kynomesh/pkg/webhook"
)

const (
	serviceNameEnvVar     = "SERVICE_NAME"
	deploymentNameEnvVar  = "DEPLOYMENT_NAME"
	clusterRoleNameEnvVar = "CLUSTER_ROLE_NAME"
	namespaceEnvVar       = "NAMESPACE"
	portEnvVar            = "PORT"
)

func Start() {
	logger := logging.NewLogger().Named("webhook")
	restConfig, err := sharedutil.K8sRestConfig()
	if err != nil {
		logger.Fatalw("Failed to get kubeconfig", zap.Error(err))
	}

	options, err := resolveOptions()
	if err != nil {
		logger.Fatalw("Invalid webhook configuration", zap.Error(err))
	}

	kubeClient := kubernetes.NewForConfigOrDie(restConfig)
	kynomeshClient := versioned.NewForConfigOrDie(restConfig).KynomeshV1alpha1()

	controller := webhook.AdmissionController{
		Client:         kubeClient,
		KynomeshClient: kynomeshClient,
		Options:        options,
		Handlers: map[schema.GroupVersionKind]runtime.Object{
			kmv1.AgentSetGroupVersionKind: &kmv1.AgentSet{},
		},
		Logger: logger,
	}
	logger.Infow("Starting admission controller", "version", version.GetVersion())
	ctx := logging.WithLogger(signals.SetupSignalHandler(), logger)
	if err := controller.Run(ctx); err != nil {
		logger.Fatalw("Failed to create admission controller", zap.Error(err))
	}
}

// resolveOptions reads the webhook's env-var configuration into a
// webhook.Options. NAMESPACE is required (the webhook must know which
// namespace its own Service/Secret/Deployment live in); everything else
// falls back to defaults matching the shipped Deployment manifest.
func resolveOptions() (webhook.Options, error) {
	namespace, defined := os.LookupEnv(namespaceEnvVar)
	if !defined {
		return webhook.Options{}, fmt.Errorf("required env %s isn't set", namespaceEnvVar)
	}

	portStr := sharedutil.LookupEnvStringOr(portEnvVar, "443")
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return webhook.Options{}, fmt.Errorf("env %s must be a number, got %q: %w", portEnvVar, portStr, err)
	}

	return webhook.Options{
		ServiceName:     sharedutil.LookupEnvStringOr(serviceNameEnvVar, "kynomesh-webhook"),
		DeploymentName:  sharedutil.LookupEnvStringOr(deploymentNameEnvVar, "kynomesh-webhook"),
		ClusterRoleName: sharedutil.LookupEnvStringOr(clusterRoleNameEnvVar, "kynomesh-webhook"),
		Namespace:       namespace,
		Port:            port,
		SecretName:      "kynomesh-webhook-certs",
		WebhookName:     "webhook.kynomesh.kyno.sh",
		ClientAuth:      tls.VerifyClientCertIfGiven,
	}, nil
}
