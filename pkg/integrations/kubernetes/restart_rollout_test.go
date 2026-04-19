package kubernetes

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/superplanehq/superplane/pkg/core"
	"github.com/superplanehq/superplane/test/support/contexts"
)

func TestRestartRolloutSetup(t *testing.T) {
	component := &RestartRollout{}

	err := component.Setup(core.SetupContext{
		Integration: &contexts.IntegrationContext{
			Configuration: map[string]any{
				"clusterName":  "production-eu-1",
				"sharedSecret": "shared-secret",
			},
		},
		Metadata: &contexts.MetadataContext{},
		Configuration: map[string]any{
			"namespace":      "payments",
			"deploymentName": "checkout-api",
		},
	})

	require.ErrorContains(t, err, "exporterURL is required")
}

func TestRestartRolloutExecute(t *testing.T) {
	component := &RestartRollout{}
	httpCtx := &contexts.HTTPContext{
		Responses: []*http.Response{
			{
				StatusCode: http.StatusOK,
				Body: ioNopCloser(`{
					"clusterName":"production-eu-1",
					"namespace":"payments",
					"deploymentName":"checkout-api",
					"restartedAt":"2026-04-19T18:05:00Z",
					"status":"requested"
				}`),
			},
		},
	}

	execState := &contexts.ExecutionStateContext{}
	err := component.Execute(core.ExecutionContext{
		Configuration: map[string]any{
			"namespace":      "payments",
			"deploymentName": "checkout-api",
		},
		HTTP: httpCtx,
		Integration: &contexts.IntegrationContext{
			Configuration: map[string]any{
				"clusterName":  "production-eu-1",
				"sharedSecret": "shared-secret",
				"exporterURL":  "https://exporter.example.com",
			},
		},
		ExecutionState: execState,
	})

	require.NoError(t, err)
	require.Len(t, httpCtx.Requests, 1)
	assert.Equal(t, "https://exporter.example.com/commands/restart-rollout", httpCtx.Requests[0].URL.String())
	assert.Equal(t, "Bearer shared-secret", httpCtx.Requests[0].Header.Get("Authorization"))
	assert.Equal(t, KubernetesDeploymentRestartPayloadType, execState.Type)
	require.Len(t, execState.Payloads, 1)
}
