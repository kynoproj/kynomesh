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
	"strconv"

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

// randomSuffixLength is the random tail on pod names.
const randomSuffixLength = 5

// reconcilePods drives the per-replica pod set toward the desired spec
// with rolling update:
//
//  1. Scale-down first — pods at indices outside [0, desired) are deleted
//     unconditionally; that work doesn't count against MaxUnavailable.
//  2. Initial bring-up — if no slot in [0, desired) is on the desired hash
//     yet (fresh deploy, or a full wipe), create the entire window in one
//     pass. MaxUnavailable only gates *replacement* of existing pods.
//  3. Rolling replacement — for slots not yet on the desired hash, replace
//     them delete-then-create, batched by MaxUnavailable. Between batches
//     the reconciler waits for the in-flight replacements to go Ready (it
//     returns nil and lets the pod-watch event drive the next pass).
func (r *Reconciler) reconcilePods(ctx context.Context, ad *kmv1.AgentDeploy) error {
	desired := desiredReplicas(ad)
	// Stamp the scale time whenever the target replica count actually changes
	// (but not on initial bring-up, when DesiredReplicas is still unset). This
	// is the reference point the autoscaler reads for its cooldowns, regardless
	// of whether the change came from the autoscaler or a manual edit.
	if ad.Status.DesiredReplicas != 0 && uint32(desired) != ad.Status.DesiredReplicas {
		ad.Status.LastScaledAt = metav1.Now()
	}
	ad.Status.DesiredReplicas = uint32(desired)
	// Seed the cooldown reference from the creation time so a brand-new
	// AgentDeploy is treated as recently scaled to its initial size, rather than
	// being immediately eligible for autoscaling.
	if ad.Status.LastScaledAt.IsZero() {
		ad.Status.LastScaledAt = ad.CreationTimestamp
	}

	desiredPodSpec := buildPodSpec(ad, r.image, r.imagePullPolicy)
	desiredHash := sharedutil.MustHash(desiredPodSpec)

	// Detect a spec change: reset the rollout cursor so a mid-rollout
	// spec edit restarts from slot 0. updatePodStatus will reconcile this
	// back to the live count at the end of Reconcile.
	if ad.Status.UpdateHash != desiredHash {
		ad.Status.UpdateHash = desiredHash
		ad.Status.UpdatedReplicas = 0
		ad.Status.UpdatedReadyReplicas = 0
	}

	existing, err := r.listOwnedPods(ctx, ad)
	if err != nil {
		return err
	}
	byReplica := groupPodsByReplica(existing)

	// (1) Scale-down: delete pods outside the desired window. Pods with an
	// invalid replica annotation land in bucket -1 and are treated the same.
	for replica, pods := range byReplica {
		if replica >= 0 && replica < desired {
			continue
		}
		for _, p := range pods {
			if err := r.deletePod(ctx, ad, p, "outside replica window"); err != nil {
				return err
			}
		}
	}

	// Classify each in-window slot:
	//   - "satisfied": already has a non-terminating pod on the desired hash
	//   - "needsCreate": no live pod at all — a scale-up or initial-bring-up slot
	//   - "needsReplace": has a live pod on a non-desired hash — rolling-update target
	// Within-slot duplicates and pods already being deleted are pruned
	// unconditionally; they're not part of the desired state regardless of
	// rolling-update batching.
	needsCreate := make([]int, 0, desired)
	needsReplace := make([]int, 0, desired)
	updatedOnDesired := 0
	updatedReadyOnDesired := 0
	for replica := range desired {
		pods := byReplica[replica]
		var kept *corev1.Pod
		hasLiveOld := false
		for _, p := range pods {
			if !p.DeletionTimestamp.IsZero() {
				continue // terminating — let it finish; don't count it
			}
			if p.Annotations[kmv1.KeyHash] == desiredHash {
				if kept == nil {
					kept = p
					continue
				}
				// Duplicate on the desired hash — keep one, delete extras.
				if err := r.deletePod(ctx, ad, p, "duplicate"); err != nil {
					return err
				}
				continue
			}
			hasLiveOld = true
		}
		switch {
		case kept != nil:
			updatedOnDesired++
			if podReady(kept) {
				updatedReadyOnDesired++
			}
		case hasLiveOld:
			needsReplace = append(needsReplace, replica)
		default:
			needsCreate = append(needsCreate, replica)
		}
	}

	// Empty slots are never gated — they aren't a rolling-update concern.
	// This covers initial bring-up *and* scale-up of an in-progress deploy.
	for _, replica := range needsCreate {
		if err := r.createPodForReplica(ctx, ad, replica, desiredPodSpec, desiredHash); err != nil {
			return err
		}
	}

	if len(needsReplace) == 0 {
		// Every slot is either on the desired hash or has just been created.
		// Promote CurrentHash once the full window is on UpdateHash.
		if updatedOnDesired+len(needsCreate) == desired {
			ad.Status.CurrentHash = desiredHash
		}
		return nil
	}

	// Rolling replacement: bound the number of slots we touch per pass by
	// MaxUnavailable, and wait between batches for the in-flight new pods
	// to become Ready.
	maxUnavailable := resolveMaxUnavailable(ad, desired)

	// Wait gate: if a previous batch's replacements haven't all gone Ready,
	// hold off — the pod-watch event will requeue us when one becomes Ready.
	// Computed from the live pod set rather than Status counters so a
	// half-finished rollout where Status drifted from reality doesn't
	// deadlock the controller.
	if updatedOnDesired > 0 && updatedReadyOnDesired < updatedOnDesired {
		return nil
	}

	batch := min(maxUnavailable, len(needsReplace))

	for _, replica := range needsReplace[:batch] {
		for _, p := range byReplica[replica] {
			if !p.DeletionTimestamp.IsZero() || p.Annotations[kmv1.KeyHash] == desiredHash {
				continue
			}
			if err := r.deletePod(ctx, ad, p, "rolling update"); err != nil {
				return err
			}
		}
		if err := r.createPodForReplica(ctx, ad, replica, desiredPodSpec, desiredHash); err != nil {
			return err
		}
	}
	return nil
}

// resolveMaxUnavailable returns the per-pass replacement budget. Always at
// least 1 so a rollout can make forward progress.
func resolveMaxUnavailable(ad *kmv1.AgentDeploy, desired int) int {
	mu := ad.Spec.UpdateStrategy.GetRollingUpdateStrategy().GetMaxUnavailable()
	n, err := intstr.GetScaledValueFromIntOrPercent(&mu, desired, true)
	if err != nil || n < 1 {
		n = 1
	}
	return n
}

func (r *Reconciler) createPodForReplica(ctx context.Context, ad *kmv1.AgentDeploy, replica int, podSpec corev1.PodSpec, hash string) error {
	log := logging.FromContext(ctx)
	pod := newPod(ad, replica, podSpec, hash)
	if err := r.Create(ctx, pod); err != nil && !apierrors.IsAlreadyExists(err) {
		log.Errorw("Failed to create pod", zap.Int("replica", replica), zap.Error(err))
		return fmt.Errorf("failed to create pod for replica %d: %w", replica, err)
	}
	log.Infow("Succeeded to create pod", zap.Int("replica", replica), zap.String("podName", pod.Name))
	r.recorder.Eventf(ad, nil, corev1.EventTypeNormal, "CreatedPod", "CreatePod", "Created pod %s (replica %d)", pod.Name, replica)
	return nil
}

func (r *Reconciler) deletePod(ctx context.Context, ad *kmv1.AgentDeploy, p *corev1.Pod, reason string) error {
	log := logging.FromContext(ctx)
	if !p.DeletionTimestamp.IsZero() {
		return nil
	}
	if err := r.Delete(ctx, p); err != nil && !apierrors.IsNotFound(err) {
		log.Errorw("Failed to delete pod", zap.String("podName", p.Name), zap.Error(err))
		return fmt.Errorf("failed to delete pod %s: %w", p.Name, err)
	}
	log.Infow("Succeeded to delete pod", zap.String("podName", p.Name))
	r.recorder.Eventf(ad, nil, corev1.EventTypeNormal, "DeletedPod", "DeletePod", "Deleted pod %s (%s)", p.Name, reason)
	return nil
}

func (r *Reconciler) listOwnedPods(ctx context.Context, ad *kmv1.AgentDeploy) ([]*corev1.Pod, error) {
	var list corev1.PodList
	if err := r.List(ctx, &list,
		client.InNamespace(ad.Namespace),
		client.MatchingLabels{
			kmv1.KeyAgentSetName:    ad.Spec.AgentSetName,
			kmv1.KeyAgentDeployName: ad.Spec.Name,
			kmv1.KeyManagedBy:       kmv1.ControllerAgentDeploy,
		},
	); err != nil {
		return nil, err
	}
	out := make([]*corev1.Pod, 0, len(list.Items))
	for i := range list.Items {
		out = append(out, &list.Items[i])
	}
	return out, nil
}

// updatePodStatus folds the live pod set into Status.Replicas /
// ReadyReplicas / UpdatedReplicas — called after orchestration so it
// observes the post-write state.
func (r *Reconciler) updatePodStatus(ctx context.Context, ad *kmv1.AgentDeploy) error {
	pods, err := r.listOwnedPods(ctx, ad)
	if err != nil {
		return err
	}
	var replicas, ready, updated, updatedReady uint32
	for _, p := range pods {
		if !p.DeletionTimestamp.IsZero() {
			continue
		}
		replicas++
		isReady := podReady(p)
		if isReady {
			ready++
		}
		if p.Annotations[kmv1.KeyHash] == ad.Status.UpdateHash {
			updated++
			if isReady {
				updatedReady++
			}
		}
	}
	ad.Status.Replicas = replicas
	ad.Status.ReadyReplicas = ready
	ad.Status.UpdatedReplicas = updated
	ad.Status.UpdatedReadyReplicas = updatedReady
	ad.Status.Selector = fmt.Sprintf("%s=%s,%s=%s,%s=%s",
		kmv1.KeyAgentSetName, ad.Spec.AgentSetName,
		kmv1.KeyAgentDeployName, ad.Spec.Name,
		kmv1.KeyManagedBy, kmv1.ControllerAgentDeploy)
	return nil
}

// desiredReplicas returns the replica count from spec, defaulting to 1.
func desiredReplicas(ad *kmv1.AgentDeploy) int {
	if ad.Spec.Replicas == nil {
		return 1
	}
	r := int(*ad.Spec.Replicas)
	if r < 0 {
		return 0
	}
	return r
}

// newPod renders a corev1.Pod for the given replica index.
func newPod(ad *kmv1.AgentDeploy, replica int, podSpec corev1.PodSpec, hash string) *corev1.Pod {
	name := fmt.Sprintf("%s-%d-%s", ad.Name, replica, sharedutil.RandomLowerCaseString(randomSuffixLength))
	hostname := fmt.Sprintf("%s-%d", ad.Name, replica)

	labels := map[string]string{
		kmv1.KeyAppName:         ad.Name,
		kmv1.KeyAgentSetName:    ad.Spec.AgentSetName,
		kmv1.KeyAgentDeployName: ad.Spec.Name,
		kmv1.KeyComponent:       kmv1.ComponentAgent,
		kmv1.KeyPartOf:          kmv1.Project,
		kmv1.KeyManagedBy:       kmv1.ControllerAgentDeploy,
		kmv1.KeyReplica:         strconv.Itoa(replica),
		kmv1.KeyServing:         "true",
	}
	if ad.Spec.Topology.IsEntry {
		labels[kmv1.KeyEntry] = "true"
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ad.Namespace,
			Name:      name,
			Labels:    labels,
			Annotations: map[string]string{
				kmv1.KeyHash:             hash,
				kmv1.KeyReplica:          strconv.Itoa(replica),
				kmv1.KeyDefaultContainer: kmv1.ContainerNameAgent,
			},
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(ad, kmv1.AgentDeployGroupVersionKind)},
		},
		Spec: podSpec,
	}
	pod.Spec.Hostname = hostname
	if pod.Spec.Subdomain == "" {
		pod.Spec.Subdomain = ad.HeadlessServiceName()
	}
	return pod
}

// groupPodsByReplica buckets pods by their KeyReplica annotation. Pods
// without a valid annotation land in bucket -1 and the caller treats them
// as "outside the desired window".
func groupPodsByReplica(pods []*corev1.Pod) map[int][]*corev1.Pod {
	out := make(map[int][]*corev1.Pod, len(pods))
	for _, p := range pods {
		idx, err := strconv.Atoi(p.Annotations[kmv1.KeyReplica])
		if err != nil {
			idx = -1
		}
		out[idx] = append(out[idx], p)
	}
	return out
}

func podReady(p *corev1.Pod) bool {
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}
