package kubernetes

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/mitchellh/mapstructure"
	"github.com/superplanehq/superplane/pkg/configuration"
	"github.com/superplanehq/superplane/pkg/core"
)

type RestartRollout struct{}

type RestartRolloutConfiguration struct {
	Namespace      string `json:"namespace" mapstructure:"namespace"`
	DeploymentName string `json:"deploymentName" mapstructure:"deploymentName"`
}

type RestartRolloutMetadata struct {
	ClusterName    string `json:"clusterName" mapstructure:"clusterName"`
	Namespace      string `json:"namespace" mapstructure:"namespace"`
	DeploymentName string `json:"deploymentName" mapstructure:"deploymentName"`
	ExporterURL    string `json:"exporterUrl,omitempty" mapstructure:"exporterUrl"`
}

func (c *RestartRollout) Name() string {
	return "kubernetes.restartRollout"
}

func (c *RestartRollout) Label() string {
	return "Restart Rollout"
}

func (c *RestartRollout) Description() string {
	return "Request a rollout restart for a Kubernetes Deployment"
}

func (c *RestartRollout) Documentation() string {
	return `The Restart Rollout component asks the Kubernetes exporter to patch a Deployment with the standard ` + "`kubectl.kubernetes.io/restartedAt`" + ` annotation.

## Configuration

- **Namespace**: Namespace containing the target Deployment
- **Deployment Name**: Name of the Deployment to restart

## Requirements

- The Kubernetes integration must have a reachable **Exporter Base URL**
- The in-cluster exporter must have permission to patch Deployments`
}

func (c *RestartRollout) Icon() string {
	return "kubernetes"
}

func (c *RestartRollout) Color() string {
	return "gray"
}

func (c *RestartRollout) ExampleOutput() map[string]any {
	return restartRolloutExampleOutput()
}

func (c *RestartRollout) OutputChannels(configuration any) []core.OutputChannel {
	return []core.OutputChannel{core.DefaultOutputChannel}
}

func (c *RestartRollout) Configuration() []configuration.Field {
	return []configuration.Field{
		{
			Name:        "namespace",
			Label:       "Namespace",
			Type:        configuration.FieldTypeString,
			Required:    true,
			Placeholder: "default",
			Description: "Namespace containing the Deployment to restart.",
		},
		{
			Name:        "deploymentName",
			Label:       "Deployment Name",
			Type:        configuration.FieldTypeString,
			Required:    true,
			Placeholder: "checkout-api",
			Description: "Deployment that should receive a rollout restart.",
		},
	}
}

func (c *RestartRollout) Setup(ctx core.SetupContext) error {
	config, err := parseRestartRolloutConfiguration(ctx.Configuration)
	if err != nil {
		return err
	}

	integrationConfig, err := parseIntegrationConfiguration(ctx.Integration)
	if err != nil {
		return err
	}

	if strings.TrimSpace(integrationConfig.ExporterURL) == "" {
		return fmt.Errorf("exporterURL is required on the Kubernetes integration for restartRollout")
	}

	if ctx.Metadata == nil {
		return nil
	}

	return ctx.Metadata.Set(RestartRolloutMetadata{
		ClusterName:    integrationConfig.ClusterName,
		Namespace:      config.Namespace,
		DeploymentName: config.DeploymentName,
		ExporterURL:    integrationConfig.ExporterURL,
	})
}

func (c *RestartRollout) ProcessQueueItem(ctx core.ProcessQueueContext) (*uuid.UUID, error) {
	return ctx.DefaultProcessing()
}

func (c *RestartRollout) Execute(ctx core.ExecutionContext) error {
	config, err := parseRestartRolloutConfiguration(ctx.Configuration)
	if err != nil {
		return err
	}

	integrationConfig, err := parseIntegrationConfiguration(ctx.Integration)
	if err != nil {
		return err
	}

	if strings.TrimSpace(integrationConfig.ExporterURL) == "" {
		return fmt.Errorf("exporterURL is required on the Kubernetes integration for restartRollout")
	}

	client := NewClient(ctx.HTTP, integrationConfig.ExporterURL, integrationConfig.SharedSecret)
	payload, err := client.RestartRollout(RestartRolloutCommand{
		Namespace:      config.Namespace,
		DeploymentName: config.DeploymentName,
	})
	if err != nil {
		return err
	}

	if payload.ClusterName == "" {
		payload.ClusterName = integrationConfig.ClusterName
	}

	return ctx.ExecutionState.Emit(core.DefaultOutputChannel.Name, KubernetesDeploymentRestartPayloadType, []any{payload})
}

func (c *RestartRollout) Actions() []core.Action {
	return []core.Action{}
}

func (c *RestartRollout) HandleAction(ctx core.ActionContext) error {
	return nil
}

func (c *RestartRollout) HandleWebhook(ctx core.WebhookRequestContext) (int, *core.WebhookResponseBody, error) {
	return http.StatusOK, nil, nil
}

func (c *RestartRollout) Cancel(ctx core.ExecutionContext) error {
	return nil
}

func (c *RestartRollout) Cleanup(ctx core.SetupContext) error {
	return nil
}

func parseRestartRolloutConfiguration(configuration any) (RestartRolloutConfiguration, error) {
	config := RestartRolloutConfiguration{}
	if err := mapstructure.Decode(configuration, &config); err != nil {
		return config, fmt.Errorf("failed to decode configuration: %w", err)
	}

	config.Namespace = strings.TrimSpace(config.Namespace)
	config.DeploymentName = strings.TrimSpace(config.DeploymentName)

	if config.Namespace == "" {
		return config, fmt.Errorf("namespace is required")
	}

	if config.DeploymentName == "" {
		return config, fmt.Errorf("deploymentName is required")
	}

	return config, nil
}
