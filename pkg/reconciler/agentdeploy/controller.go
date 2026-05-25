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
// resources into their owned Pods and the headless Service that gives each
// replica a stable DNS name.
//
//   - One pod per replica index `[0, replicas)`, named
//     "<deploy>-<replicaIdx>-<rand5>" — the index is stable across
//     rollouts; the random suffix is throwaway so delete-and-recreate
//     doesn't hit name-already-exists.
//   - The replica index is carried in the `KeyReplica` annotation AND in
//     `pod.Spec.Hostname` so callers can address a specific replica via
//     "<deploy>-<idx>.<deploy>-headless.<ns>.svc.cluster.local".
//   - A spec hash is stamped on each pod as the `KeyHash` annotation.
//     Drift triggers a delete-and-recreate (k8s forbids most pod-spec
//     mutations after creation).
//   - A single headless Service per AgentDeploy gives the pods their DNS.
package agentdeploy

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strconv"

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
	"github.com/kynoproj/kynomesh/pkg/shared/logging"
	sharedutil "github.com/kynoproj/kynomesh/pkg/shared/util"
)

const (
	// ControllerName identifies this controller in event sources and logs.
	ControllerName = "agentdeploy-controller"

	// FinalizerName guards an AgentDeploy against deletion until the
	// controller has cleaned up the owned pods and service.
	FinalizerName = "kynomesh.kyno.sh/agentdeploy-controller"

	// agentContainerName is the conventional name of the main user-supplied
	// container. Sidecar / init containers keep whatever name the user gave.
	agentContainerName = "agent"

	// brokerContainerName is the name of the broker sidecar the controller
	// injects into every AgentDeploy pod. The broker fronts the agent over
	// the A2A protocol (JSON-RPC, REST, gRPC on one port).
	brokerContainerName = "broker"

	// brokerPort is the port the broker listens on inside each pod. Matches
	// the broker CLI default (KYNOMESH_BROKER_PORT). Kept const so the
	// headless-service DNS pattern + this port form a stable agent address.
	brokerPort = 9100

	// brokerBinaryPath is the in-image entrypoint of the kynomesh binary
	// (the Dockerfile installs it at /bin/kynomesh).
	brokerBinaryPath = "/bin/kynomesh"

	// EnvNamespace / EnvPodName are downward-API env vars the controller
	// injects into every container of an AgentDeploy pod (agent, broker,
	// and any user-supplied sidecars), so workloads can read their own
	// pod identity without a downward-API mount. Names match what the
	// controller itself consumes for self-discovery.
	EnvNamespace = "NAMESPACE"
	EnvPodName   = "POD_NAME"

	// headlessServiceSuffix produces the per-deploy headless Service name.
	headlessServiceSuffix = "-headless"

	// randomSuffixLength is the random tail on pod names — long enough to
	// avoid collisions across rollouts, short enough to keep names well
	// under the 63-char DNS label limit.
	randomSuffixLength = 5
)

// Reconciler implements controller-runtime's reconcile.Reconciler.
type Reconciler struct {
	client.Client
	scheme      *runtime.Scheme
	logger      *zap.SugaredLogger
	recorder    events.EventRecorder
	brokerImage string
}

// NewReconciler returns a Reconciler bound to the supplied client and scheme.
//
// brokerImage is the container image used for the A2A broker sidecar that the
// controller injects into every AgentDeploy pod. It is captured once at
// startup (discovered from the controller's own pod) so reconciliation does
// not need to call the API server to find it.
func NewReconciler(c client.Client, scheme *runtime.Scheme, logger *zap.SugaredLogger, recorder events.EventRecorder, brokerImage string) *Reconciler {
	if logger == nil {
		logger = logging.NewLogger().Named(ControllerName)
	}
	if recorder == nil {
		recorder = noopRecorder{}
	}
	return &Reconciler{
		Client:      c,
		scheme:      scheme,
		logger:      logger,
		recorder:    recorder,
		brokerImage: brokerImage,
	}
}

// Reconcile is the controller-runtime entry point.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.logger.With("namespace", req.Namespace, "name", req.Name)

	var original kmv1.AgentDeploy
	if err := r.Get(ctx, req.NamespacedName, &original); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get AgentDeploy: %w", err)
	}

	ad := original.DeepCopy()
	reconcileErr := r.reconcile(ctx, ad)

	if persistErr := r.persist(ctx, &original, ad); persistErr != nil {
		if reconcileErr == nil {
			return ctrl.Result{}, persistErr
		}
		log.Warnw("failed to persist AgentDeploy updates", "err", persistErr)
	}
	if reconcileErr != nil {
		return ctrl.Result{}, reconcileErr
	}
	return ctrl.Result{}, nil
}

// reconcile mutates the supplied deep copy. All API writes for children
// happen here; the parent is persisted by Reconcile via persist().
func (r *Reconciler) reconcile(ctx context.Context, ad *kmv1.AgentDeploy) error {
	ad.Status.InitializeConditions(
		kmv1.AgentDeployConditionDeployed,
		kmv1.AgentDeployConditionPodsHealthy,
	)
	ad.Status.ObservedGeneration = ad.Generation

	if !ad.DeletionTimestamp.IsZero() {
		if err := r.deleteOwned(ctx, ad); err != nil {
			return fmt.Errorf("failed to delete owned resources: %w", err)
		}
		removeFinalizer(ad)
		return nil
	}
	addFinalizer(ad)

	if err := r.reconcileService(ctx, ad); err != nil {
		ad.Status.MarkFalse(kmv1.AgentDeployConditionDeployed, "ServiceFailed", err.Error())
		ad.Status.Phase = kmv1.AgentDeployPhaseFailed
		ad.Status.Reason = "ServiceFailed"
		ad.Status.Message = err.Error()
		return err
	}

	if err := r.reconcilePods(ctx, ad); err != nil {
		ad.Status.MarkFalse(kmv1.AgentDeployConditionDeployed, "PodsFailed", err.Error())
		ad.Status.Phase = kmv1.AgentDeployPhaseFailed
		ad.Status.Reason = "PodsFailed"
		ad.Status.Message = err.Error()
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

// reconcilePods drives the per-replica pod set: bucket existing pods by
// replica index, then for each index in [0, desired) ensure exactly one pod
// whose hash matches the desired hash. Stale pods (wrong hash, duplicate,
// or out-of-range replica) are deleted.
func (r *Reconciler) reconcilePods(ctx context.Context, ad *kmv1.AgentDeploy) error {
	desired := desiredReplicas(ad)
	ad.Status.DesiredReplicas = uint32(desired)

	desiredPodSpec := buildPodSpec(ad, r.brokerImage)
	desiredHash := sharedutil.MustHash(desiredPodSpec)
	ad.Status.UpdateHash = desiredHash

	existing, err := r.listOwnedPods(ctx, ad)
	if err != nil {
		return err
	}
	byReplica := groupPodsByReplica(existing)

	for replica := range desired {
		pods := byReplica[replica]
		var kept *corev1.Pod
		for i := range pods {
			p := pods[i]
			if kept == nil && p.Annotations[kmv1.KeyHash] == desiredHash && p.DeletionTimestamp.IsZero() {
				kept = p
				continue
			}
			if err := r.deletePod(ctx, ad, p, "drift or duplicate"); err != nil {
				return err
			}
		}
		if kept != nil {
			continue
		}
		if err := r.createPodForReplica(ctx, ad, replica, desiredPodSpec, desiredHash); err != nil {
			return err
		}
	}

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
	return nil
}

func (r *Reconciler) createPodForReplica(ctx context.Context, ad *kmv1.AgentDeploy, replica int, podSpec corev1.PodSpec, hash string) error {
	pod := newPod(ad, replica, podSpec, hash)
	if err := r.Create(ctx, pod); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create pod for replica %d: %w", replica, err)
	}
	r.recorder.Eventf(ad, nil, corev1.EventTypeNormal, "CreatedPod", "CreatePod", "Created pod %s (replica %d)", pod.Name, replica)
	return nil
}

func (r *Reconciler) deletePod(ctx context.Context, ad *kmv1.AgentDeploy, p *corev1.Pod, reason string) error {
	if !p.DeletionTimestamp.IsZero() {
		return nil
	}
	if err := r.Delete(ctx, p); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete pod %s: %w", p.Name, err)
	}
	r.recorder.Eventf(ad, nil, corev1.EventTypeNormal, "DeletedPod", "DeletePod", "Deleted pod %s (%s)", p.Name, reason)
	return nil
}

// reconcileService ensures the per-deploy headless service exists with the
// expected spec. Drift triggers delete-and-recreate because some Service
// spec fields are immutable.
func (r *Reconciler) reconcileService(ctx context.Context, ad *kmv1.AgentDeploy) error {
	desired := newHeadlessService(ad)
	desiredHash := sharedutil.MustHash(desired.Spec)
	desired.Annotations[kmv1.KeyHash] = desiredHash

	var existing corev1.Service
	err := r.Get(ctx, client.ObjectKey{Namespace: desired.Namespace, Name: desired.Name}, &existing)
	if apierrors.IsNotFound(err) {
		if createErr := r.Create(ctx, desired); createErr != nil && !apierrors.IsAlreadyExists(createErr) {
			return fmt.Errorf("failed to create headless service: %w", createErr)
		}
		r.recorder.Eventf(ad, nil, corev1.EventTypeNormal, "CreatedService", "CreateService", "Created headless service %s", desired.Name)
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get headless service: %w", err)
	}
	if existing.Annotations[kmv1.KeyHash] == desiredHash {
		return nil
	}
	if err := r.Delete(ctx, &existing); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete stale headless service: %w", err)
	}
	if err := r.Create(ctx, desired); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to recreate headless service: %w", err)
	}
	r.recorder.Eventf(ad, nil, corev1.EventTypeNormal, "UpdatedService", "UpdateService", "Recreated headless service %s on spec drift", desired.Name)
	return nil
}

// deleteOwned removes every Pod and Service this AgentDeploy controls.
func (r *Reconciler) deleteOwned(ctx context.Context, ad *kmv1.AgentDeploy) error {
	pods, err := r.listOwnedPods(ctx, ad)
	if err != nil {
		return err
	}
	for _, p := range pods {
		if err := r.Delete(ctx, p); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete pod %s: %w", p.Name, err)
		}
	}

	var svc corev1.Service
	err = r.Get(ctx, client.ObjectKey{Namespace: ad.Namespace, Name: headlessServiceName(ad)}, &svc)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := r.Delete(ctx, &svc); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete headless service: %w", err)
	}
	return nil
}

func (r *Reconciler) listOwnedPods(ctx context.Context, ad *kmv1.AgentDeploy) ([]*corev1.Pod, error) {
	var list corev1.PodList
	if err := r.List(ctx, &list,
		client.InNamespace(ad.Namespace),
		client.MatchingLabels{kmv1.KeyAppName: ad.Name, kmv1.KeyManagedBy: ControllerName},
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
	ad.Status.Selector = fmt.Sprintf("%s=%s,%s=%s",
		kmv1.KeyAppName, ad.Name, kmv1.KeyManagedBy, ControllerName)
	return nil
}

// persist writes finalizer and status changes back to the API server.
// Status before finalizer so the parent isn't garbage-collected before the
// status update lands on the deletion path.
func (r *Reconciler) persist(ctx context.Context, original, updated *kmv1.AgentDeploy) error {
	if !reflect.DeepEqual(original.Status, updated.Status) {
		statusPatch := client.MergeFrom(original.DeepCopy())
		patchObj := original.DeepCopy()
		patchObj.Status = updated.Status
		if err := r.Status().Patch(ctx, patchObj, statusPatch); err != nil {
			return fmt.Errorf("failed to patch status: %w", err)
		}
	}
	if !reflect.DeepEqual(original.Finalizers, updated.Finalizers) {
		metaPatch := client.MergeFrom(original.DeepCopy())
		patchObj := original.DeepCopy()
		patchObj.Finalizers = updated.Finalizers
		if err := r.Patch(ctx, patchObj, metaPatch); err != nil {
			return fmt.Errorf("failed to patch finalizers: %w", err)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Builders
// ---------------------------------------------------------------------------

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

// buildPodSpec composes the corev1.PodSpec from the AgentDeploy spec. It is
// deterministic per (spec, template, brokerImage) so the hash over it is a
// meaningful drift signal — changing the broker image triggers a pod
// recreate, just like changing the agent spec.
//
// Container order: [agent, broker, ...sidecars]. The broker sits between the
// user's agent and any user-supplied sidecars so the conventional default
// container annotation still points at "agent" for kubectl exec/logs.
func buildPodSpec(ad *kmv1.AgentDeploy, brokerImage string) corev1.PodSpec {
	agentContainer := corev1.Container{
		Name: agentContainerName,
	}
	if ct := ad.Spec.ContainerTemplate; ct != nil {
		ct.ApplyToContainer(&agentContainer)
	}
	containers := []corev1.Container{agentContainer, newBrokerContainer(brokerImage)}
	containers = append(containers, ad.Spec.Sidecars...)

	ps := corev1.PodSpec{
		Containers:     containers,
		InitContainers: ad.Spec.InitContainers,
		Volumes:        ad.Spec.Volumes,
		Subdomain:      headlessServiceName(ad),
	}
	ad.Spec.ApplyToPodSpec(&ps)

	// Inject downward-API env into every container so workloads can read
	// their own pod identity. Done last so it covers user-supplied
	// sidecars too. Built-in env always wins on key conflict — users must
	// not be able to lie about their own pod identity.
	builtin := downwardAPIEnv()
	for i := range ps.Containers {
		ps.Containers[i].Env = mergeEnv(ps.Containers[i].Env, builtin)
	}
	return ps
}

// downwardAPIEnv returns the env vars every AgentDeploy container receives:
// NAMESPACE (the pod's namespace) and POD_NAME (the pod's name), both via
// the downward API so no values need to be threaded through at build time.
func downwardAPIEnv() []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name: EnvNamespace,
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
			},
		},
		{
			Name: EnvPodName,
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
			},
		},
	}
}

// mergeEnv returns existing with overrides applied: any entry in overrides
// replaces an entry in existing with the same Name, and anything not already
// present is appended. overrides wins — used here to guarantee the controller's
// downward-API env can't be shadowed by user-supplied values.
func mergeEnv(existing, overrides []corev1.EnvVar) []corev1.EnvVar {
	overrideByName := make(map[string]corev1.EnvVar, len(overrides))
	for _, o := range overrides {
		overrideByName[o.Name] = o
	}
	out := make([]corev1.EnvVar, 0, len(existing)+len(overrides))
	seen := make(map[string]struct{}, len(existing))
	for _, e := range existing {
		if o, ok := overrideByName[e.Name]; ok {
			out = append(out, o)
		} else {
			out = append(out, e)
		}
		seen[e.Name] = struct{}{}
	}
	for _, o := range overrides {
		if _, ok := seen[o.Name]; ok {
			continue
		}
		out = append(out, o)
	}
	return out
}

// newBrokerContainer builds the A2A broker sidecar. The image is supplied by
// the controller (typically discovered as the controller's own image) so the
// broker stays bit-for-bit in sync with the controller release.
func newBrokerContainer(image string) corev1.Container {
	return corev1.Container{
		Name:    brokerContainerName,
		Image:   image,
		Command: []string{brokerBinaryPath},
		Args:    []string{"broker"},
		Ports: []corev1.ContainerPort{{
			Name:          "a2a",
			ContainerPort: brokerPort,
			Protocol:      corev1.ProtocolTCP,
		}},
	}
}

// newPod renders a corev1.Pod for the given replica index. The random
// suffix on the pod name lets a delete-and-recreate rollout proceed
// without a name-already-exists race; the stable bits (replica index,
// labels, annotation, hostname) are what consumers should rely on.
func newPod(ad *kmv1.AgentDeploy, replica int, podSpec corev1.PodSpec, hash string) *corev1.Pod {
	name := fmt.Sprintf("%s-%d-%s", ad.Name, replica, sharedutil.RandomLowerCaseString(randomSuffixLength))
	hostname := fmt.Sprintf("%s-%d", ad.Name, replica)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ad.Namespace,
			Name:      name,
			Labels: map[string]string{
				kmv1.KeyAppName:   ad.Name,
				kmv1.KeyComponent: "agent",
				kmv1.KeyPartOf:    kmv1.Project,
				kmv1.KeyManagedBy: ControllerName,
				kmv1.KeyReplica:   strconv.Itoa(replica),
			},
			Annotations: map[string]string{
				kmv1.KeyHash:             hash,
				kmv1.KeyReplica:          strconv.Itoa(replica),
				kmv1.KeyDefaultContainer: agentContainerName,
			},
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(ad, kmv1.AgentDeployGroupVersionKind)},
		},
		Spec: podSpec,
	}
	// Carry the AgentSet label through if present, so set-level selectors
	// still find the pods.
	if setName, ok := ad.Labels[kmv1.KeyAgentSetName]; ok {
		pod.Labels[kmv1.KeyAgentSetName] = setName
	}
	pod.Spec.Hostname = hostname
	if pod.Spec.Subdomain == "" {
		pod.Spec.Subdomain = headlessServiceName(ad)
	}
	return pod
}

// newHeadlessService builds the per-deploy ClusterIP=None service. Each
// pod with matching labels gets a DNS record at
// "<deploy>-<idx>.<deploy>-headless.<ns>.svc.cluster.local".
func newHeadlessService(ad *kmv1.AgentDeploy) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ad.Namespace,
			Name:      headlessServiceName(ad),
			Labels: map[string]string{
				kmv1.KeyAppName:   ad.Name,
				kmv1.KeyComponent: "agent",
				kmv1.KeyPartOf:    kmv1.Project,
				kmv1.KeyManagedBy: ControllerName,
			},
			Annotations:     map[string]string{},
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(ad, kmv1.AgentDeployGroupVersionKind)},
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: corev1.ClusterIPNone,
			Selector: map[string]string{
				kmv1.KeyAppName:   ad.Name,
				kmv1.KeyManagedBy: ControllerName,
			},
			// Pods get a DNS A record the instant the kubelet posts an IP,
			// without waiting for readiness. Agents typically need to
			// address each other during bootstrap, before any of them
			// passes a readiness probe.
			PublishNotReadyAddresses: true,
		},
	}
}

func headlessServiceName(ad *kmv1.AgentDeploy) string {
	return ad.Name + headlessServiceSuffix
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

func addFinalizer(ad *kmv1.AgentDeploy) {
	if slices.Contains(ad.Finalizers, FinalizerName) {
		return
	}
	ad.Finalizers = append(ad.Finalizers, FinalizerName)
}

func removeFinalizer(ad *kmv1.AgentDeploy) {
	out := ad.Finalizers[:0]
	for _, f := range ad.Finalizers {
		if f != FinalizerName {
			out = append(out, f)
		}
	}
	ad.Finalizers = out
}

type noopRecorder struct{}

func (noopRecorder) Eventf(runtime.Object, runtime.Object, string, string, string, string, ...any) {
}

var _ reconcile.Reconciler = (*Reconciler)(nil)
