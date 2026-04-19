package kubernetes

import (
	"net/http"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/superplanehq/superplane/pkg/core"
	"github.com/superplanehq/superplane/test/support/contexts"
)

func TestOnDeploymentEventSetup(t *testing.T) {
	trigger := &OnDeploymentEvent{}
	integrationCtx := &contexts.IntegrationContext{
		IntegrationID: "11111111-1111-1111-1111-111111111111",
		Configuration: map[string]any{
			"clusterName":  "production-eu-1",
			"sharedSecret": "shared-secret",
			"exporterURL":  "https://exporter.example.com",
		},
	}

	setupTrigger := func(namespace string, deploymentName string) OnDeploymentEventMetadata {
		metadata := &contexts.MetadataContext{}
		webhook := &contexts.NodeWebhookContext{}

		err := trigger.Setup(core.TriggerContext{
			Integration: integrationCtx,
			Metadata:    metadata,
			Webhook:     webhook,
			Configuration: map[string]any{
				"namespace":      namespace,
				"deploymentName": deploymentName,
				"eventMode":      EventModeRolloutCompleted,
			},
		})

		require.NoError(t, err)
		assert.Len(t, integrationCtx.WebhookRequests, 0)
		assert.Empty(t, webhook.Secret)

		stored, ok := metadata.Metadata.(OnDeploymentEventMetadata)
		require.True(t, ok)
		return stored
	}

	first := setupTrigger("payments", "checkout-api")
	second := setupTrigger("identity", "api")

	assert.Equal(t, "payments", first.Namespace)
	assert.Equal(t, "identity", second.Namespace)
	assert.Equal(t, first.WebhookURL, second.WebhookURL)
	assert.Equal(t, "http://localhost:3000/api/v1/integrations/11111111-1111-1111-1111-111111111111/events", first.WebhookURL)
	assert.Contains(t, first.HelmInstallCommand, "helm upgrade --install")
	assert.NotContains(t, first.HelmInstallCommand, "checkout-api")
	assert.Contains(t, first.HelmValuesSnippet, "namespace: \"\"")
	require.Len(t, integrationCtx.Subscriptions, 2)
	assert.Equal(t, map[string]any{"type": onDeploymentEventSubscriptionType}, integrationCtx.Subscriptions[0].Configuration)
	assert.Equal(t, map[string]any{"type": onDeploymentEventSubscriptionType}, integrationCtx.Subscriptions[1].Configuration)
}

func TestOnDeploymentEventHandleWebhook(t *testing.T) {
	trigger := &OnDeploymentEvent{}
	baseContext := core.WebhookRequestContext{
		Logger: log.NewEntry(log.New()),
		Configuration: map[string]any{
			"namespace":      "payments",
			"deploymentName": "checkout-api",
			"eventMode":      EventModeRolloutCompleted,
		},
		Webhook: &contexts.NodeWebhookContext{Secret: "shared-secret"},
	}

	t.Run("invalid bearer token returns unauthorized", func(t *testing.T) {
		code, _, err := trigger.HandleWebhook(core.WebhookRequestContext{
			Body:          []byte(`{}`),
			Headers:       http.Header{"Authorization": []string{"Bearer wrong"}},
			Configuration: baseContext.Configuration,
			Webhook:       baseContext.Webhook,
			Events:        &contexts.EventContext{},
			Logger:        baseContext.Logger,
		})

		assert.Equal(t, http.StatusUnauthorized, code)
		require.ErrorContains(t, err, "invalid bearer authorization")
	})

	t.Run("matching completed event emits payload", func(t *testing.T) {
		events := &contexts.EventContext{}
		code, _, err := trigger.HandleWebhook(core.WebhookRequestContext{
			Body: []byte(`{
				"clusterName":"production-eu-1",
				"namespace":"payments",
				"deploymentName":"checkout-api",
				"eventMode":"rollout_completed",
				"rolloutState":"completed",
				"timestamp":"2026-04-19T18:00:00Z"
			}`),
			Headers:       http.Header{"Authorization": []string{"Bearer shared-secret"}},
			Configuration: baseContext.Configuration,
			Webhook:       baseContext.Webhook,
			Events:        events,
			Logger:        baseContext.Logger,
		})

		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, code)
		require.Len(t, events.Payloads, 1)
		assert.Equal(t, KubernetesDeploymentEventPayloadType, events.Payloads[0].Type)
	})

	t.Run("current integration secret overrides stale webhook secret", func(t *testing.T) {
		events := &contexts.EventContext{}
		code, _, err := trigger.HandleWebhook(core.WebhookRequestContext{
			Body: []byte(`{
				"clusterName":"production-eu-1",
				"namespace":"payments",
				"deploymentName":"checkout-api",
				"eventMode":"rollout_completed",
				"rolloutState":"completed",
				"timestamp":"2026-04-19T18:00:00Z"
			}`),
			Headers:       http.Header{"Authorization": []string{"Bearer current-secret"}},
			Configuration: baseContext.Configuration,
			Webhook:       &contexts.NodeWebhookContext{Secret: "stale-secret"},
			Integration: &contexts.IntegrationContext{
				Configuration: map[string]any{
					"clusterName":  "production-eu-1",
					"sharedSecret": "current-secret",
				},
			},
			Events: events,
			Logger: baseContext.Logger,
		})

		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, code)
		require.Len(t, events.Payloads, 1)
	})

	t.Run("event mode mismatch is ignored", func(t *testing.T) {
		events := &contexts.EventContext{}
		code, _, err := trigger.HandleWebhook(core.WebhookRequestContext{
			Body: []byte(`{
				"clusterName":"production-eu-1",
				"namespace":"payments",
				"deploymentName":"checkout-api",
				"eventMode":"any_change",
				"rolloutState":"progressing",
				"timestamp":"2026-04-19T18:01:00Z"
			}`),
			Headers:       http.Header{"Authorization": []string{"Bearer shared-secret"}},
			Configuration: baseContext.Configuration,
			Webhook:       baseContext.Webhook,
			Events:        events,
			Logger:        baseContext.Logger,
		})

		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, code)
		assert.Len(t, events.Payloads, 0)
	})
}

func TestOnDeploymentEventOnIntegrationMessage(t *testing.T) {
	trigger := &OnDeploymentEvent{}
	logger := log.NewEntry(log.New())

	t.Run("matching deployment emits payload", func(t *testing.T) {
		events := &contexts.EventContext{}

		err := trigger.OnIntegrationMessage(core.IntegrationMessageContext{
			Logger: logger,
			Configuration: map[string]any{
				"namespace":      "payments",
				"deploymentName": "checkout-api",
				"eventMode":      EventModeRolloutCompleted,
			},
			Message: DeploymentEventPayload{
				ClusterName:    "production-eu-1",
				Namespace:      "payments",
				DeploymentName: "checkout-api",
				EventMode:      EventModeRolloutCompleted,
				RolloutState:   "completed",
			},
			Events: events,
		})

		require.NoError(t, err)
		require.Len(t, events.Payloads, 1)
		assert.Equal(t, KubernetesDeploymentEventPayloadType, events.Payloads[0].Type)
	})

	t.Run("non-matching deployment is ignored", func(t *testing.T) {
		events := &contexts.EventContext{}

		err := trigger.OnIntegrationMessage(core.IntegrationMessageContext{
			Logger: logger,
			Configuration: map[string]any{
				"namespace":      "payments",
				"deploymentName": "checkout-api",
				"eventMode":      EventModeRolloutCompleted,
			},
			Message: DeploymentEventPayload{
				ClusterName:    "production-eu-1",
				Namespace:      "identity",
				DeploymentName: "api",
				EventMode:      EventModeRolloutCompleted,
				RolloutState:   "completed",
			},
			Events: events,
		})

		require.NoError(t, err)
		assert.Len(t, events.Payloads, 0)
	})
}
