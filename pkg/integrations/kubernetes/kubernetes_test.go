package kubernetes

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/superplanehq/superplane/pkg/core"
	"github.com/superplanehq/superplane/test/support/contexts"
)

func TestKubernetesSync(t *testing.T) {
	integration := &Kubernetes{}

	t.Run("missing clusterName returns error", func(t *testing.T) {
		err := integration.Sync(core.SyncContext{
			Configuration: map[string]any{
				"sharedSecret": "secret",
			},
			Integration: &contexts.IntegrationContext{},
		})

		require.ErrorContains(t, err, "clusterName is required")
	})

	t.Run("valid trigger-only configuration sets ready state", func(t *testing.T) {
		integrationCtx := &contexts.IntegrationContext{
			IntegrationID: "11111111-1111-1111-1111-111111111111",
		}
		err := integration.Sync(core.SyncContext{
			Configuration: map[string]any{
				"clusterName":  "production-eu-1",
				"sharedSecret": "secret",
			},
			Integration:     integrationCtx,
			WebhooksBaseURL: "https://hooks.example.com",
		})

		require.NoError(t, err)
		assert.Equal(t, "ready", integrationCtx.State)
		metadata, ok := integrationCtx.Metadata.(IntegrationMetadata)
		require.True(t, ok)
		assert.False(t, metadata.ExporterReachable)
		assert.Equal(t, "https://hooks.example.com/api/v1/integrations/11111111-1111-1111-1111-111111111111/events", metadata.WebhookURL)
		assert.Contains(t, metadata.HelmInstallCommand, metadata.WebhookURL)
		assert.Contains(t, metadata.HelmValuesSnippet, metadata.WebhookURL)
		require.Contains(t, integrationCtx.Secrets, "sharedSecret")
		assert.Equal(t, []byte("secret"), integrationCtx.Secrets["sharedSecret"].Value)
	})

	t.Run("exporterURL triggers health probe", func(t *testing.T) {
		httpCtx := &contexts.HTTPContext{
			Responses: []*http.Response{
				{
					StatusCode: http.StatusOK,
					Body:       ioNopCloser(`{"status":"ok"}`),
				},
			},
		}

		integrationCtx := &contexts.IntegrationContext{}
		err := integration.Sync(core.SyncContext{
			Configuration: map[string]any{
				"clusterName":  "production-eu-1",
				"sharedSecret": "secret",
				"exporterURL":  "https://exporter.example.com",
			},
			HTTP:            httpCtx,
			Integration:     integrationCtx,
			WebhooksBaseURL: "https://hooks.example.com",
		})

		require.NoError(t, err)
		require.Len(t, httpCtx.Requests, 1)
		assert.Equal(t, "https://exporter.example.com/healthz", httpCtx.Requests[0].URL.String())
		metadata := integrationCtx.Metadata.(IntegrationMetadata)
		assert.True(t, metadata.ExporterReachable)
	})
}

func TestKubernetesHandleRequest(t *testing.T) {
	integration := &Kubernetes{}
	first := &recordingSubscription{configuration: map[string]any{"type": onDeploymentEventSubscriptionType}}
	second := &recordingSubscription{configuration: map[string]any{"type": onDeploymentEventSubscriptionType}}
	other := &recordingSubscription{configuration: map[string]any{"type": "kubernetes.other"}}

	integrationCtx := &recordingIntegrationContext{
		IntegrationContext: &contexts.IntegrationContext{
			Configuration: map[string]any{
				"clusterName":  "production-eu-1",
				"sharedSecret": "current-secret",
			},
			Secrets: map[string]core.IntegrationSecret{
				"sharedSecret": {
					Name:  "sharedSecret",
					Value: []byte("current-secret"),
				},
			},
		},
		subscriptions: []core.IntegrationSubscriptionContext{first, second, other},
	}

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/integrations/11111111-1111-1111-1111-111111111111/events",
		bytes.NewBufferString(`{
			"clusterName":"production-eu-1",
			"namespace":"payments",
			"deploymentName":"checkout-api",
			"eventMode":"rollout_completed",
			"rolloutState":"completed"
		}`),
	)
	req.Header.Set("Authorization", "Bearer current-secret")
	recorder := httptest.NewRecorder()

	integration.HandleRequest(core.HTTPRequestContext{
		Logger:      logrus.NewEntry(logrus.New()),
		Request:     req,
		Response:    recorder,
		Integration: integrationCtx,
	})

	assert.Equal(t, http.StatusOK, recorder.Code)
	require.Len(t, first.messages, 1)
	require.Len(t, second.messages, 1)
	assert.Empty(t, other.messages)
	_, ok := first.messages[0].(DeploymentEventPayload)
	assert.True(t, ok)
}

func TestParseIntegrationConfigurationPrefersStoredSecret(t *testing.T) {
	config, err := parseIntegrationConfiguration(&contexts.IntegrationContext{
		Configuration: map[string]any{
			"clusterName":  "production-eu-1",
			"sharedSecret": "stale-secret",
			"exporterURL":  "https://exporter.example.com",
		},
		Secrets: map[string]core.IntegrationSecret{
			"sharedSecret": {
				Name:  "sharedSecret",
				Value: []byte("current-secret"),
			},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, "current-secret", config.SharedSecret)
}

func TestResolveIntegrationSharedSecretSeedsStoredSecret(t *testing.T) {
	integrationCtx := &contexts.IntegrationContext{
		Configuration: map[string]any{
			"clusterName":  "production-eu-1",
			"sharedSecret": "seed-secret",
		},
	}

	secret, err := resolveIntegrationSharedSecret(integrationCtx)

	require.NoError(t, err)
	assert.Equal(t, "seed-secret", secret)
	require.Contains(t, integrationCtx.Secrets, "sharedSecret")
	assert.Equal(t, []byte("seed-secret"), integrationCtx.Secrets["sharedSecret"].Value)
}

type recordingIntegrationContext struct {
	*contexts.IntegrationContext
	subscriptions []core.IntegrationSubscriptionContext
}

func (c *recordingIntegrationContext) ListSubscriptions() ([]core.IntegrationSubscriptionContext, error) {
	return c.subscriptions, nil
}

type recordingSubscription struct {
	configuration any
	messages      []any
}

func (s *recordingSubscription) Configuration() any {
	return s.configuration
}

func (s *recordingSubscription) SendMessage(message any) error {
	s.messages = append(s.messages, message)
	return nil
}

func ioNopCloser(body string) io.ReadCloser {
	return io.NopCloser(strings.NewReader(body))
}
