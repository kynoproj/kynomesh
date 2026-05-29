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
// resources and orchestrates the corresponding child AgentDeploy resources.
//
// The parent AgentSet describes a logical set of agents; for each entry in
// AgentSet.Spec.Agents, the controller creates / updates a child AgentDeploy
// owned by the parent, and prunes children that are no longer referenced.
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
	sharedutil "github.com/kynoproj/kynomesh/pkg/shared/util"
)

const (
	// FinalizerName guards an AgentSet against deletion until the controller
	// has removed its child AgentDeploy objects.
	FinalizerName = "kynomesh.kyno.sh/" + kmv1.ControllerAgentSet
)

// Reconciler implements sigs.k8s.io/controller-runtime/pkg/reconcile.Reconciler
// for AgentSet. It diffs the desired AgentDeploy set (derived from the
// parent's spec) against the live children (located via ownerReferences and
// the agentset-name label) and creates / updates / deletes as needed.
type Reconciler struct {
	client.Client
	scheme   *runtime.Scheme
	logger   *zap.SugaredLogger
	recorder events.EventRecorder
}

// NewReconciler returns a Reconciler bound to the supplied controller-runtime
// client and scheme.
func NewReconciler(c client.Client, scheme *runtime.Scheme, logger *zap.SugaredLogger, recorder events.EventRecorder) *Reconciler {
	if logger == nil {
		logger = logging.NewLogger().Named(kmv1.ControllerAgentSet)
	}
	return &Reconciler{
		Client:   c,
		scheme:   scheme,
		logger:   logger,
		recorder: recorder,
	}
}

// Reconcile is the controller-runtime entry point. It fetches the AgentSet
// by name, runs the inner reconcile on a deep copy, then persists status and
// finalizer changes back to the API server.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.logger.With("namespace", req.Namespace, "name", req.Name)

	var original kmv1.AgentSet
	if err := r.Get(ctx, req.NamespacedName, &original); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get AgentSet: %w", err)
	}

	as := original.DeepCopy()
	reconcileErr := r.reconcile(ctx, as)

	if statusErr := r.persist(ctx, &original, as); statusErr != nil {
		if reconcileErr == nil {
			return ctrl.Result{}, statusErr
		}
		log.Warnw("failed to persist AgentSet updates", "err", statusErr)
	}
	if reconcileErr != nil {
		return ctrl.Result{}, reconcileErr
	}
	return ctrl.Result{}, nil
}

// reconcile is the inner reconciliation that mutates the provided AgentSet
// copy. It handles finalizer add/remove, deletion cleanup, validation, child
// diffing, and status condition transitions. All API writes happen here for
// children; the parent itself is persisted by Reconcile via persist().
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
	as.Status.MarkTrue(kmv1.AgentSetConditionDeployed)

	r.aggregateChildHealth(as, desired, existing)
	count := uint32(len(desired))
	as.Status.AgentCount = &count
	as.Status.LastUpdated = metav1.Now()
	return nil
}

// applyDesiredState reconciles the live set of AgentDeploys with the desired
// set: create missing children, update those whose spec hash drifted, delete
// orphans.
func (r *Reconciler) applyDesiredState(
	ctx context.Context,
	as *kmv1.AgentSet,
	existing map[string]*kmv1.AgentDeploy,
	desired map[string]*kmv1.AgentDeploy,
) error {
	for name, want := range desired {
		got, ok := existing[name]
		if !ok {
			if err := r.Create(ctx, want); err != nil && !apierrors.IsAlreadyExists(err) {
				return fmt.Errorf("failed to create AgentDeploy %s: %w", name, err)
			}
			r.recorder.Eventf(as, nil, corev1.EventTypeNormal, "CreatedAgentDeploy", "CreateAgentDeploy", "Created AgentDeploy %s", name)
			continue
		}
		if !needsUpdate(got, want) {
			continue
		}
		updated := got.DeepCopy()
		updated.Labels = want.Labels
		updated.Annotations = want.Annotations
		updated.OwnerReferences = want.OwnerReferences
		updated.Spec = want.Spec
		if err := r.Update(ctx, updated); err != nil {
			return fmt.Errorf("failed to update AgentDeploy %s: %w", name, err)
		}
		r.recorder.Eventf(as, nil, corev1.EventTypeNormal, "UpdatedAgentDeploy", "UpdateAgentDeploy", "Updated AgentDeploy %s", name)
	}

	for name, got := range existing {
		if _, keep := desired[name]; keep {
			continue
		}
		if err := r.Delete(ctx, got); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete orphan AgentDeploy %s: %w", name, err)
		}
		r.recorder.Eventf(as, nil, corev1.EventTypeNormal, "DeletedAgentDeploy", "DeleteAgentDeploy", "Deleted orphan AgentDeploy %s", name)
	}
	return nil
}

// deleteChildren removes all AgentDeploys labelled as belonging to this
// AgentSet. Called on the deletion path before the finalizer is dropped.
func (r *Reconciler) deleteChildren(ctx context.Context, as *kmv1.AgentSet) error {
	existing, err := r.listOwnedAgentDeploys(ctx, as)
	if err != nil {
		return err
	}
	for _, ad := range existing {
		if err := r.Delete(ctx, ad); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete AgentDeploy %s during cleanup: %w", ad.Name, err)
		}
	}
	return nil
}

func (r *Reconciler) listOwnedAgentDeploys(ctx context.Context, as *kmv1.AgentSet) (map[string]*kmv1.AgentDeploy, error) {
	var list kmv1.AgentDeployList
	if err := r.List(ctx, &list,
		client.InNamespace(as.Namespace),
		client.MatchingLabels{kmv1.KeyAgentSetName: as.Name},
	); err != nil {
		return nil, err
	}
	out := make(map[string]*kmv1.AgentDeploy, len(list.Items))
	for i := range list.Items {
		ad := &list.Items[i]
		out[ad.Name] = ad
	}
	return out, nil
}

// aggregateChildHealth folds child phases into the parent's Phase/Conditions.
// A set is Running iff every desired child exists and is in AgentDeployPhase
// Running; otherwise it stays Unknown (no desired children) or Failed.
func (r *Reconciler) aggregateChildHealth(
	as *kmv1.AgentSet,
	desired map[string]*kmv1.AgentDeploy,
	existing map[string]*kmv1.AgentDeploy,
) {
	if len(desired) == 0 {
		as.Status.Phase = kmv1.AgentSetPhaseUnknown
		as.Status.MarkUnknown(kmv1.AgentSetConditionAgentsHealthy, "NoAgents", "No agents are defined")
		return
	}
	var unhealthy []string
	for name := range desired {
		ad, ok := existing[name]
		if !ok {
			unhealthy = append(unhealthy, name+":missing")
			continue
		}
		if ad.Status.Phase != kmv1.AgentDeployPhaseRunning {
			unhealthy = append(unhealthy, fmt.Sprintf("%s:%s", name, ad.Status.Phase))
		}
	}
	if len(unhealthy) == 0 {
		as.Status.Phase = kmv1.AgentSetPhaseRunning
		as.Status.Message = ""
		as.Status.MarkTrue(kmv1.AgentSetConditionAgentsHealthy)
		return
	}
	as.Status.Phase = kmv1.AgentSetPhaseFailed
	msg := fmt.Sprintf("unhealthy agents: %v", unhealthy)
	as.Status.Message = msg
	as.Status.MarkFalse(kmv1.AgentSetConditionAgentsHealthy, "AgentsUnhealthy", msg)
}

// persist writes finalizer and status changes back to the API server.
// Finalizers are written first (via a metadata patch) so that on the
// deletion path the parent is not garbage-collected before the status
// update lands. Each operation only fires if something actually changed.
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

func (r *Reconciler) buildDesired(as *kmv1.AgentSet) (map[string]*kmv1.AgentDeploy, error) {
	out := make(map[string]*kmv1.AgentDeploy, len(as.Spec.Agents))
	for i := range as.Spec.Agents {
		ad, err := r.newAgentDeploy(as, as.Spec.Agents[i])
		if err != nil {
			return nil, err
		}
		out[ad.Name] = ad
	}
	return out, nil
}

func (r *Reconciler) newAgentDeploy(as *kmv1.AgentSet, agent kmv1.AbstractAgentDeploy) (*kmv1.AgentDeploy, error) {
	abstract := *agent.DeepCopy()
	if tmpl := agentDeployTemplate(as); tmpl != nil {
		applyTemplate(&abstract, tmpl)
	}
	ad := &kmv1.AgentDeploy{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: as.Namespace,
			Name:      childName(as.Name, agent.Name),
			Labels: map[string]string{
				kmv1.KeyAgentSetName: as.Name,
				kmv1.KeyComponent:    kmv1.ComponentAgent,
				kmv1.KeyPartOf:       kmv1.Project,
				kmv1.KeyManagedBy:    kmv1.ControllerAgentSet,
				kmv1.KeyAppName:      agent.Name,
			},
		},
		Spec: kmv1.AgentDeploySpec{
			AbstractAgentDeploy: abstract,
			AgentSetName:        as.Name,
		},
	}
	if r.scheme != nil {
		if err := ctrl.SetControllerReference(as, ad, r.scheme); err != nil {
			return nil, fmt.Errorf("failed to set controller reference: %w", err)
		}
	} else {
		ad.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(as, kmv1.AgentSetGroupVersionKind)}
	}
	ad.Annotations = map[string]string{
		kmv1.KeyHash: sharedutil.MustHash(ad.Spec),
	}
	return ad, nil
}

// agentDeployTemplate returns the AgentDeploy template configured on the
// AgentSet, if any.
func agentDeployTemplate(as *kmv1.AgentSet) *kmv1.AgentDeployTemplate {
	if as.Spec.Templates == nil {
		return nil
	}
	return as.Spec.Templates.AgentDeployTemplate
}

// applyTemplate fills unset fields of the per-agent spec from the
// AgentSet-level template. Per-agent values always win.
func applyTemplate(agent *kmv1.AbstractAgentDeploy, tmpl *kmv1.AgentDeployTemplate) {
	if agent.ContainerTemplate == nil && tmpl.ContainerTemplate != nil {
		ct := *tmpl.ContainerTemplate
		agent.ContainerTemplate = &ct
	}
}

func childName(setName, agentName string) string {
	return setName + "-" + agentName
}

// needsUpdate compares two AgentDeploy objects to decide whether an Update
// API call is needed. The spec hash is the cheap path; ownerReferences and
// labels are also compared so misconfigured children get healed.
func needsUpdate(existing, desired *kmv1.AgentDeploy) bool {
	if existing.Annotations[kmv1.KeyHash] != desired.Annotations[kmv1.KeyHash] {
		return true
	}
	if !reflect.DeepEqual(existing.OwnerReferences, desired.OwnerReferences) {
		return true
	}
	if !labelsEqual(existing.Labels, desired.Labels) {
		return true
	}
	return false
}

func labelsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
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
