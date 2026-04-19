package kubernetes

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/mitchellh/mapstructure"
	"github.com/superplanehq/superplane/pkg/configuration"
	"github.com/superplanehq/superplane/pkg/core"
)

type OnDeploymentEvent struct{}

type OnDeploymentEventConfiguration struct {
	Namespace      string `json:"namespace" mapstructure:"namespace"`
	DeploymentName string `json:"deploymentName" mapstructure:"deploymentName"`
	EventMode      string `json:"eventMode" mapstructure:"eventMode"`
}

type OnDeploymentEventMetadata struct {
	ClusterName            string `json:"clusterName" mapstructure:"clusterName"`
	Namespace              string `json:"namespace" mapstructure:"namespace"`
	DeploymentName         string `json:"deploymentName" mapstructure:"deploymentName"`
	EventMode              string `json:"eventMode" mapstructure:"eventMode"`
	WebhookURL             string `json:"webhookUrl" mapstructure:"webhookUrl"`
	ExporterURL            string `json:"exporterUrl,omitempty" mapstructure:"exporterUrl"`
	SharedSecretConfigured bool   `json:"sharedSecretConfigured" mapstructure:"sharedSecretConfigured"`
	HelmInstallCommand     string `json:"helmInstallCommand" mapstructure:"helmInstallCommand"`
	HelmValuesSnippet      string `json:"helmValuesSnippet" mapstructure:"helmValuesSnippet"`
}

func (t *OnDeploymentEvent) Name() string {
	return "kubernetes.onDeploymentEvent"
}

func (t *OnDeploymentEvent) Label() string {
	return "On Deployment Event"
}

func (t *OnDeploymentEvent) Description() string {
	return "Trigger when the in-cluster exporter reports a Deployment change"
}

func (t *OnDeploymentEvent) Documentation() string {
	return `The On Deployment Event trigger starts a workflow execution when the Kubernetes exporter reports a Deployment change.

## What this trigger does

- Uses one shared cluster exporter
- Filters one Deployment in one namespace at the trigger level
- Receives exporter events over a SuperPlane webhook URL
- Emits normalized deployment payloads as ` + "`kubernetes.deployment.event`" + `
- Filters by event mode (` + "`any_change`" + `, ` + "`rollout_completed`" + `, or ` + "`rollout_failed`" + `)

## Configuration

- **Namespace**: Namespace containing the target Deployment
- **Deployment Name**: Name of the target Deployment
- **Event Mode**: Which exporter events should emit workflow executions

## Exporter setup

Save the trigger to generate the webhook URL and Helm install snippet. Install one exporter into the target cluster with that webhook URL and the same shared secret configured on the integration. Additional trigger nodes can reuse the same exporter install.`
}

func (t *OnDeploymentEvent) Icon() string {
	return "kubernetes"
}

func (t *OnDeploymentEvent) Color() string {
	return "gray"
}

func (t *OnDeploymentEvent) ExampleData() map[string]any {
	return onDeploymentEventExampleData()
}

func (t *OnDeploymentEvent) Configuration() []configuration.Field {
	return []configuration.Field{
		{
			Name:        "namespace",
			Label:       "Namespace",
			Type:        configuration.FieldTypeString,
			Required:    true,
			Placeholder: "default",
			Description: "Namespace containing the Deployment to watch.",
		},
		{
			Name:        "deploymentName",
			Label:       "Deployment Name",
			Type:        configuration.FieldTypeString,
			Required:    true,
			Placeholder: "checkout-api",
			Description: "Deployment name reported by the exporter.",
		},
		{
			Name:     "eventMode",
			Label:    "Event Mode",
			Type:     configuration.FieldTypeSelect,
			Required: true,
			Default:  EventModeAnyChange,
			TypeOptions: &configuration.TypeOptions{
				Select: &configuration.SelectTypeOptions{
					Options: []configuration.FieldOption{
						{Label: "Any Change", Value: EventModeAnyChange},
						{Label: "Rollout Completed", Value: EventModeRolloutCompleted},
						{Label: "Rollout Failed", Value: EventModeRolloutFailed},
					},
				},
			},
			Description: "Choose which exporter event transitions should start the workflow.",
		},
	}
}

func (t *OnDeploymentEvent) Setup(ctx core.TriggerContext) error {
	config, err := parseOnDeploymentEventConfiguration(ctx.Configuration)
	if err != nil {
		return err
	}

	integrationConfig, err := parseIntegrationConfiguration(ctx.Integration)
	if err != nil {
		return err
	}

	if ctx.Webhook == nil {
		return fmt.Errorf("missing webhook context")
	}

	webhookURL, err := ctx.Webhook.Setup()
	if err != nil {
		return fmt.Errorf("failed to setup webhook URL: %w", err)
	}

	if err := ctx.Webhook.SetSecret([]byte(integrationConfig.SharedSecret)); err != nil {
		return fmt.Errorf("failed to store webhook secret: %w", err)
	}

	metadata := OnDeploymentEventMetadata{
		ClusterName:            integrationConfig.ClusterName,
		Namespace:              config.Namespace,
		DeploymentName:         config.DeploymentName,
		EventMode:              config.EventMode,
		WebhookURL:             webhookURL,
		ExporterURL:            integrationConfig.ExporterURL,
		SharedSecretConfigured: integrationConfig.SharedSecret != "",
		HelmInstallCommand:     buildHelmInstallCommand(integrationConfig.ClusterName, webhookURL),
		HelmValuesSnippet:      buildHelmValuesSnippet(integrationConfig.ClusterName, webhookURL),
	}

	if ctx.Metadata == nil {
		return nil
	}

	return ctx.Metadata.Set(metadata)
}

func (t *OnDeploymentEvent) Actions() []core.Action {
	return []core.Action{}
}

func (t *OnDeploymentEvent) HandleAction(ctx core.TriggerActionContext) (map[string]any, error) {
	return nil, nil
}

func (t *OnDeploymentEvent) HandleWebhook(ctx core.WebhookRequestContext) (int, *core.WebhookResponseBody, error) {
	config, err := parseOnDeploymentEventConfiguration(ctx.Configuration)
	if err != nil {
		return http.StatusInternalServerError, nil, err
	}

	if err := authorizeWebhook(ctx); err != nil {
		return http.StatusUnauthorized, nil, err
	}

	payload := DeploymentEventPayload{}
	if err := json.Unmarshal(ctx.Body, &payload); err != nil {
		return http.StatusBadRequest, nil, fmt.Errorf("failed to parse deployment event: %w", err)
	}

	if strings.TrimSpace(payload.Namespace) != config.Namespace || strings.TrimSpace(payload.DeploymentName) != config.DeploymentName {
		return http.StatusOK, nil, nil
	}

	if !matchesEventMode(config.EventMode, payload.EventMode) {
		return http.StatusOK, nil, nil
	}

	if err := ctx.Events.Emit(KubernetesDeploymentEventPayloadType, payload); err != nil {
		return http.StatusInternalServerError, nil, fmt.Errorf("failed to emit deployment event: %w", err)
	}

	return http.StatusOK, nil, nil
}

func (t *OnDeploymentEvent) Cleanup(ctx core.TriggerContext) error {
	return nil
}

func parseOnDeploymentEventConfiguration(configuration any) (OnDeploymentEventConfiguration, error) {
	config := OnDeploymentEventConfiguration{}
	if err := mapstructure.Decode(configuration, &config); err != nil {
		return config, fmt.Errorf("failed to decode configuration: %w", err)
	}

	config.Namespace = strings.TrimSpace(config.Namespace)
	config.DeploymentName = strings.TrimSpace(config.DeploymentName)
	config.EventMode = normalizeEventMode(config.EventMode)

	if config.Namespace == "" {
		return config, fmt.Errorf("namespace is required")
	}

	if config.DeploymentName == "" {
		return config, fmt.Errorf("deploymentName is required")
	}

	switch config.EventMode {
	case EventModeAnyChange, EventModeRolloutCompleted, EventModeRolloutFailed:
	default:
		return config, fmt.Errorf("eventMode must be one of: %s, %s, %s", EventModeAnyChange, EventModeRolloutCompleted, EventModeRolloutFailed)
	}

	return config, nil
}

func authorizeWebhook(ctx core.WebhookRequestContext) error {
	secret, err := resolveWebhookAuthorizationSecret(ctx)
	if err != nil {
		return err
	}

	authorization := strings.TrimSpace(ctx.Headers.Get("Authorization"))
	if !strings.HasPrefix(authorization, "Bearer ") {
		return fmt.Errorf("missing bearer authorization")
	}

	token := strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer "))
	if subtle.ConstantTimeCompare([]byte(token), secret) != 1 {
		return fmt.Errorf("invalid bearer authorization")
	}

	return nil
}

func resolveWebhookAuthorizationSecret(ctx core.WebhookRequestContext) ([]byte, error) {
	if ctx.Integration != nil {
		secret, err := resolveIntegrationSharedSecret(ctx.Integration)
		if err == nil && strings.TrimSpace(secret) != "" {
			return []byte(secret), nil
		}
	}

	if ctx.Webhook == nil {
		return nil, fmt.Errorf("missing webhook context")
	}

	secret, err := ctx.Webhook.GetSecret()
	if err != nil {
		return nil, fmt.Errorf("failed to read webhook secret: %w", err)
	}

	if strings.TrimSpace(string(secret)) == "" {
		return nil, fmt.Errorf("missing webhook secret")
	}

	return secret, nil
}

func normalizeEventMode(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return EventModeAnyChange
	}

	return trimmed
}

func matchesEventMode(configured string, received string) bool {
	configured = normalizeEventMode(configured)
	received = normalizeEventMode(received)
	if configured == EventModeAnyChange {
		return true
	}

	return configured == received
}

func buildHelmInstallCommand(clusterName string, webhookURL string) string {
	return fmt.Sprintf(
		"helm upgrade --install superplane-kubernetes-exporter release/kubernetes-exporter-helm-chart/helm \\\n  --set watch.clusterName=%s \\\n  --set-string watch.namespace= \\\n  --set superplane.webhookUrl=%s \\\n  --set superplane.sharedSecret=<same-shared-secret-as-integration>",
		clusterName,
		webhookURL,
	)
}

func buildHelmValuesSnippet(clusterName string, webhookURL string) string {
	return fmt.Sprintf(
		"watch:\n  clusterName: %s\n  namespace: \"\" # leave empty for cluster-wide watch, or set one namespace to narrow scope\n\nsuperplane:\n  webhookUrl: %s\n  sharedSecret: <same-shared-secret-as-integration>\n",
		clusterName,
		webhookURL,
	)
}
