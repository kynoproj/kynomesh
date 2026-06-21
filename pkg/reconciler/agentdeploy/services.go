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

package agentdeploy

import (
	"context"
	"fmt"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
	"github.com/kynoproj/kynomesh/pkg/shared/logging"
	sharedutil "github.com/kynoproj/kynomesh/pkg/shared/util"
)

// reconcileServices ensures both per-deploy services exist with the expected.
func (r *Reconciler) reconcileServices(ctx context.Context, ad *kmv1.AgentDeploy) error {
	if err := r.upsertService(ctx, ad, newHeadlessService(ad), "headless"); err != nil {
		return err
	}
	return r.upsertService(ctx, ad, newClusterIPService(ad), "ClusterIP")
}

// upsertService creates desired if absent, no-ops if the existing spec hash
// matches, otherwise delete-and-recreates.
func (r *Reconciler) upsertService(ctx context.Context, ad *kmv1.AgentDeploy, desired *corev1.Service, kind string) error {
	log := logging.FromContext(ctx)
	desiredHash := sharedutil.MustHash(desired.Spec)
	desired.Annotations[kmv1.KeyHash] = desiredHash

	var existing corev1.Service
	err := r.Get(ctx, client.ObjectKey{Namespace: desired.Namespace, Name: desired.Name}, &existing)
	if apierrors.IsNotFound(err) {
		if createErr := r.Create(ctx, desired); createErr != nil && !apierrors.IsAlreadyExists(createErr) {
			log.Errorw("Failed to create "+kind+" service", zap.String("serviceName", desired.Name), zap.Error(createErr))
			return fmt.Errorf("failed to create %s service: %w", kind, createErr)
		}
		log.Infow("Succeeded to create "+kind+" service", zap.String("serviceName", desired.Name))
		r.recorder.Eventf(ad, nil, corev1.EventTypeNormal, "CreatedService", "CreateService", "Created %s service %s", kind, desired.Name)
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get %s service: %w", kind, err)
	}
	if existing.Annotations[kmv1.KeyHash] == desiredHash {
		return nil
	}
	if err := r.Delete(ctx, &existing); err != nil && !apierrors.IsNotFound(err) {
		log.Errorw("Failed to delete stale "+kind+" service", zap.String("serviceName", desired.Name), zap.Error(err))
		return fmt.Errorf("failed to delete stale %s service: %w", kind, err)
	}
	log.Infow("Succeeded to delete stale "+kind+" service", zap.String("serviceName", desired.Name))
	if err := r.Create(ctx, desired); err != nil && !apierrors.IsAlreadyExists(err) {
		log.Errorw("Failed to recreate "+kind+" service", zap.String("serviceName", desired.Name), zap.Error(err))
		return fmt.Errorf("failed to recreate %s service: %w", kind, err)
	}
	log.Infow("Succeeded to recreate "+kind+" service", zap.String("serviceName", desired.Name))
	r.recorder.Eventf(ad, nil, corev1.EventTypeNormal, "UpdatedService", "UpdateService", "Recreated %s service %s on spec drift", kind, desired.Name)
	return nil
}

// newHeadlessService builds the per-deploy ClusterIP=None service. Each
// pod with matching labels gets a DNS record at
// "<deploy>-<idx>.<deploy>-headless.<ns>.svc.cluster.local".
func newHeadlessService(ad *kmv1.AgentDeploy) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ad.Namespace,
			Name:      ad.HeadlessServiceName(),
			Labels: map[string]string{
				kmv1.KeyAppName:         ad.Name,
				kmv1.KeyAgentSetName:    ad.Spec.AgentSetName,
				kmv1.KeyAgentDeployName: ad.Spec.Name,
				kmv1.KeyComponent:       kmv1.ComponentAgent,
				kmv1.KeyPartOf:          kmv1.Project,
				kmv1.KeyManagedBy:       kmv1.ControllerAgentDeploy,
			},
			Annotations:     map[string]string{},
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(ad, kmv1.AgentDeployGroupVersionKind)},
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: corev1.ClusterIPNone,
			Selector: map[string]string{
				kmv1.KeyAgentSetName:    ad.Spec.AgentSetName,
				kmv1.KeyAgentDeployName: ad.Spec.Name,
				kmv1.KeyManagedBy:       kmv1.ControllerAgentDeploy,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "introspect",
					Port:       kmv1.AgentBrokerIntrospectionPort,
					TargetPort: intstr.FromString("introspect"),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			// Pods get a DNS A record without waiting for readiness.
			PublishNotReadyAddresses: true,
		},
	}
}

// newClusterIPService builds the per-deploy normal ClusterIP service.
func newClusterIPService(ad *kmv1.AgentDeploy) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ad.Namespace,
			Name:      ad.ServiceName(),
			Labels: map[string]string{
				kmv1.KeyAppName:         ad.Name,
				kmv1.KeyAgentSetName:    ad.Spec.AgentSetName,
				kmv1.KeyAgentDeployName: ad.Spec.Name,
				kmv1.KeyComponent:       kmv1.ComponentAgent,
				kmv1.KeyPartOf:          kmv1.Project,
				kmv1.KeyManagedBy:       kmv1.ControllerAgentDeploy,
			},
			Annotations:     map[string]string{},
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(ad, kmv1.AgentDeployGroupVersionKind)},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				kmv1.KeyAgentSetName:    ad.Spec.AgentSetName,
				kmv1.KeyAgentDeployName: ad.Spec.Name,
				kmv1.KeyManagedBy:       kmv1.ControllerAgentDeploy,
				kmv1.KeyServing:         "true",
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "broker",
					Port:       kmv1.AgentBrokerPort,
					TargetPort: intstr.FromString("broker"),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}
