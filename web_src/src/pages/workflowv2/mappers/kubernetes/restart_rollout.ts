import type {
  ComponentBaseContext,
  ComponentBaseMapper,
  ExecutionDetailsContext,
  ExecutionInfo,
  NodeInfo,
  OutputPayload,
  SubtitleContext,
} from "../types";
import type { ComponentBaseProps, EventSection } from "@/ui/componentBase";
import type React from "react";
import kubernetesIcon from "@/assets/icons/integrations/kubernetes.svg";
import { getState, getStateMap, getTriggerRenderer } from "..";
import { renderTimeAgo } from "@/components/TimeAgo";
import type { RestartRolloutConfiguration, RestartRolloutMetadata, RestartRolloutPayload } from "./types";

export const restartRolloutMapper: ComponentBaseMapper = {
  props(context: ComponentBaseContext): ComponentBaseProps {
    const lastExecution = context.lastExecutions.length > 0 ? context.lastExecutions[0] : null;
    const componentName = context.componentDefinition.name ?? "kubernetes.restartRollout";

    return {
      iconSrc: kubernetesIcon,
      iconSlug: context.componentDefinition.icon ?? "server",
      collapsedBackground: "bg-white",
      collapsed: context.node.isCollapsed,
      title:
        context.node.name || context.componentDefinition.label || context.componentDefinition.name || "Restart Rollout",
      eventSections: lastExecution ? buildEventSections(context.nodes, lastExecution, componentName) : undefined,
      includeEmptyState: !lastExecution,
      eventStateMap: getStateMap(componentName),
      metadata: buildMetadata(context.node),
    };
  },

  subtitle(context: SubtitleContext): string | React.ReactNode {
    const timestamp = context.execution.updatedAt || context.execution.createdAt;
    return timestamp ? renderTimeAgo(new Date(timestamp)) : "";
  },

  getExecutionDetails(context: ExecutionDetailsContext): Record<string, string> {
    const outputs = context.execution.outputs as { default?: OutputPayload[] } | undefined;
    const payload = outputs?.default?.[0]?.data as RestartRolloutPayload | undefined;
    const configuration = context.node.configuration as RestartRolloutConfiguration | undefined;
    const metadata = context.node.metadata as RestartRolloutMetadata | undefined;

    return {
      Cluster: metadata?.clusterName || "-",
      Namespace: payload?.namespace || metadata?.namespace || configuration?.namespace || "-",
      Deployment: payload?.deploymentName || metadata?.deploymentName || configuration?.deploymentName || "-",
      Status: payload?.status || "-",
      "Restarted At": payload?.restartedAt ? new Date(payload.restartedAt).toLocaleString() : "-",
    };
  },
};

function buildMetadata(node: NodeInfo) {
  const configuration = node.configuration as RestartRolloutConfiguration | undefined;
  const metadata = node.metadata as RestartRolloutMetadata | undefined;
  const items = [];

  if (metadata?.clusterName) {
    items.push({ icon: "server", label: metadata.clusterName });
  }

  const target = [
    metadata?.namespace || configuration?.namespace,
    metadata?.deploymentName || configuration?.deploymentName,
  ]
    .filter(Boolean)
    .join("/");
  if (target) {
    items.push({ icon: "box", label: target });
  }

  return items.slice(0, 2);
}

function buildEventSections(nodes: NodeInfo[], execution: ExecutionInfo, componentName: string): EventSection[] {
  if (!execution.rootEvent) return [];

  const rootTriggerNode = nodes.find((node) => node.id === execution.rootEvent?.nodeId);
  const rootTriggerRenderer = getTriggerRenderer(rootTriggerNode?.componentName || "");
  const { title } = rootTriggerRenderer.getTitleAndSubtitle({ event: execution.rootEvent });
  const subtitleTimestamp = execution.updatedAt || execution.createdAt;
  const eventSubtitle = subtitleTimestamp ? renderTimeAgo(new Date(subtitleTimestamp)) : "";

  return [
    {
      receivedAt: new Date(execution.createdAt),
      eventTitle: title,
      eventSubtitle,
      eventState: getState(componentName)(execution),
      eventId: execution.rootEvent.id,
    },
  ];
}
