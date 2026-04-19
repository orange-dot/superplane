package kubernetes

import (
	"fmt"
	"strings"

	"github.com/mitchellh/mapstructure"
	"github.com/superplanehq/superplane/pkg/configuration"
	"github.com/superplanehq/superplane/pkg/core"
	"github.com/superplanehq/superplane/pkg/registry"
)

const (
	KubernetesDeploymentEventPayloadType   = "kubernetes.deployment.event"
	KubernetesDeploymentRestartPayloadType = "kubernetes.deployment.restart"
	integrationSharedSecretName            = "sharedSecret"

	EventModeAnyChange        = "any_change"
	EventModeRolloutCompleted = "rollout_completed"
	EventModeRolloutFailed    = "rollout_failed"
)

func init() {
	registry.RegisterIntegration("kubernetes", &Kubernetes{})
}

type Kubernetes struct{}

type IntegrationConfiguration struct {
	ClusterName  string `json:"clusterName" mapstructure:"clusterName"`
	ExporterURL  string `json:"exporterURL" mapstructure:"exporterURL"`
	SharedSecret string `json:"sharedSecret" mapstructure:"sharedSecret"`
}

type IntegrationMetadata struct {
	ClusterName            string `json:"clusterName" mapstructure:"clusterName"`
	ExporterURL            string `json:"exporterURL,omitempty" mapstructure:"exporterURL"`
	SharedSecretConfigured bool   `json:"sharedSecretConfigured" mapstructure:"sharedSecretConfigured"`
	ExporterReachable      bool   `json:"exporterReachable" mapstructure:"exporterReachable"`
}

type DeploymentEventPayload struct {
	ClusterName         string `json:"clusterName" mapstructure:"clusterName"`
	Namespace           string `json:"namespace" mapstructure:"namespace"`
	DeploymentName      string `json:"deploymentName" mapstructure:"deploymentName"`
	EventMode           string `json:"eventMode" mapstructure:"eventMode"`
	RolloutState        string `json:"rolloutState" mapstructure:"rolloutState"`
	Reason              string `json:"reason,omitempty" mapstructure:"reason"`
	Message             string `json:"message,omitempty" mapstructure:"message"`
	ObservedGeneration  int64  `json:"observedGeneration" mapstructure:"observedGeneration"`
	Generation          int64  `json:"generation" mapstructure:"generation"`
	ResourceVersion     string `json:"resourceVersion,omitempty" mapstructure:"resourceVersion"`
	DesiredReplicas     int32  `json:"desiredReplicas" mapstructure:"desiredReplicas"`
	UpdatedReplicas     int32  `json:"updatedReplicas" mapstructure:"updatedReplicas"`
	ReadyReplicas       int32  `json:"readyReplicas" mapstructure:"readyReplicas"`
	AvailableReplicas   int32  `json:"availableReplicas" mapstructure:"availableReplicas"`
	UnavailableReplicas int32  `json:"unavailableReplicas" mapstructure:"unavailableReplicas"`
	Timestamp           string `json:"timestamp" mapstructure:"timestamp"`
}

type RestartRolloutCommand struct {
	Namespace      string `json:"namespace" mapstructure:"namespace"`
	DeploymentName string `json:"deploymentName" mapstructure:"deploymentName"`
}

type RestartRolloutPayload struct {
	ClusterName    string `json:"clusterName" mapstructure:"clusterName"`
	Namespace      string `json:"namespace" mapstructure:"namespace"`
	DeploymentName string `json:"deploymentName" mapstructure:"deploymentName"`
	RestartedAt    string `json:"restartedAt" mapstructure:"restartedAt"`
	Status         string `json:"status" mapstructure:"status"`
}

func (k *Kubernetes) Name() string {
	return "kubernetes"
}

func (k *Kubernetes) Label() string {
	return "Kubernetes"
}

func (k *Kubernetes) Icon() string {
	return "kubernetes"
}

func (k *Kubernetes) Description() string {
	return "Connect a Kubernetes cluster through a small in-cluster exporter"
}

func (k *Kubernetes) Instructions() string {
	return `## Connection

Configure this integration with:
- **Cluster Name**: A human-readable label for the target cluster
- **Shared Secret**: A secret used by the exporter to authenticate deployment events and restart commands
- **Exporter Base URL** (optional): A URL SuperPlane can call for rollout restarts and health checks

## Exporter-first setup

This integration expects a small exporter to run inside the target cluster.

1. Create the integration in SuperPlane with a cluster name and shared secret.
2. Add an **On Deployment Event** trigger to generate the webhook URL and Helm install snippet.
3. Install one exporter per cluster with that webhook URL and the same shared secret.
4. Add as many ` + "`namespace + deploymentName`" + ` triggers as needed; SuperPlane filters the shared event stream per node.
5. If you want the **Restart Rollout** action, expose the exporter with a reachable URL and paste that URL into **Exporter Base URL**.

The v1 base intentionally stays narrow:
- Deployments only
- Typed namespace/name configuration
- No in-product cluster browsing or resource pickers`
}

func (k *Kubernetes) Configuration() []configuration.Field {
	return []configuration.Field{
		{
			Name:        "clusterName",
			Label:       "Cluster Name",
			Type:        configuration.FieldTypeString,
			Required:    true,
			Placeholder: "production-eu-1",
			Description: "Human-readable cluster label shown in workflow nodes and execution details.",
		},
		{
			Name:        "sharedSecret",
			Label:       "Shared Secret",
			Type:        configuration.FieldTypeString,
			Required:    true,
			Sensitive:   true,
			Description: "Shared bearer secret used between SuperPlane and the in-cluster exporter.",
		},
		{
			Name:        "exporterURL",
			Label:       "Exporter Base URL",
			Type:        configuration.FieldTypeString,
			Required:    false,
			Placeholder: "https://kubernetes-exporter.example.com",
			Description: "Optional URL SuperPlane can call for health checks and rollout restarts.",
		},
	}
}

func (k *Kubernetes) Components() []core.Component {
	return []core.Component{
		&RestartRollout{},
	}
}

func (k *Kubernetes) Triggers() []core.Trigger {
	return []core.Trigger{
		&OnDeploymentEvent{},
	}
}

func (k *Kubernetes) Sync(ctx core.SyncContext) error {
	config := IntegrationConfiguration{}
	if err := mapstructure.Decode(ctx.Configuration, &config); err != nil {
		return fmt.Errorf("failed to decode configuration: %w", err)
	}

	if err := validateIntegrationConfiguration(config); err != nil {
		return err
	}

	metadata := IntegrationMetadata{
		ClusterName:            strings.TrimSpace(config.ClusterName),
		ExporterURL:            strings.TrimSpace(config.ExporterURL),
		SharedSecretConfigured: strings.TrimSpace(config.SharedSecret) != "",
		ExporterReachable:      false,
	}

	if err := ctx.Integration.SetSecret(integrationSharedSecretName, []byte(strings.TrimSpace(config.SharedSecret))); err != nil {
		return fmt.Errorf("failed to store shared secret: %w", err)
	}

	if metadata.ExporterURL != "" {
		client := NewClient(ctx.HTTP, metadata.ExporterURL, strings.TrimSpace(config.SharedSecret))
		if err := client.Healthz(); err != nil {
			return fmt.Errorf("failed to reach exporter health endpoint: %w", err)
		}
		metadata.ExporterReachable = true
	}

	ctx.Integration.SetMetadata(metadata)
	ctx.Integration.Ready()
	return nil
}

func validateIntegrationConfiguration(config IntegrationConfiguration) error {
	if strings.TrimSpace(config.ClusterName) == "" {
		return fmt.Errorf("clusterName is required")
	}

	if strings.TrimSpace(config.SharedSecret) == "" {
		return fmt.Errorf("sharedSecret is required")
	}

	return nil
}

func parseIntegrationConfiguration(integration core.IntegrationContext) (IntegrationConfiguration, error) {
	config := IntegrationConfiguration{}

	clusterName, err := integration.GetConfig("clusterName")
	if err != nil {
		return config, fmt.Errorf("clusterName is required")
	}

	exporterURL, _ := integration.GetConfig("exporterURL")

	config.ClusterName = strings.TrimSpace(string(clusterName))
	config.ExporterURL = strings.TrimSpace(string(exporterURL))
	config.SharedSecret, err = resolveIntegrationSharedSecret(integration)
	if err != nil {
		return config, err
	}

	if config.ClusterName == "" {
		return config, fmt.Errorf("clusterName is required")
	}

	if config.SharedSecret == "" {
		return config, fmt.Errorf("sharedSecret is required")
	}

	return config, nil
}

func resolveIntegrationSharedSecret(integration core.IntegrationContext) (string, error) {
	secrets, err := integration.GetSecrets()
	if err == nil {
		for _, secret := range secrets {
			if secret.Name != integrationSharedSecretName {
				continue
			}

			value := strings.TrimSpace(string(secret.Value))
			if value != "" {
				return value, nil
			}
		}
	}

	sharedSecret, err := integration.GetConfig("sharedSecret")
	if err != nil {
		return "", fmt.Errorf("sharedSecret is required")
	}

	trimmed := strings.TrimSpace(string(sharedSecret))
	if trimmed == "" {
		return "", fmt.Errorf("sharedSecret is required")
	}

	return trimmed, nil
}

func (k *Kubernetes) Cleanup(ctx core.IntegrationCleanupContext) error {
	return nil
}

func (k *Kubernetes) Actions() []core.Action {
	return []core.Action{}
}

func (k *Kubernetes) HandleAction(ctx core.IntegrationActionContext) error {
	return nil
}

func (k *Kubernetes) ListResources(resourceType string, ctx core.ListResourcesContext) ([]core.IntegrationResource, error) {
	return []core.IntegrationResource{}, nil
}

func (k *Kubernetes) HandleRequest(ctx core.HTTPRequestContext) {
}
