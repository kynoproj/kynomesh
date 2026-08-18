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
	"reflect"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
	"github.com/kynoproj/kynomesh/pkg/shared/logging"
	sharedutil "github.com/kynoproj/kynomesh/pkg/shared/util"
)

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
	if t := as.Spec.Templates; t != nil && t.AgentDeployTemplate != nil {
		applyTemplate(&abstract, t.AgentDeployTemplate)
	}
	ad := &kmv1.AgentDeploy{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: as.Namespace,
			Name:      as.ChildAgentDeployName(agent.Name),
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

// applyTemplate fills unset fields of the per-agent spec from the
// AgentSet-level template, field by field, with per-agent values always winning.
func applyTemplate(agent *kmv1.AbstractAgentDeploy, tmpl *kmv1.AgentDeployTemplate) {
	agent.ApplyDefaultsFrom(&tmpl.AbstractPodTemplate)
	if tmpl.BrokerContainer != nil {
		if agent.BrokerContainer == nil {
			agent.BrokerContainer = &kmv1.ContainerTemplate{}
		}
		agent.BrokerContainer.ApplyDefaultsFrom(tmpl.BrokerContainer)
	}
}

// computeTopology derives the per-agent topology view from the AgentSet pattern.
func computeTopology(as *kmv1.AgentSet, agentName string) kmv1.Topology {
	t := kmv1.Topology{
		Pattern: as.Spec.Pattern,
		IsEntry: agentName == as.Spec.Entry,
	}
	switch as.Spec.Pattern {
	case kmv1.AgentPatternHandoff:
		t.Peers = peersExcluding(as.Spec.Agents, as.Spec.ExternalAgents, agentName)
	case kmv1.AgentPatternSupervisor:
		if t.IsEntry {
			t.Peers = peersExcluding(as.Spec.Agents, as.Spec.ExternalAgents, agentName)
		}
	case kmv1.AgentPatternSequential:
		if next, ok := nextAgent(as.Spec.Agents, as.Spec.ExternalAgents, agentName); ok {
			t.Peers = []kmv1.Peer{next}
		}
	}
	return t
}

// peersExcluding returns every managed agent except self, plus every
// external agent, as Peer entries.
func peersExcluding(agents []kmv1.AbstractAgentDeploy, externalAgents []kmv1.ExternalAgentRef, self string) []kmv1.Peer {
	out := make([]kmv1.Peer, 0, len(agents)-1+len(externalAgents))
	for _, a := range agents {
		if a.Name == self {
			continue
		}
		out = append(out, kmv1.Peer{Name: a.Name, Kind: kmv1.PeerKindManaged})
	}
	for _, e := range externalAgents {
		out = append(out, kmv1.Peer{Name: e.Name, Kind: kmv1.PeerKindExternal, URL: e.URL})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// nextAgent returns the peer immediately after self in the chain: the next
// managed agent in declaration order, or — if self is the last managed
// agent — the sole external agent, if one is configured.
// Sequential allows at most one, enforced by the validator, and it may only
// be the final hop.
func nextAgent(agents []kmv1.AbstractAgentDeploy, externalAgents []kmv1.ExternalAgentRef, self string) (kmv1.Peer, bool) {
	for i, a := range agents {
		if a.Name != self {
			continue
		}
		if i+1 < len(agents) {
			return kmv1.Peer{Name: agents[i+1].Name, Kind: kmv1.PeerKindManaged}, true
		}
		if len(externalAgents) > 0 {
			e := externalAgents[0]
			return kmv1.Peer{Name: e.Name, Kind: kmv1.PeerKindExternal, URL: e.URL}, true
		}
		return kmv1.Peer{}, false
	}
	return kmv1.Peer{}, false
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
