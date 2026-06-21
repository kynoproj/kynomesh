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

// Package agentset implements the controller that reconciles AgentSet
// resources and orchestrates the corresponding child resources.
package agentset

import (
	"context"
	"fmt"
	"reflect"
	"slices"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
	"github.com/kynoproj/kynomesh/pkg/reconciler/validator"
	"github.com/kynoproj/kynomesh/pkg/shared/logging"
)

const (
	FinalizerName = "kynomesh.kyno.sh/" + kmv1.ControllerAgentSet
)

// Reconciler implements sigs.k8s.io/controller-runtime/pkg/reconcile.Reconciler
// for AgentSet.
type Reconciler struct {
	client.Client
	scheme          *runtime.Scheme
	logger          *zap.SugaredLogger
	recorder        events.EventRecorder
	image           string
	imagePullPolicy corev1.PullPolicy
}

// NewReconciler returns a Reconciler bound to the supplied controller-runtime
// client and scheme. image and imagePullPolicy are used to provision the
// per-AgentSet metrics daemon Deployment.
func NewReconciler(c client.Client, scheme *runtime.Scheme, logger *zap.SugaredLogger, recorder events.EventRecorder, image string, imagePullPolicy corev1.PullPolicy) *Reconciler {
	if logger == nil {
		logger = logging.NewLogger().Named(kmv1.ControllerAgentSet)
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
	var original kmv1.AgentSet
	if err := r.Get(ctx, req.NamespacedName, &original); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		r.logger.Errorw("Unable to get AgentSet", zap.Any("request", req), zap.Error(err))
		return ctrl.Result{}, fmt.Errorf("failed to get AgentSet: %w", err)
	}

	log := r.logger.With("namespace", req.Namespace).With("agentSet", original.Name)
	ctx = logging.WithLogger(ctx, log)

	as := original.DeepCopy()
	reconcileErr := r.reconcile(ctx, as)

	if statusErr := r.persist(ctx, &original, as); statusErr != nil {
		if reconcileErr == nil {
			return ctrl.Result{}, statusErr
		}
		log.Warnw("Failed to persist AgentSet updates", zap.Error(statusErr))
	}
	if reconcileErr != nil {
		log.Errorw("Failed to reconcile AgentSet", zap.Error(reconcileErr))
		return ctrl.Result{}, reconcileErr
	}
	return ctrl.Result{}, nil
}

// reconcile is the inner reconciliation that mutates the provided AgentSet
// copy. It orchestrates the per-component reconcilers in agentdeploys.go,
// entry_service.go, and daemon.go.
func (r *Reconciler) reconcile(ctx context.Context, as *kmv1.AgentSet) error {
	as.Status.InitializeConditions(
		kmv1.AgentSetConditionConfigured,
		kmv1.AgentSetConditionDeployed,
		kmv1.AgentSetConditionAgentsHealthy,
	)
	as.Status.ObservedGeneration = as.Generation

	if !as.DeletionTimestamp.IsZero() {
		as.Status.Phase = kmv1.AgentSetPhaseDeleting
		if err := r.deleteChildren(ctx, as); err != nil {
			return fmt.Errorf("failed to delete child AgentDeploys: %w", err)
		}
		if err := r.deleteEntryService(ctx, as); err != nil {
			return fmt.Errorf("failed to delete entry service: %w", err)
		}
		if err := r.deleteDaemon(ctx, as); err != nil {
			return fmt.Errorf("failed to delete daemon: %w", err)
		}
		removeFinalizer(as)
		return nil
	}

	addFinalizer(as)

	if err := validator.ValidateAgentSet(as); err != nil {
		as.Status.MarkFalse(kmv1.AgentSetConditionConfigured, "InvalidSpec", err.Error())
		as.Status.Phase = kmv1.AgentSetPhaseFailed
		as.Status.Message = err.Error()
		return nil
	}
	as.Status.MarkTrue(kmv1.AgentSetConditionConfigured)

	existing, err := r.listOwnedAgentDeploys(ctx, as)
	if err != nil {
		return fmt.Errorf("failed to list child AgentDeploys: %w", err)
	}

	desired, err := r.buildDesired(as)
	if err != nil {
		as.Status.MarkFalse(kmv1.AgentSetConditionConfigured, "BuildFailed", err.Error())
		as.Status.Phase = kmv1.AgentSetPhaseFailed
		as.Status.Message = err.Error()
		return nil
	}

	if err := r.applyDesiredState(ctx, as, existing, desired); err != nil {
		as.Status.MarkFalse(kmv1.AgentSetConditionDeployed, "DeployFailed", err.Error())
		as.Status.Phase = kmv1.AgentSetPhaseFailed
		as.Status.Message = err.Error()
		return err
	}
	if err := r.reconcileEntryService(ctx, as); err != nil {
		as.Status.MarkFalse(kmv1.AgentSetConditionDeployed, "EntryServiceFailed", err.Error())
		as.Status.Phase = kmv1.AgentSetPhaseFailed
		as.Status.Message = err.Error()
		return err
	}
	if err := r.reconcileDaemon(ctx, as); err != nil {
		as.Status.MarkFalse(kmv1.AgentSetConditionDeployed, "DaemonFailed", err.Error())
		as.Status.Phase = kmv1.AgentSetPhaseFailed
		as.Status.Message = err.Error()
		return err
	}
	as.Status.MarkTrue(kmv1.AgentSetConditionDeployed)

	r.aggregateChildHealth(as, desired, existing)
	count := uint32(len(desired))
	as.Status.AgentCount = &count
	as.Status.LastUpdated = metav1.Now()
	return nil
}

// persist writes finalizer and status changes back to the API server.
func (r *Reconciler) persist(ctx context.Context, original, updated *kmv1.AgentSet) error {
	finalizersChanged := !reflect.DeepEqual(original.Finalizers, updated.Finalizers)
	statusChanged := !reflect.DeepEqual(original.Status, updated.Status)

	if statusChanged {
		statusPatch := client.MergeFrom(original.DeepCopy())
		// Build the patch object: same identity as original, but with the
		// updated status. Using a fresh copy avoids ResourceVersion drift
		// caused by an in-flight finalizer patch below.
		patchObj := original.DeepCopy()
		patchObj.Status = updated.Status
		if err := r.Status().Patch(ctx, patchObj, statusPatch); err != nil {
			return fmt.Errorf("failed to patch status: %w", err)
		}
	}

	if finalizersChanged {
		metaPatch := client.MergeFrom(original.DeepCopy())
		patchObj := original.DeepCopy()
		patchObj.Finalizers = updated.Finalizers
		if err := r.Patch(ctx, patchObj, metaPatch); err != nil {
			return fmt.Errorf("failed to patch finalizers: %w", err)
		}
	}
	return nil
}

func addFinalizer(as *kmv1.AgentSet) {
	if slices.Contains(as.Finalizers, FinalizerName) {
		return
	}
	as.Finalizers = append(as.Finalizers, FinalizerName)
}

func removeFinalizer(as *kmv1.AgentSet) {
	out := as.Finalizers[:0]
	for _, f := range as.Finalizers {
		if f != FinalizerName {
			out = append(out, f)
		}
	}
	as.Finalizers = out
}

// Reference assertion to surface signature errors at compile time rather
// than only when SetupWithManager is invoked at runtime.
var _ reconcile.Reconciler = (*Reconciler)(nil)
