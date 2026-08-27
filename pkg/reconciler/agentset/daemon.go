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

package agentset

import (
	"context"
	"encoding/json"
	"fmt"

	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
	"github.com/kynoproj/kynomesh/pkg/shared/logging"
	sharedutil "github.com/kynoproj/kynomesh/pkg/shared/util"
)

// daemonReplicas is fixed at 1 by design: the daemon is a singleton
// per AgentSet with in-memory state and no need for HA.
const daemonReplicas int32 = 1

const daemonProbeInitialDelaySec int32 = 5

// newDaemonDeployment builds the Deployment that runs the per-
// AgentSet metrics daemon.
func (r *Reconciler) newDaemonDeployment(as *kmv1.AgentSet) (*appsv1.Deployment, error) {
	agentNames := agentDeployNamesFromSpec(as)
	encodedAgents, err := json.Marshal(agentNames)
	if err != nil {
		return nil, fmt.Errorf("marshal agentdeploys: %w", err)
	}

	labels := daemonLabels(as)
	podLabels := daemonLabels(as)
	var tmpl *kmv1.DaemonTemplate
	if t := as.Spec.Templates; t != nil {
		tmpl = t.DaemonTemplate
	}

	var cTmpl *kmv1.ContainerTemplate
	if tmpl != nil {
		cTmpl = tmpl.Container
	}

	replicas := daemonReplicas
	defaultResources := r.config.GetDefaults().GetDefaultContainerResources()
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   as.Namespace,
			Name:        as.DaemonName(),
			Labels:      labels,
			Annotations: map[string]string{},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RecreateDeploymentStrategyType,
			},
			Selector: &metav1.LabelSelector{MatchLabels: podLabels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: podLabels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						newDaemonContainer(r.image, r.imagePullPolicy, as, string(encodedAgents), cTmpl, defaultResources),
					},
				},
			},
		},
	}
	if tmpl != nil {
		tmpl.ApplyToPodTemplateSpec(&dep.Spec.Template)
	}
	if r.scheme != nil {
		if err := ctrl.SetControllerReference(as, dep, r.scheme); err != nil {
			return nil, fmt.Errorf("set controller reference on daemon Deployment: %w", err)
		}
	} else {
		dep.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(as, kmv1.AgentSetGroupVersionKind)}
	}
	return dep, nil
}

// newDaemonService builds the ClusterIP Service in front of the
// daemon Pod.
func (r *Reconciler) newDaemonService(as *kmv1.AgentSet) (*corev1.Service, error) {
	labels := daemonLabels(as)
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   as.Namespace,
			Name:        as.DaemonName(),
			Labels:      labels,
			Annotations: map[string]string{},
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: labels,
			Ports: []corev1.ServicePort{
				{
					Name:       "api",
					Port:       kmv1.DaemonAPIPort,
					TargetPort: intstr.FromString("api"),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "metrics",
					Port:       kmv1.DaemonMetricsPort,
					TargetPort: intstr.FromString("metrics"),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
	if r.scheme != nil {
		if err := ctrl.SetControllerReference(as, svc, r.scheme); err != nil {
			return nil, fmt.Errorf("set controller reference on daemon Service: %w", err)
		}
	} else {
		svc.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(as, kmv1.AgentSetGroupVersionKind)}
	}
	return svc, nil
}

// newDaemonContainer builds the single container that runs the
// daemon binary.
func newDaemonContainer(image string, pullPolicy corev1.PullPolicy, as *kmv1.AgentSet, encodedAgents string, tmpl *kmv1.ContainerTemplate, defaultResources corev1.ResourceRequirements) corev1.Container {
	probe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path:   "/healthz",
				Port:   intstr.FromString("metrics"),
				Scheme: corev1.URISchemeHTTPS,
			},
		},
		InitialDelaySeconds: daemonProbeInitialDelaySec,
		PeriodSeconds:       10,
		TimeoutSeconds:      2,
		FailureThreshold:    3,
		SuccessThreshold:    1,
	}
	c := corev1.Container{
		Name:            kmv1.ContainerNameDaemon,
		Image:           image,
		ImagePullPolicy: pullPolicy,
		Args:            []string{"daemon"},
		Env: []corev1.EnvVar{
			{Name: kmv1.EnvNamespace, ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
			}},
			{Name: kmv1.EnvPodName, ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
			}},
			{Name: kmv1.EnvAgentSetName, Value: as.Name},
			{Name: kmv1.EnvAgentSetAgentDeploys, Value: encodedAgents},
		},
		Ports: []corev1.ContainerPort{
			{Name: "api", ContainerPort: kmv1.DaemonAPIPort, Protocol: corev1.ProtocolTCP},
			{Name: "metrics", ContainerPort: kmv1.DaemonMetricsPort, Protocol: corev1.ProtocolTCP},
		},
		ReadinessProbe: probe,
		LivenessProbe:  probe.DeepCopy(),
	}
	if tmpl != nil {
		tmpl.ApplyToContainer(&c)
	}
	kmv1.ApplyDefaultResources(&c, defaultResources)
	return c
}

// daemonLabels returns the labels common to the daemon's Deployment,
// Service, and Pod template.
func daemonLabels(as *kmv1.AgentSet) map[string]string {
	return map[string]string{
		kmv1.KeyAppName:      as.DaemonName(),
		kmv1.KeyAgentSetName: as.Name,
		kmv1.KeyComponent:    kmv1.ComponentDaemon,
		kmv1.KeyPartOf:       kmv1.Project,
		kmv1.KeyManagedBy:    kmv1.ControllerAgentSet,
	}
}

// agentDeployNamesFromSpec returns the AgentDeploy child names that
// the AgentSet will own.
func agentDeployNamesFromSpec(as *kmv1.AgentSet) []string {
	out := make([]string, 0, len(as.Spec.Agents))
	for _, a := range as.Spec.Agents {
		out = append(out, as.ChildAgentDeployName(a.Name))
	}
	return out
}

// reconcileDaemon ensures the per-AgentSet metrics daemon Deployment
// and Service exist with the expected spec.
func (r *Reconciler) reconcileDaemon(ctx context.Context, as *kmv1.AgentSet) error {
	if err := r.reconcileDaemonDeployment(ctx, as); err != nil {
		return err
	}
	return r.reconcileDaemonService(ctx, as)
}

func (r *Reconciler) reconcileDaemonDeployment(ctx context.Context, as *kmv1.AgentSet) error {
	log := logging.FromContext(ctx)
	desired, err := r.newDaemonDeployment(as)
	if err != nil {
		return fmt.Errorf("build daemon Deployment: %w", err)
	}
	desiredHash := sharedutil.MustHash(desired.Spec)
	desired.Annotations[kmv1.KeyHash] = desiredHash

	var existing appsv1.Deployment
	getErr := r.Get(ctx, client.ObjectKey{Namespace: desired.Namespace, Name: desired.Name}, &existing)
	if apierrors.IsNotFound(getErr) {
		if createErr := r.Create(ctx, desired); createErr != nil && !apierrors.IsAlreadyExists(createErr) {
			log.Errorw("Failed to create daemon Deployment", zap.String("deploymentName", desired.Name), zap.Error(createErr))
			return fmt.Errorf("create daemon Deployment: %w", createErr)
		}
		log.Infow("Created daemon Deployment", zap.String("deploymentName", desired.Name))
		r.recorder.Eventf(as, nil, corev1.EventTypeNormal, "CreatedDaemon", "CreateDaemon", "Created daemon Deployment %s", desired.Name)
		return nil
	}
	if getErr != nil {
		return fmt.Errorf("get daemon Deployment: %w", getErr)
	}
	if existing.Annotations[kmv1.KeyHash] == desiredHash {
		return nil
	}
	// Spec drifted — recreate. We delete first rather than patching
	// because the Recreate strategy guarantees the old Pod is gone
	// before the new one starts, and that's enforced naturally by
	// destroy/create rather than mutation.
	if err := r.Delete(ctx, &existing); err != nil && !apierrors.IsNotFound(err) {
		log.Errorw("Failed to delete stale daemon Deployment", zap.String("deploymentName", desired.Name), zap.Error(err))
		return fmt.Errorf("delete stale daemon Deployment: %w", err)
	}
	log.Infow("Deleted stale daemon Deployment", zap.String("deploymentName", desired.Name))
	if err := r.Create(ctx, desired); err != nil && !apierrors.IsAlreadyExists(err) {
		log.Errorw("Failed to recreate daemon Deployment", zap.String("deploymentName", desired.Name), zap.Error(err))
		return fmt.Errorf("recreate daemon Deployment: %w", err)
	}
	log.Infow("Recreated daemon Deployment", zap.String("deploymentName", desired.Name))
	r.recorder.Eventf(as, nil, corev1.EventTypeNormal, "UpdatedDaemon", "UpdateDaemon", "Recreated daemon Deployment %s on spec drift", desired.Name)
	return nil
}

func (r *Reconciler) reconcileDaemonService(ctx context.Context, as *kmv1.AgentSet) error {
	log := logging.FromContext(ctx)
	desired, err := r.newDaemonService(as)
	if err != nil {
		return fmt.Errorf("build daemon Service: %w", err)
	}
	desiredHash := sharedutil.MustHash(desired.Spec)
	desired.Annotations[kmv1.KeyHash] = desiredHash

	var existing corev1.Service
	getErr := r.Get(ctx, client.ObjectKey{Namespace: desired.Namespace, Name: desired.Name}, &existing)
	if apierrors.IsNotFound(getErr) {
		if createErr := r.Create(ctx, desired); createErr != nil && !apierrors.IsAlreadyExists(createErr) {
			log.Errorw("Failed to create daemon Service", zap.String("serviceName", desired.Name), zap.Error(createErr))
			return fmt.Errorf("create daemon Service: %w", createErr)
		}
		log.Infow("Created daemon Service", zap.String("serviceName", desired.Name))
		return nil
	}
	if getErr != nil {
		return fmt.Errorf("get daemon Service: %w", getErr)
	}
	if existing.Annotations[kmv1.KeyHash] == desiredHash {
		return nil
	}
	if err := r.Delete(ctx, &existing); err != nil && !apierrors.IsNotFound(err) {
		log.Errorw("Failed to delete stale daemon Service", zap.String("serviceName", desired.Name), zap.Error(err))
		return fmt.Errorf("delete stale daemon Service: %w", err)
	}
	log.Infow("Deleted stale daemon Service", zap.String("serviceName", desired.Name))
	if err := r.Create(ctx, desired); err != nil && !apierrors.IsAlreadyExists(err) {
		log.Errorw("Failed to recreate daemon Service", zap.String("serviceName", desired.Name), zap.Error(err))
		return fmt.Errorf("recreate daemon Service: %w", err)
	}
	log.Infow("Recreated daemon Service", zap.String("serviceName", desired.Name))
	return nil
}
