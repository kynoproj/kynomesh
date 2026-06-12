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
	"k8s.io/apimachinery/pkg/util/intstr"
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
	FinalizerName = "kynomesh.kyno.sh/" + kmv1.ControllerAgentSet
)

// Reconciler implements sigs.k8s.io/controller-runtime/pkg/reconcile.Reconciler
// for AgentSet.
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
// copy.
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
	log := logging.FromContext(ctx)
	for name, want := range desired {
		got, ok := existing[name]
		if !ok {
			if err := r.Create(ctx, want); err != nil && !apierrors.IsAlreadyExists(err) {
				return fmt.Errorf("failed to create AgentDeploy %s: %w", name, err)
			}
			log.Infow("Created an AgentDeploy", zap.String("agetDeploy", name))
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
		log.Infow("Updated an existing AgentDeploy", zap.String("agetDeploy", name))
		r.recorder.Eventf(as, nil, corev1.EventTypeNormal, "UpdatedAgentDeploy", "UpdateAgentDeploy", "Updated AgentDeploy %s", name)
	}

	for name, got := range existing {
		if _, keep := desired[name]; keep {
			continue
		}
		if err := r.Delete(ctx, got); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete orphan AgentDeploy %s: %w", name, err)
		}
		log.Infow("Deleted an orphan AgentDeploy", zap.String("agentDeploy", name))
		r.recorder.Eventf(as, nil, corev1.EventTypeNormal, "DeletedAgentDeploy", "DeleteAgentDeploy", "Deleted orphan AgentDeploy %s", name)
	}
	return nil
}

// deleteChildren removes all AgentDeploys labelled as belonging to this
// AgentSet.
func (r *Reconciler) deleteChildren(ctx context.Context, as *kmv1.AgentSet) error {
	log := logging.FromContext(ctx)
	existing, err := r.listOwnedAgentDeploys(ctx, as)
	if err != nil {
		return err
	}
	for _, ad := range existing {
		if err := r.Delete(ctx, ad); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete AgentDeploy %s during cleanup: %w", ad.Name, err)
		}
		log.Infow("Deleted an AgentDeploy", zap.String("agentDeploy", ad.Name))
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
			Topology:            computeTopology(as, agent.Name),
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
	if agent.BrokerTemplate == nil && tmpl.BrokerTemplate != nil {
		ct := *tmpl.BrokerTemplate
		agent.BrokerTemplate = &ct
	}
}

func childName(setName, agentName string) string {
	return setName + "-" + agentName
}

// computeTopology derives the per-agent topology view from the AgentSet pattern.
func computeTopology(as *kmv1.AgentSet, agentName string) kmv1.Topology {
	t := kmv1.Topology{
		Pattern: as.Spec.Pattern,
		IsEntry: agentName == as.Spec.Entry,
	}
	switch as.Spec.Pattern {
	case kmv1.AgentPatternHandoff:
		t.Peers = peersExcluding(as.Spec.Agents, agentName)
	case kmv1.AgentPatternSupervisor:
		if t.IsEntry {
			t.Peers = peersExcluding(as.Spec.Agents, agentName)
		}
	case kmv1.AgentPatternSequential:
		if next, ok := nextAgent(as.Spec.Agents, agentName); ok {
			t.Peers = []kmv1.Peer{{Name: next, Kind: kmv1.PeerKindManaged}}
		}
	}
	return t
}

// peersExcluding returns every agent except self as Managed peer entries.
func peersExcluding(agents []kmv1.AbstractAgentDeploy, self string) []kmv1.Peer {
	out := make([]kmv1.Peer, 0, len(agents)-1)
	for _, a := range agents {
		if a.Name == self {
			continue
		}
		out = append(out, kmv1.Peer{Name: a.Name, Kind: kmv1.PeerKindManaged})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// nextAgent returns the agent immediately after self in declaration order.
func nextAgent(agents []kmv1.AbstractAgentDeploy, self string) (string, bool) {
	for i, a := range agents {
		if a.Name == self && i+1 < len(agents) {
			return agents[i+1].Name, true
		}
	}
	return "", false
}

// needsUpdate compares two AgentDeploy objects to decide whether an Update
// API call is needed.
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

// Reference assertion to surface signature errors at compile time rather
// than only when SetupWithManager is invoked at runtime.
var _ reconcile.Reconciler = (*Reconciler)(nil)
