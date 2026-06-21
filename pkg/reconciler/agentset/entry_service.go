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
	"fmt"

	"go.uber.org/zap"
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

// reconcileEntryService ensures the per-AgentSet entry ClusterIP service
// exists with the expected spec.
func (r *Reconciler) reconcileEntryService(ctx context.Context, as *kmv1.AgentSet) error {
	log := logging.FromContext(ctx)
	desired, err := r.newEntryService(as)
	if err != nil {
		return fmt.Errorf("failed to build entry service: %w", err)
	}
	desiredHash := sharedutil.MustHash(desired.Spec)
	desired.Annotations[kmv1.KeyHash] = desiredHash

	var existing corev1.Service
	getErr := r.Get(ctx, client.ObjectKey{Namespace: desired.Namespace, Name: desired.Name}, &existing)
	if apierrors.IsNotFound(getErr) {
		if createErr := r.Create(ctx, desired); createErr != nil && !apierrors.IsAlreadyExists(createErr) {
			log.Errorw("Failed to create entry service", zap.String("serviceName", desired.Name), zap.Error(createErr))
			return fmt.Errorf("failed to create entry service: %w", createErr)
		}
		log.Infow("Succeeded to create entry service", zap.String("serviceName", desired.Name))
		r.recorder.Eventf(as, nil, corev1.EventTypeNormal, "CreatedEntryService", "CreateEntryService", "Created entry service %s", desired.Name)
		return nil
	}
	if getErr != nil {
		return fmt.Errorf("failed to get entry service: %w", getErr)
	}
	if existing.Annotations[kmv1.KeyHash] == desiredHash {
		return nil
	}
	if err := r.Delete(ctx, &existing); err != nil && !apierrors.IsNotFound(err) {
		log.Errorw("Failed to delete stale entry service", zap.String("serviceName", desired.Name), zap.Error(err))
		return fmt.Errorf("failed to delete stale entry service: %w", err)
	}
	log.Infow("Succeeded to delete stale entry service", zap.String("serviceName", desired.Name))
	if err := r.Create(ctx, desired); err != nil && !apierrors.IsAlreadyExists(err) {
		log.Errorw("Failed to recreate entry service", zap.String("serviceName", desired.Name), zap.Error(err))
		return fmt.Errorf("failed to recreate entry service: %w", err)
	}
	log.Infow("Succeeded to recreate entry service", zap.String("serviceName", desired.Name))
	r.recorder.Eventf(as, nil, corev1.EventTypeNormal, "UpdatedEntryService", "UpdateEntryService", "Recreated entry service %s on spec drift", desired.Name)
	return nil
}

// deleteEntryService removes the entry service if present.
func (r *Reconciler) deleteEntryService(ctx context.Context, as *kmv1.AgentSet) error {
	log := logging.FromContext(ctx)
	var svc corev1.Service
	err := r.Get(ctx, client.ObjectKey{Namespace: as.Namespace, Name: as.EntryServiceName()}, &svc)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get entry service: %w", err)
	}
	if err := r.Delete(ctx, &svc); err != nil && !apierrors.IsNotFound(err) {
		log.Errorw("Failed to delete entry service", zap.String("serviceName", svc.Name), zap.Error(err))
		return fmt.Errorf("failed to delete entry service: %w", err)
	}
	log.Infow("Succeeded to delete entry service", zap.String("serviceName", svc.Name))
	return nil
}

// newEntryService builds the desired ClusterIP service for the AgentSet's
// entry pods.
func (r *Reconciler) newEntryService(as *kmv1.AgentSet) (*corev1.Service, error) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: as.Namespace,
			Name:      as.EntryServiceName(),
			Labels: map[string]string{
				kmv1.KeyAppName:      as.Name,
				kmv1.KeyAgentSetName: as.Name,
				kmv1.KeyComponent:    kmv1.ComponentAgent,
				kmv1.KeyPartOf:       kmv1.Project,
				kmv1.KeyManagedBy:    kmv1.ControllerAgentSet,
			},
			Annotations: map[string]string{},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				kmv1.KeyAgentSetName: as.Name,
				kmv1.KeyManagedBy:    kmv1.ControllerAgentDeploy,
				kmv1.KeyEntry:        "true",
				kmv1.KeyServing:      "true",
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
	if r.scheme != nil {
		if err := ctrl.SetControllerReference(as, svc, r.scheme); err != nil {
			return nil, fmt.Errorf("failed to set controller reference: %w", err)
		}
	} else {
		svc.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(as, kmv1.AgentSetGroupVersionKind)}
	}
	return svc, nil
}
