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

// Package cmd boots the kynomesh controller-manager: it constructs a
// controller-runtime manager, builds each reconciler as a standalone
// controller, wires probes and metrics, and blocks until the process
// is signalled to exit.
package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/go-logr/zapr"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
	"github.com/kynoproj/kynomesh/pkg/reconciler/agentdeploy"
	"github.com/kynoproj/kynomesh/pkg/reconciler/agentset"
	"github.com/kynoproj/kynomesh/pkg/shared/logging"
	sharedutil "github.com/kynoproj/kynomesh/pkg/shared/util"
	"github.com/kynoproj/kynomesh/pkg/version"
)

const (
	// leaderElectionID is the lease name used when multiple controller
	// replicas race for leadership.
	leaderElectionID = "kynomesh-controller-lock"

	metricsAddr = ":9090"
	probeAddr   = ":8081"

	// imageDiscoveryTimeout caps the one-shot self-pod lookup at startup.
	// If the API server is unreachable that long, fail fast — the
	// controller can't function without it anyway.
	imageDiscoveryTimeout = 30 * time.Second

	// Leader-election lease defaults.
	defaultLeaseDuration = 15 * time.Second
	defaultRenewDeadline = 10 * time.Second
	defaultRetryPeriod   = 2 * time.Second
)

// Start boots the controller manager. It blocks until the signal handler
// fires (typically SIGINT or SIGTERM) and only returns after the manager has
// cleanly shut down.
//
// namespaced=true scopes the cache to managedNamespace so the operator can
// run without cluster-wide list/watch RBAC.
func Start(namespaced bool, managedNamespace string) {
	logger := logging.NewLogger().Named("controller-manager")

	// Route controller-runtime's internal logs into the same zap logger.
	// Without this, controller-runtime panics on first log call ("must call
	// SetLogger") on any path that runs before the manager prints its
	// startup banner.
	ctrllog.SetLogger(zapr.NewLogger(logger.Desugar()))

	v := version.GetVersion()
	logger.Infow("Starting kynomesh controller",
		"version", v.Version,
		"buildDate", v.BuildDate,
		"gitCommit", v.GitCommit,
		"gitTreeState", v.GitTreeState,
		"goVersion", v.GoVersion,
		"platform", v.Platform,
	)

	leaseDuration, renewDeadline, retryPeriod, err := resolveLeaderElectionTimings()
	if err != nil {
		logger.Fatalw("invalid leader election timings", "err", err)
	}

	opts := ctrl.Options{
		Scheme:                 buildScheme(logger),
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         !sharedutil.LookupEnvBoolOr(kmv1.EnvLeaderElectionDisabled, false),
		LeaderElectionID:       leaderElectionID,
		LeaseDuration:          &leaseDuration,
		RenewDeadline:          &renewDeadline,
		RetryPeriod:            &retryPeriod,
	}
	if namespaced {
		opts.Cache = cache.Options{
			DefaultNamespaces: map[string]cache.Config{managedNamespace: {}},
		}
	}

	cfg := ctrl.GetConfigOrDie()

	brokerImage, err := discoverControllerImage(cfg)
	if err != nil {
		logger.Fatalw("failed to discover controller image for broker sidecar", "err", err)
	}
	logger.Infow("discovered broker sidecar image", "image", brokerImage)

	mgr, err := ctrl.NewManager(cfg, opts)
	if err != nil {
		logger.Fatalw("failed to create controller manager", "err", err)
	}

	if err := mgr.AddReadyzCheck("readiness", healthz.Ping); err != nil {
		logger.Fatalw("failed to register readiness check", "err", err)
	}
	if err := mgr.AddHealthzCheck("liveness", healthz.Ping); err != nil {
		logger.Fatalw("failed to register liveness check", "err", err)
	}

	if err := registerAgentSetController(mgr, logger); err != nil {
		logger.Fatalw("failed to register AgentSet controller", "err", err)
	}
	if err := registerAgentDeployController(mgr, logger, brokerImage); err != nil {
		logger.Fatalw("failed to register AgentDeploy controller", "err", err)
	}

	logger.Infow("Starting controller-manager",
		"namespaced", namespaced,
		"managedNamespace", managedNamespace,
		"leaderElection", opts.LeaderElection,
		"leaseDuration", leaseDuration,
		"renewDeadline", renewDeadline,
		"retryPeriod", retryPeriod,
		"metricsBindAddress", opts.Metrics.BindAddress,
		"healthProbeBindAddress", opts.HealthProbeBindAddress,
		"brokerImage", brokerImage,
	)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.Fatalw("controller manager exited with error", "err", err)
	}
}

// resolveLeaderElectionTimings reads the lease-duration / renew-deadline /
// retry-period env vars, parses each via time.ParseDuration, falls back to
// the client-go defaults on missing values.
func resolveLeaderElectionTimings() (lease, renew, retry time.Duration, err error) {
	type entry struct {
		envKey string
		def    time.Duration
		out    *time.Duration
	}
	entries := []entry{
		{kmv1.EnvLeaderElectionLeaseDuration, defaultLeaseDuration, &lease},
		{kmv1.EnvLeaderElectionRenewDeadline, defaultRenewDeadline, &renew},
		{kmv1.EnvLeaderElectionRetryPeriod, defaultRetryPeriod, &retry},
	}
	for _, e := range entries {
		raw := sharedutil.LookupEnvStringOr(e.envKey, "")
		if raw == "" {
			*e.out = e.def
			continue
		}
		d, parseErr := time.ParseDuration(raw)
		if parseErr != nil {
			return 0, 0, 0, fmt.Errorf("invalid %s=%q: %w", e.envKey, raw, parseErr)
		}
		*e.out = d
	}
	if retry >= renew || renew >= lease {
		return 0, 0, 0, fmt.Errorf(
			"leader election timings must satisfy retry < renew < lease (got retry=%s renew=%s lease=%s)",
			retry, renew, lease,
		)
	}
	return lease, renew, retry, nil
}

// registerAgentSetController wires the AgentSet reconciler into the manager.
// It is constructed with controller.New(), then watches are attached explicitly
// so the choice of handler and predicate per source is visible at the call site rather than
// hidden behind a builder DSL.
//
// Watches:
//
//   - AgentSet (primary): enqueue self on spec (Generation) or label changes.
//     Status-only updates are filtered out — the reconciler writes status
//     itself and shouldn't trigger its own re-runs.
//
//   - AgentDeploy (owned): enqueue the controlling AgentSet on any meaningful
//     change to a child. ResourceVersionChangedPredicate is the default for
//     owned resources because we care about status flips, not just spec.
func registerAgentSetController(mgr manager.Manager, logger *zap.SugaredLogger) error {
	r := agentset.NewReconciler(
		mgr.GetClient(),
		mgr.GetScheme(),
		logger.Named(kmv1.ControllerAgentSet),
		mgr.GetEventRecorder(kmv1.ControllerAgentSet),
	)

	c, err := controller.New(kmv1.ControllerAgentSet, mgr, controller.Options{
		Reconciler:              r,
		MaxConcurrentReconciles: 1,
	})
	if err != nil {
		return err
	}

	if err := c.Watch(source.Kind(
		mgr.GetCache(),
		&kmv1.AgentSet{},
		&handler.TypedEnqueueRequestForObject[*kmv1.AgentSet]{},
		predicate.Or(
			predicate.TypedGenerationChangedPredicate[*kmv1.AgentSet]{},
			predicate.TypedLabelChangedPredicate[*kmv1.AgentSet]{},
		),
	)); err != nil {
		return err
	}

	if err := c.Watch(source.Kind(
		mgr.GetCache(),
		&kmv1.AgentDeploy{},
		handler.TypedEnqueueRequestForOwner[*kmv1.AgentDeploy](
			mgr.GetScheme(),
			mgr.GetRESTMapper(),
			&kmv1.AgentSet{},
			handler.OnlyControllerOwner(),
		),
		predicate.TypedResourceVersionChangedPredicate[*kmv1.AgentDeploy]{},
	)); err != nil {
		return err
	}
	return nil
}

// registerAgentDeployController wires the AgentDeploy reconciler into the
// manager. Same pattern as registerAgentSetController — controller.New
// plus explicit Watch calls — so the per-source handler / predicate choice
// is visible at the call site.
//
// Watches:
//
//   - AgentDeploy (primary): enqueue self on spec or label changes.
//
//   - Pod (owned): enqueue the controlling AgentDeploy on any meaningful
//     change. We care about phase / Ready condition flips, not just spec,
//     so ResourceVersionChangedPredicate is the right filter.
//
//   - Service (owned): enqueue the controlling AgentDeploy if the headless
//     service is mutated or deleted out from under us.
func registerAgentDeployController(mgr manager.Manager, logger *zap.SugaredLogger, brokerImage string) error {
	r := agentdeploy.NewReconciler(
		mgr.GetClient(),
		mgr.GetScheme(),
		logger.Named(kmv1.ControllerAgentDeploy),
		mgr.GetEventRecorder(kmv1.ControllerAgentDeploy),
		brokerImage,
	)

	c, err := controller.New(kmv1.ControllerAgentDeploy, mgr, controller.Options{
		Reconciler:              r,
		MaxConcurrentReconciles: 1,
	})
	if err != nil {
		return err
	}

	if err := c.Watch(source.Kind(
		mgr.GetCache(),
		&kmv1.AgentDeploy{},
		&handler.TypedEnqueueRequestForObject[*kmv1.AgentDeploy]{},
		predicate.Or(
			predicate.TypedGenerationChangedPredicate[*kmv1.AgentDeploy]{},
			predicate.TypedLabelChangedPredicate[*kmv1.AgentDeploy]{},
		),
	)); err != nil {
		return err
	}

	if err := c.Watch(source.Kind(
		mgr.GetCache(),
		&corev1.Pod{},
		handler.TypedEnqueueRequestForOwner[*corev1.Pod](
			mgr.GetScheme(),
			mgr.GetRESTMapper(),
			&kmv1.AgentDeploy{},
			handler.OnlyControllerOwner(),
		),
		predicate.TypedResourceVersionChangedPredicate[*corev1.Pod]{},
	)); err != nil {
		return err
	}

	if err := c.Watch(source.Kind(
		mgr.GetCache(),
		&corev1.Service{},
		handler.TypedEnqueueRequestForOwner[*corev1.Service](
			mgr.GetScheme(),
			mgr.GetRESTMapper(),
			&kmv1.AgentDeploy{},
			handler.OnlyControllerOwner(),
		),
		predicate.TypedResourceVersionChangedPredicate[*corev1.Service]{},
	)); err != nil {
		return err
	}
	return nil
}

// buildScheme returns a runtime.Scheme with the core Kubernetes types and
// the kynomesh CRDs registered. Built as a helper so failure to add a type
// panics loudly at startup rather than silently dropping watches.
func buildScheme(logger *zap.SugaredLogger) *runtime.Scheme {
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		logger.Fatalw("failed to register kubernetes core types", "err", err)
	}
	if err := kmv1.AddToScheme(s); err != nil {
		logger.Fatalw("failed to register kynomesh types", "err", err)
	}
	return s
}

// discoverControllerImage reads the controller's own pod from the API server
// and returns the image of the controller-manager container.
func discoverControllerImage(cfg *rest.Config) (string, error) {
	podName := os.Getenv(kmv1.EnvPodName)
	if podName == "" {
		return "", fmt.Errorf("env %s is not set; the controller Deployment must expose it via downward API", kmv1.EnvPodName)
	}
	podNamespace := os.Getenv(kmv1.EnvNamespace)
	if podNamespace == "" {
		return "", fmt.Errorf("env %s is not set; the controller Deployment must expose it via downward API", kmv1.EnvNamespace)
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return "", fmt.Errorf("failed to build kubernetes clientset: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), imageDiscoveryTimeout)
	defer cancel()

	pod, err := clientset.CoreV1().Pods(podNamespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get own pod %s/%s: %w", podNamespace, podName, err)
	}
	return controllerImageFromPod(pod)
}

// controllerImageFromPod picks the image of the controller-manager container
// out of a pod spec. If the pod has a single container we accept it; if
// multiple, we look for one named kmv1.ControllerContainerName.
func controllerImageFromPod(pod *corev1.Pod) (string, error) {
	containers := pod.Spec.Containers
	if len(containers) == 0 {
		return "", fmt.Errorf("pod %s/%s has no containers", pod.Namespace, pod.Name)
	}
	if len(containers) == 1 {
		if containers[0].Image == "" {
			return "", fmt.Errorf("pod %s/%s container %q has empty image", pod.Namespace, pod.Name, containers[0].Name)
		}
		return containers[0].Image, nil
	}
	for _, c := range containers {
		if c.Name == kmv1.ContainerNameController {
			if c.Image == "" {
				return "", fmt.Errorf("pod %s/%s container %q has empty image", pod.Namespace, pod.Name, c.Name)
			}
			return c.Image, nil
		}
	}
	return "", fmt.Errorf("pod %s/%s has no container named %q", pod.Namespace, pod.Name, kmv1.ContainerNameController)
}
