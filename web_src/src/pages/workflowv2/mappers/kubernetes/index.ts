import type { ComponentBaseMapper, CustomFieldRenderer, EventStateRegistry, TriggerRenderer } from "../types";
import { buildActionStateRegistry } from "../utils";
import { onDeploymentEventCustomFieldRenderer, onDeploymentEventTriggerRenderer } from "./on_deployment_event";
import { restartRolloutMapper } from "./restart_rollout";

export const componentMappers: Record<string, ComponentBaseMapper> = {
  restartRollout: restartRolloutMapper,
};

export const triggerRenderers: Record<string, TriggerRenderer> = {
  onDeploymentEvent: onDeploymentEventTriggerRenderer,
};

export const customFieldRenderers: Record<string, CustomFieldRenderer> = {
  onDeploymentEvent: onDeploymentEventCustomFieldRenderer,
};

export const eventStateRegistry: Record<string, EventStateRegistry> = {
  restartRollout: buildActionStateRegistry("restarted"),
};
