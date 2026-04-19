package main

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	kubernetesintegration "github.com/superplanehq/superplane/pkg/integrations/kubernetes"
)

func TestAuthorizeBearer(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/commands/restart-rollout", nil)
	req.Header.Set("Authorization", "Bearer test-secret")

	assert.True(t, authorizeBearer(req, "test-secret"))
	assert.False(t, authorizeBearer(req, "other-secret"))
}

func TestNormalizeDeploymentEvent(t *testing.T) {
	now := time.Date(2026, 4, 19, 18, 0, 0, 0, time.UTC)

	t.Run("completed rollout", func(t *testing.T) {
		replicas := int32(3)
		payload := normalizeDeploymentEvent("cluster-a", deploymentResource{
			Metadata: deploymentMetadata{
				Name:            "checkout-api",
				Namespace:       "payments",
				ResourceVersion: "42",
				Generation:      7,
			},
			Spec: deploymentSpec{Replicas: &replicas},
			Status: deploymentStatus{
				ObservedGeneration:  7,
				UpdatedReplicas:     3,
				ReadyReplicas:       3,
				AvailableReplicas:   3,
				UnavailableReplicas: 0,
				Conditions: []deploymentCondition{
					{Type: "Progressing", Reason: "NewReplicaSetAvailable", Message: "Deployment has successfully progressed."},
				},
			},
		}, now)

		assert.Equal(t, kubernetesintegration.EventModeRolloutCompleted, payload.EventMode)
		assert.Equal(t, "completed", payload.RolloutState)
		assert.Equal(t, "payments", payload.Namespace)
	})

	t.Run("failed rollout", func(t *testing.T) {
		replicas := int32(2)
		payload := normalizeDeploymentEvent("cluster-a", deploymentResource{
			Metadata: deploymentMetadata{
				Name:            "checkout-api",
				Namespace:       "payments",
				ResourceVersion: "43",
				Generation:      8,
			},
			Spec: deploymentSpec{Replicas: &replicas},
			Status: deploymentStatus{
				ObservedGeneration: 8,
				Conditions: []deploymentCondition{
					{Type: "Progressing", Reason: "ProgressDeadlineExceeded", Message: "ReplicaSet did not progress in time."},
				},
			},
		}, now)

		assert.Equal(t, kubernetesintegration.EventModeRolloutFailed, payload.EventMode)
		assert.Equal(t, "failed", payload.RolloutState)
	})
}

func TestHandleRestartRollout(t *testing.T) {
	now := time.Date(2026, 4, 19, 18, 10, 0, 0, time.UTC)
	app := &app{
		cfg: config{
			ClusterName:            "cluster-a",
			SuperplaneSharedSecret: "secret",
			WatchNamespace:         "payments",
			KubeAPIURL:             "https://kube.example.com",
			ServiceAccountToken:    "kube-token",
		},
		kubeClient: &http.Client{
			Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				assert.Equal(t, http.MethodPatch, req.Method)
				assert.Equal(t, "/apis/apps/v1/namespaces/payments/deployments/checkout-api", req.URL.Path)
				assert.Equal(t, "Bearer kube-token", req.Header.Get("Authorization"))
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       http.NoBody,
					Header:     make(http.Header),
				}, nil
			}),
		},
		logger: testLogger(),
	}

	payload, err := app.restartRollout(t.Context(), "payments", "checkout-api", now)
	require.NoError(t, err)
	require.NotNil(t, payload)
	assert.Equal(t, "requested", payload.Status)
	assert.Equal(t, now.Format(time.RFC3339), payload.RestartedAt)
}

func TestResolveTarget(t *testing.T) {
	exporter := &app{cfg: config{WatchNamespace: "payments"}}

	t.Run("requires namespace and deployment", func(t *testing.T) {
		_, _, err := exporter.resolveTarget(kubernetesintegration.RestartRolloutCommand{})
		require.ErrorContains(t, err, "namespace is required")
	})

	t.Run("rejects namespace outside scoped exporter", func(t *testing.T) {
		_, _, err := exporter.resolveTarget(kubernetesintegration.RestartRolloutCommand{
			Namespace:      "identity",
			DeploymentName: "api",
		})
		require.ErrorContains(t, err, "scope does not include namespace")
	})

	t.Run("cluster scoped exporter accepts any namespace", func(t *testing.T) {
		clusterApp := &app{cfg: config{}}
		namespace, deployment, err := clusterApp.resolveTarget(kubernetesintegration.RestartRolloutCommand{
			Namespace:      "identity",
			DeploymentName: "api",
		})
		require.NoError(t, err)
		assert.Equal(t, "identity", namespace)
		assert.Equal(t, "api", deployment)
	})
}

func TestDiffDeploymentResourceVersions(t *testing.T) {
	replicas := int32(1)
	initial := []deploymentResource{
		{
			Metadata: deploymentMetadata{Name: "checkout-api", Namespace: "payments", ResourceVersion: "10"},
			Spec:     deploymentSpec{Replicas: &replicas},
		},
		{
			Metadata: deploymentMetadata{Name: "worker", Namespace: "payments", ResourceVersion: "20"},
			Spec:     deploymentSpec{Replicas: &replicas},
		},
	}

	changed, snapshot := diffDeploymentResourceVersions(nil, initial)
	require.Empty(t, changed)
	require.Len(t, snapshot, 2)

	next := []deploymentResource{
		{
			Metadata: deploymentMetadata{Name: "checkout-api", Namespace: "payments", ResourceVersion: "11"},
			Spec:     deploymentSpec{Replicas: &replicas},
		},
		{
			Metadata: deploymentMetadata{Name: "worker", Namespace: "payments", ResourceVersion: "20"},
			Spec:     deploymentSpec{Replicas: &replicas},
		},
		{
			Metadata: deploymentMetadata{Name: "billing-api", Namespace: "payments", ResourceVersion: "30"},
			Spec:     deploymentSpec{Replicas: &replicas},
		},
	}

	changed, snapshot = diffDeploymentResourceVersions(snapshot, next)
	require.Len(t, changed, 2)
	assert.Equal(t, "payments/checkout-api", deploymentKey(changed[0]))
	assert.Equal(t, "payments/billing-api", deploymentKey(changed[1]))
	require.Len(t, snapshot, 3)
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func testLogger() *log.Logger {
	return log.New(io.Discard, "", 0)
}
