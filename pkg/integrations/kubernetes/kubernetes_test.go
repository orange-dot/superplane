package kubernetes

import (
	"io"
	"net/http"
	"strings"
	"testing"

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
		integrationCtx := &contexts.IntegrationContext{}
		err := integration.Sync(core.SyncContext{
			Configuration: map[string]any{
				"clusterName":  "production-eu-1",
				"sharedSecret": "secret",
			},
			Integration: integrationCtx,
		})

		require.NoError(t, err)
		assert.Equal(t, "ready", integrationCtx.State)
		metadata, ok := integrationCtx.Metadata.(IntegrationMetadata)
		require.True(t, ok)
		assert.False(t, metadata.ExporterReachable)
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
			HTTP:        httpCtx,
			Integration: integrationCtx,
		})

		require.NoError(t, err)
		require.Len(t, httpCtx.Requests, 1)
		assert.Equal(t, "https://exporter.example.com/healthz", httpCtx.Requests[0].URL.String())
		metadata := integrationCtx.Metadata.(IntegrationMetadata)
		assert.True(t, metadata.ExporterReachable)
	})
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

func ioNopCloser(body string) io.ReadCloser {
	return io.NopCloser(strings.NewReader(body))
}
