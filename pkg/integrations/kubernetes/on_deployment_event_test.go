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
	metadata := &contexts.MetadataContext{}
	webhook := &contexts.NodeWebhookContext{}
	integrationCtx := &contexts.IntegrationContext{
		Configuration: map[string]any{
			"clusterName":  "production-eu-1",
			"sharedSecret": "shared-secret",
			"exporterURL":  "https://exporter.example.com",
		},
	}

	err := trigger.Setup(core.TriggerContext{
		Integration: integrationCtx,
		Metadata:    metadata,
		Webhook:     webhook,
		Configuration: map[string]any{
			"namespace":      "payments",
			"deploymentName": "checkout-api",
			"eventMode":      EventModeRolloutCompleted,
		},
	})

	require.NoError(t, err)
	assert.Len(t, integrationCtx.WebhookRequests, 0)
	stored, ok := metadata.Metadata.(OnDeploymentEventMetadata)
	require.True(t, ok)
	assert.Equal(t, "payments", stored.Namespace)
	assert.NotEmpty(t, stored.WebhookURL)
	assert.Equal(t, "shared-secret", webhook.Secret)
	assert.Contains(t, stored.HelmInstallCommand, "helm upgrade --install")
	assert.NotContains(t, stored.HelmInstallCommand, "checkout-api")
	assert.Contains(t, stored.HelmValuesSnippet, "namespace: \"\"")
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
