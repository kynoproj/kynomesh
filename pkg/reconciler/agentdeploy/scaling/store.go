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

package scaling

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
)

// Store retains an AgentDeploy's load history. It is passive: the caller drives
// ingestion (Record), reads (History), persistence (Flush), and startup
// hydration (Load). Implementations are safe for concurrent use.
type Store interface {
	// Record appends a fresh sample to memory. Cheap; does not touch the API.
	Record(s Sample)
	// History returns the time-ordered samples for the estimator.
	History(now time.Time) []Sample
	// Load hydrates memory from the backing object (startup). A missing object
	// is not an error.
	Load(ctx context.Context) error
	// Flush persists the current buffer to the backing object. Called on a
	// slow cadence, never per-sample.
	Flush(ctx context.Context) error
}

// historyKey is the ConfigMap binaryData key holding the encoded history blob.
const historyKey = "history"

// ConfigMapStore persists history to a single ConfigMap owned by the
// AgentDeploy — one object per AgentDeploy, garbage-collected with it.
type ConfigMapStore struct {
	client    client.Client
	namespace string
	name      string
	ownerRef  metav1.OwnerReference
	labels    map[string]string
	buf       *buffer
}

var _ Store = (*ConfigMapStore)(nil)

// HistoryConfigMapName is the ConfigMap name holding an AgentDeploy's history.
func HistoryConfigMapName(adName string) string {
	return adName + "-scaling-history"
}

// NewConfigMapStore binds a store to one AgentDeploy.
func NewConfigMapStore(c client.Client, ad *kmv1.AgentDeploy, opts ...bufferOption) *ConfigMapStore {
	s := &ConfigMapStore{
		client:    c,
		namespace: ad.Namespace,
		name:      HistoryConfigMapName(ad.Name),
		ownerRef:  *metav1.NewControllerRef(ad, kmv1.AgentDeployGroupVersionKind),
		labels: map[string]string{
			kmv1.KeyManagedBy:       kmv1.ControllerAgentDeploy,
			kmv1.KeyAgentDeployName: ad.Name,
			kmv1.KeyComponent:       "scaling-history",
		},
		buf: newBuffer(opts...),
	}
	s.buf.setGeneration(ad.Generation)
	return s
}

func (s *ConfigMapStore) Record(sample Sample) { s.buf.add(sample) }

func (s *ConfigMapStore) History(now time.Time) []Sample { return s.buf.samples(now) }

// Load hydrates the in-memory buffer from the backing ConfigMap. A missing
// object is not an error (first run); the buffer stays empty.
func (s *ConfigMapStore) Load(ctx context.Context) error {
	var cm corev1.ConfigMap
	if err := s.client.Get(ctx, client.ObjectKey{Namespace: s.namespace, Name: s.name}, &cm); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to get history ConfigMap: %w", err)
	}
	records, err := decodeRecords(cm.BinaryData[historyKey])
	if err != nil {
		return fmt.Errorf("failed to decode history: %w", err)
	}
	s.buf.load(records)
	return nil
}

// Flush persists the current buffer to the backing ConfigMap, creating it if
// absent. Retries once on a write conflict.
func (s *ConfigMapStore) Flush(ctx context.Context) error {
	return s.upsert(ctx, encodeRecords(s.buf.snapshot()), true)
}

func (s *ConfigMapStore) upsert(ctx context.Context, blob []byte, retry bool) error {
	var cm corev1.ConfigMap
	err := s.client.Get(ctx, client.ObjectKey{Namespace: s.namespace, Name: s.name}, &cm)
	if apierrors.IsNotFound(err) {
		create := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:       s.namespace,
				Name:            s.name,
				Labels:          s.labels,
				OwnerReferences: []metav1.OwnerReference{s.ownerRef},
			},
			BinaryData: map[string][]byte{historyKey: blob},
		}
		if createErr := s.client.Create(ctx, create); createErr != nil && !apierrors.IsAlreadyExists(createErr) {
			return fmt.Errorf("failed to create history ConfigMap: %w", createErr)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get history ConfigMap: %w", err)
	}

	if cm.BinaryData == nil {
		cm.BinaryData = map[string][]byte{}
	}
	cm.BinaryData[historyKey] = blob
	if updErr := s.client.Update(ctx, &cm); updErr != nil {
		if apierrors.IsConflict(updErr) && retry {
			return s.upsert(ctx, blob, false)
		}
		return fmt.Errorf("failed to update history ConfigMap: %w", updErr)
	}
	return nil
}
