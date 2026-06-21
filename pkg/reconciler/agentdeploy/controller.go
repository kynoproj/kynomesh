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

// Package agentdeploy implements the controller that reconciles AgentDeploy
// resources.
//
// Deletion: all children (Pods, headless Service, ClusterIP Service)
// carry owner references to the AgentDeploy, so Kubernetes' built-in
// cascading garbage collection cleans them up when the AgentDeploy is
// removed. The reconciler does not register a finalizer.
package agentdeploy

import (
	"context"
	"fmt"
	"reflect"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
	"github.com/kynoproj/kynomesh/pkg/shared/logging"
)

// Reconciler implements controller-runtime's reconcile.Reconciler.
type Reconciler struct {
	client.Client
	scheme          *runtime.Scheme
	logger          *zap.SugaredLogger
	recorder        events.EventRecorder
	image           string
	imagePullPolicy corev1.PullPolicy
}

// NewReconciler returns a Reconciler bound to the supplied client and scheme.
func NewReconciler(c client.Client, scheme *runtime.Scheme, logger *zap.SugaredLogger, recorder events.EventRecorder, image string, imagePullPolicy corev1.PullPolicy) *Reconciler {
	if logger == nil {
		logger = logging.NewLogger().Named(kmv1.ControllerAgentDeploy)
	}
	if recorder == nil {
		recorder = noopRecorder{}
	}
	return &Reconciler{
		Client:          c,
		scheme:          scheme,
		logger:          logger,
		recorder:        recorder,
		image:           image,
		imagePullPolicy: imagePullPolicy,
	}
}

// Reconcile is the controller-runtime entry point.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var original kmv1.AgentDeploy
	if err := r.Get(ctx, req.NamespacedName, &original); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		r.logger.Errorw("Unable to get AgentDeploy", zap.Any("request", req), zap.Error(err))
		return ctrl.Result{}, fmt.Errorf("failed to get AgentDeploy: %w", err)
	}

	// Mid-deletion the API server keeps the object alive only briefly
	// during cascading GC. Skip — there's nothing controller-side to
	// do and any patches would race against deletion.
	if !original.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	log := r.logger.With("namespace", req.Namespace).With("agentSet", original.Spec.AgentSetName).
		With("agentDeploy", original.Name)
	ctx = logging.WithLogger(ctx, log)

	ad := original.DeepCopy()
	reconcileErr := r.reconcile(ctx, ad)

	if persistErr := r.persist(ctx, &original, ad); persistErr != nil {
		if reconcileErr == nil {
			return ctrl.Result{}, persistErr
		}
		log.Warnw("Failed to persist AgentDeploy updates", zap.Error(persistErr))
	}
	if reconcileErr != nil {
		log.Errorw("Failed to reconcile AgentSet", zap.Error(reconcileErr))
		return ctrl.Result{}, reconcileErr
	}
	return ctrl.Result{}, nil
}

// reconcile mutates the supplied deep copy. All API writes for children
// happen via the per-component reconcilers in pods.go and services.go.
// The parent is persisted by Reconcile via persist().
func (r *Reconciler) reconcile(ctx context.Context, ad *kmv1.AgentDeploy) error {
	log := logging.FromContext(ctx)
	ad.Status.InitializeConditions(
		kmv1.AgentDeployConditionDeployed,
		kmv1.AgentDeployConditionPodsHealthy,
	)
	ad.Status.ObservedGeneration = ad.Generation

	if err := r.reconcileServices(ctx, ad); err != nil {
		ad.Status.MarkFalse(kmv1.AgentDeployConditionDeployed, "ServiceFailed", err.Error())
		ad.Status.Phase = kmv1.AgentDeployPhaseFailed
		ad.Status.Reason = "ServiceFailed"
		ad.Status.Message = err.Error()
		log.Errorw("Failed to reconcile AgentDeploy services", zap.Error(err))
		return err
	}

	if err := r.reconcilePods(ctx, ad); err != nil {
		ad.Status.MarkFalse(kmv1.AgentDeployConditionDeployed, "PodsFailed", err.Error())
		ad.Status.Phase = kmv1.AgentDeployPhaseFailed
		ad.Status.Reason = "PodsFailed"
		ad.Status.Message = err.Error()
		log.Errorw("Failed to reconcile AgentDeploy pods", zap.Error(err))
		return err
	}
	ad.Status.MarkTrue(kmv1.AgentDeployConditionDeployed)
	ad.Status.Reason = ""
	ad.Status.Message = ""

	if err := r.updatePodStatus(ctx, ad); err != nil {
		return fmt.Errorf("failed to compute pod status: %w", err)
	}
	if ad.Status.DesiredReplicas > 0 && ad.Status.ReadyReplicas == ad.Status.DesiredReplicas {
		ad.Status.Phase = kmv1.AgentDeployPhaseRunning
		ad.Status.MarkTrue(kmv1.AgentDeployConditionPodsHealthy)
	} else {
		ad.Status.Phase = kmv1.AgentDeployPhaseUnknown
		ad.Status.MarkFalse(kmv1.AgentDeployConditionPodsHealthy, "NotAllReady",
			fmt.Sprintf("%d/%d replicas ready", ad.Status.ReadyReplicas, ad.Status.DesiredReplicas))
	}
	return nil
}

// persist writes status changes back to the API server.
func (r *Reconciler) persist(ctx context.Context, original, updated *kmv1.AgentDeploy) error {
	if reflect.DeepEqual(original.Status, updated.Status) {
		return nil
	}
	statusPatch := client.MergeFrom(original.DeepCopy())
	patchObj := original.DeepCopy()
	patchObj.Status = updated.Status
	if err := r.Status().Patch(ctx, patchObj, statusPatch); err != nil {
		return fmt.Errorf("failed to patch status: %w", err)
	}
	return nil
}

type noopRecorder struct{}

func (noopRecorder) Eventf(runtime.Object, runtime.Object, string, string, string, string, ...any) {
}

var _ reconcile.Reconciler = (*Reconciler)(nil)
