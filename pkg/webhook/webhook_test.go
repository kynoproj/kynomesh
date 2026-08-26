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

package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"

	kmv1 "github.com/kynoproj/kynomesh/pkg/apis/kynomesh/v1alpha1"
	"github.com/kynoproj/kynomesh/pkg/shared/logging"
)

func newTestController(objs ...runtime.Object) *AdmissionController {
	return &AdmissionController{
		Client: fake.NewClientset(objs...),
		Options: Options{
			WebhookName:     "webhook.kynomesh.kyno.sh",
			ServiceName:     "kynomesh-webhook",
			DeploymentName:  "kynomesh-webhook",
			ClusterRoleName: "kynomesh-webhook",
			SecretName:      "kynomesh-webhook-certs",
			Namespace:       "kynomesh-system",
			Port:            443,
		},
		Handlers: map[schema.GroupVersionKind]runtime.Object{
			kmv1.AgentSetGroupVersionKind: &kmv1.AgentSet{},
		},
		Logger: logging.NewLogger(),
	}
}

func testClusterRole(name string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: name}}
}

func TestAdmissionController_ServeHTTP(t *testing.T) {
	t.Run("rejects non-JSON content type", func(t *testing.T) {
		ac := newTestController()
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("{}"))
		req.Header.Set("Content-Type", "text/plain")
		rec := httptest.NewRecorder()

		ac.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnsupportedMediaType, rec.Code)
	})

	t.Run("rejects malformed body", func(t *testing.T) {
		ac := newTestController()
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString("not-json"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		ac.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("allows a valid AgentSet create", func(t *testing.T) {
		ac := newTestController()
		newObj, err := json.Marshal(validAgentSetForWebhookTest())
		require.NoError(t, err)

		review := admissionv1.AdmissionReview{
			Request: &admissionv1.AdmissionRequest{
				UID:       "abc-123",
				Operation: admissionv1.Create,
				Kind:      metav1.GroupVersionKind(kmv1.AgentSetGroupVersionKind),
				Object:    runtime.RawExtension{Raw: newObj},
			},
		}
		body, err := json.Marshal(review)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		ac.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		var resp admissionv1.AdmissionReview
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
		require.NotNil(t, resp.Response)
		assert.True(t, resp.Response.Allowed)
		assert.Equal(t, review.Request.UID, resp.Response.UID)
	})

	t.Run("denies an invalid AgentSet create", func(t *testing.T) {
		ac := newTestController()
		invalid := validAgentSetForWebhookTest()
		invalid.Spec.Agents = nil
		newObj, err := json.Marshal(invalid)
		require.NoError(t, err)

		review := admissionv1.AdmissionReview{
			Request: &admissionv1.AdmissionRequest{
				UID:       "abc-124",
				Operation: admissionv1.Create,
				Kind:      metav1.GroupVersionKind(kmv1.AgentSetGroupVersionKind),
				Object:    runtime.RawExtension{Raw: newObj},
			},
		}
		body, err := json.Marshal(review)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		ac.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		var resp admissionv1.AdmissionReview
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
		require.NotNil(t, resp.Response)
		assert.False(t, resp.Response.Allowed)
	})
}

func TestAdmissionController_Admit(t *testing.T) {
	ac := newTestController()

	t.Run("allows operations other than create/update without validation", func(t *testing.T) {
		resp := ac.admit(context.Background(), &admissionv1.AdmissionRequest{
			Operation: admissionv1.Delete,
			Kind:      metav1.GroupVersionKind(kmv1.AgentSetGroupVersionKind),
		})
		require.NotNil(t, resp)
		assert.True(t, resp.Allowed)
	})

	t.Run("denies when no validator can be resolved for the kind", func(t *testing.T) {
		resp := ac.admit(context.Background(), &admissionv1.AdmissionRequest{
			Operation: admissionv1.Create,
			Kind:      metav1.GroupVersionKind{Group: "kynomesh.kyno.sh", Version: "v1alpha1", Kind: "NotAKind"},
		})
		require.NotNil(t, resp)
		assert.False(t, resp.Allowed)
	})

	t.Run("routes create to ValidateCreate", func(t *testing.T) {
		newObj, err := json.Marshal(validAgentSetForWebhookTest())
		require.NoError(t, err)
		resp := ac.admit(context.Background(), &admissionv1.AdmissionRequest{
			Operation: admissionv1.Create,
			Kind:      metav1.GroupVersionKind(kmv1.AgentSetGroupVersionKind),
			Object:    runtime.RawExtension{Raw: newObj},
		})
		require.NotNil(t, resp)
		assert.True(t, resp.Allowed)
	})

	t.Run("routes update to ValidateUpdate", func(t *testing.T) {
		oldObj, err := json.Marshal(validAgentSetForWebhookTest())
		require.NoError(t, err)
		invalid := validAgentSetForWebhookTest()
		invalid.Spec.Agents = nil
		newObj, err := json.Marshal(invalid)
		require.NoError(t, err)

		resp := ac.admit(context.Background(), &admissionv1.AdmissionRequest{
			Operation: admissionv1.Update,
			Kind:      metav1.GroupVersionKind(kmv1.AgentSetGroupVersionKind),
			OldObject: runtime.RawExtension{Raw: oldObj},
			Object:    runtime.RawExtension{Raw: newObj},
		})
		require.NotNil(t, resp)
		assert.False(t, resp.Allowed)
	})
}

func TestAdmissionController_Register(t *testing.T) {
	t.Run("creates the webhook when absent", func(t *testing.T) {
		ac := newTestController(testClusterRole("kynomesh-webhook"))
		cl := ac.Client.AdmissionregistrationV1().ValidatingWebhookConfigurations()

		err := ac.register(context.Background(), cl, []byte("ca-bytes"))
		require.NoError(t, err)

		got, err := cl.Get(context.Background(), ac.Options.WebhookName, metav1.GetOptions{})
		require.NoError(t, err)
		require.Len(t, got.Webhooks, 1)
		assert.Equal(t, []byte("ca-bytes"), got.Webhooks[0].ClientConfig.CABundle)
		require.Len(t, got.Webhooks[0].Rules, 1)
		assert.Equal(t, []string{"agentsets"}, got.Webhooks[0].Rules[0].Resources)
		require.Len(t, got.OwnerReferences, 1)
		assert.Equal(t, "ClusterRole", got.OwnerReferences[0].Kind)
	})

	t.Run("is a no-op when the existing webhook already matches", func(t *testing.T) {
		ac := newTestController(testClusterRole("kynomesh-webhook"))
		cl := ac.Client.AdmissionregistrationV1().ValidatingWebhookConfigurations()

		require.NoError(t, ac.register(context.Background(), cl, []byte("ca-bytes")))
		before, err := cl.Get(context.Background(), ac.Options.WebhookName, metav1.GetOptions{})
		require.NoError(t, err)

		require.NoError(t, ac.register(context.Background(), cl, []byte("ca-bytes")))
		after, err := cl.Get(context.Background(), ac.Options.WebhookName, metav1.GetOptions{})
		require.NoError(t, err)

		assert.Equal(t, before.ResourceVersion, after.ResourceVersion)
	})

	t.Run("updates the webhook when the CA bundle changes", func(t *testing.T) {
		ac := newTestController(testClusterRole("kynomesh-webhook"))
		cl := ac.Client.AdmissionregistrationV1().ValidatingWebhookConfigurations()

		require.NoError(t, ac.register(context.Background(), cl, []byte("ca-bytes-1")))
		require.NoError(t, ac.register(context.Background(), cl, []byte("ca-bytes-2")))

		got, err := cl.Get(context.Background(), ac.Options.WebhookName, metav1.GetOptions{})
		require.NoError(t, err)
		require.Len(t, got.Webhooks, 1)
		assert.Equal(t, []byte("ca-bytes-2"), got.Webhooks[0].ClientConfig.CABundle)
	})

	t.Run("errors when the owning ClusterRole is missing", func(t *testing.T) {
		ac := newTestController()
		cl := ac.Client.AdmissionregistrationV1().ValidatingWebhookConfigurations()

		err := ac.register(context.Background(), cl, []byte("ca-bytes"))
		assert.Error(t, err)
	})
}

func TestAdmissionController_GetOrGenerateKeyCertsFromSecret(t *testing.T) {
	t.Run("generates and persists a new secret when none exists", func(t *testing.T) {
		deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
			Name:      "kynomesh-webhook",
			Namespace: "kynomesh-system",
		}}
		ac := newTestController(deployment)

		serverKey, serverCert, caCert, err := ac.getOrGenerateKeyCertsFromSecret(context.Background())
		require.NoError(t, err)
		assert.NotEmpty(t, serverKey)
		assert.NotEmpty(t, serverCert)
		assert.NotEmpty(t, caCert)

		secret, err := ac.Client.CoreV1().Secrets(ac.Options.Namespace).Get(context.Background(), ac.Options.SecretName, metav1.GetOptions{})
		require.NoError(t, err)
		assert.Equal(t, serverKey, secret.Data[secretServerKey])
		assert.Equal(t, serverCert, secret.Data[secretServerCert])
		assert.Equal(t, caCert, secret.Data[secretCACert])
	})

	t.Run("reuses an existing secret", func(t *testing.T) {
		ac := newTestController()
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: ac.Options.SecretName, Namespace: ac.Options.Namespace},
			Data: map[string][]byte{
				secretServerKey:  []byte("existing-key"),
				secretServerCert: []byte("existing-cert"),
				secretCACert:     []byte("existing-ca"),
			},
		}
		_, err := ac.Client.CoreV1().Secrets(ac.Options.Namespace).Create(context.Background(), secret, metav1.CreateOptions{})
		require.NoError(t, err)

		serverKey, serverCert, caCert, err := ac.getOrGenerateKeyCertsFromSecret(context.Background())
		require.NoError(t, err)
		assert.Equal(t, secret.Data[secretServerKey], serverKey)
		assert.Equal(t, secret.Data[secretServerCert], serverCert)
		assert.Equal(t, secret.Data[secretCACert], caCert)
	})

	t.Run("errors when the backing deployment for a new secret is missing", func(t *testing.T) {
		ac := newTestController()
		_, _, _, err := ac.getOrGenerateKeyCertsFromSecret(context.Background())
		assert.Error(t, err)
	})
}

func validAgentSetForWebhookTest() *kmv1.AgentSet {
	return &kmv1.AgentSet{
		ObjectMeta: metav1.ObjectMeta{Name: "greeter"},
		Spec: kmv1.AgentSetSpec{
			Pattern: kmv1.AgentPatternSupervisor,
			Entry:   "a",
			Agents: []kmv1.AbstractAgentDeploy{
				{Name: "a"},
				{Name: "b"},
			},
		},
	}
}
