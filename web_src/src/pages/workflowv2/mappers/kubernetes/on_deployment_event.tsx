import React from "react";
import type {
  CustomFieldRenderer,
  NodeInfo,
  TriggerEventContext,
  TriggerRenderer,
  TriggerRendererContext,
} from "../types";
import type { TriggerProps } from "@/ui/trigger";
import { getBackgroundColorClass, getColorClass } from "@/lib/colors";
import { renderTimeAgo } from "@/components/TimeAgo";
import kubernetesIcon from "@/assets/icons/integrations/kubernetes.svg";
import type { DeploymentEventPayload, OnDeploymentEventConfiguration, OnDeploymentEventMetadata } from "./types";
import type { MetadataItem } from "@/ui/metadataList";
import { formatTimestamp, stringOrDash } from "../utils";

const EVENT_MODE_LABELS: Record<string, string> = {
  any_change: "Any change",
  rollout_completed: "Rollout completed",
  rollout_failed: "Rollout failed",
};

export const onDeploymentEventTriggerRenderer: TriggerRenderer = {
  getTitleAndSubtitle: (context: TriggerEventContext): { title: string; subtitle: string | React.ReactNode } => {
    const eventData = context.event?.data as DeploymentEventPayload;
    const title = `${stringOrDash(eventData?.namespace)}/${stringOrDash(eventData?.deploymentName)}`;

    const parts = [
      eventData?.rolloutState,
      eventData?.reason,
      context.event?.createdAt ? renderTimeAgo(new Date(context.event.createdAt)) : "",
    ]
      .filter(Boolean)
      .map((value) => String(value));

    return {
      title,
      subtitle: parts.join(" · "),
    };
  },

  getRootEventValues: (context: TriggerEventContext): Record<string, string> => {
    const eventData = context.event?.data as DeploymentEventPayload;
    return {
      Cluster: stringOrDash(eventData?.clusterName),
      Namespace: stringOrDash(eventData?.namespace),
      Deployment: stringOrDash(eventData?.deploymentName),
      "Event Mode": stringOrDash(EVENT_MODE_LABELS[eventData?.eventMode || ""] || eventData?.eventMode),
      "Rollout State": stringOrDash(eventData?.rolloutState),
      Reason: stringOrDash(eventData?.reason),
      Message: stringOrDash(eventData?.message),
      "Desired Replicas": stringOrDash(eventData?.desiredReplicas),
      "Updated Replicas": stringOrDash(eventData?.updatedReplicas),
      "Available Replicas": stringOrDash(eventData?.availableReplicas),
      Timestamp: formatTimestamp(eventData?.timestamp),
    };
  },

  getTriggerProps: (context: TriggerRendererContext): TriggerProps => {
    const { node, definition, lastEvent } = context;
    const configuration = node.configuration as OnDeploymentEventConfiguration | undefined;
    const metadata = node.metadata as OnDeploymentEventMetadata | undefined;
    const metadataItems: MetadataItem[] = [];

    if (metadata?.clusterName) {
      metadataItems.push({ icon: "server", label: metadata.clusterName });
    }

    const target = [
      metadata?.namespace || configuration?.namespace,
      metadata?.deploymentName || configuration?.deploymentName,
    ]
      .filter(Boolean)
      .join("/");
    if (target) {
      metadataItems.push({ icon: "box", label: target });
    }

    const eventMode = metadata?.eventMode || configuration?.eventMode;
    if (eventMode) {
      metadataItems.push({ icon: "funnel", label: EVENT_MODE_LABELS[eventMode] || eventMode });
    }

    const props: TriggerProps = {
      title: node.name || definition.label || "Unnamed trigger",
      iconSrc: kubernetesIcon,
      iconColor: getColorClass(definition.color),
      collapsedBackground: getBackgroundColorClass(definition.color),
      metadata: metadataItems.slice(0, 3),
    };

    if (lastEvent) {
      const { title, subtitle } = onDeploymentEventTriggerRenderer.getTitleAndSubtitle({ event: lastEvent });
      props.lastEventData = {
        title,
        subtitle,
        receivedAt: new Date(lastEvent.createdAt),
        state: "triggered",
        eventId: lastEvent.id,
      };
    }

    return props;
  },
};

export const onDeploymentEventCustomFieldRenderer: CustomFieldRenderer = {
  render: (node: NodeInfo) => {
    const metadata = node.metadata as OnDeploymentEventMetadata | undefined;
    const webhookUrl = metadata?.webhookUrl || "[URL GENERATED ONCE THE CANVAS IS SAVED]";
    const exporterUrl =
      metadata?.exporterUrl || "[set Exporter Base URL on the integration if you want restart actions]";
    const helmInstallCommand = metadata?.helmInstallCommand || "Save the canvas to generate the install command.";
    const helmValuesSnippet = metadata?.helmValuesSnippet || "Save the canvas to generate the values snippet.";

    return (
      <div className="border-t-1 border-gray-200 pt-4">
        <div className="space-y-3">
          <div>
            <span className="text-sm font-medium text-gray-700 dark:text-gray-300">Kubernetes Exporter Setup</span>
            <div className="text-xs text-gray-800 dark:text-gray-100 mt-2 border-1 border-gray-300 dark:border-gray-600 px-2.5 py-2 bg-gray-50 dark:bg-gray-800 rounded-md">
              <ol className="list-decimal ml-4 space-y-1">
                <li>Save the canvas to generate the webhook URL.</li>
                <li>Install the in-cluster exporter with the Helm command below.</li>
                <li>Use the same Shared Secret value from the integration.</li>
                <li>
                  If you need restart actions, expose the exporter and paste that reachable URL into the integration.
                </li>
              </ol>
              <div className="mt-3">
                <span className="text-xs font-medium text-gray-700 dark:text-gray-200">Webhook URL</span>
                <pre className="mt-1 text-xs text-gray-800 dark:text-gray-100 border-1 border-gray-300 dark:border-gray-600 px-2.5 py-2 bg-white dark:bg-gray-900 rounded-md font-mono whitespace-pre-wrap break-all">
                  {webhookUrl}
                </pre>
              </div>
              <div className="mt-3">
                <span className="text-xs font-medium text-gray-700 dark:text-gray-200">Helm Command</span>
                <pre className="mt-1 text-xs text-gray-800 dark:text-gray-100 border-1 border-gray-300 dark:border-gray-600 px-2.5 py-2 bg-white dark:bg-gray-900 rounded-md font-mono whitespace-pre-wrap break-all">
                  {helmInstallCommand}
                </pre>
              </div>
              <div className="mt-3">
                <span className="text-xs font-medium text-gray-700 dark:text-gray-200">values.yaml Snippet</span>
                <pre className="mt-1 text-xs text-gray-800 dark:text-gray-100 border-1 border-gray-300 dark:border-gray-600 px-2.5 py-2 bg-white dark:bg-gray-900 rounded-md font-mono whitespace-pre-wrap break-all">
                  {helmValuesSnippet}
                </pre>
              </div>
              <div className="mt-3">
                <span className="text-xs font-medium text-gray-700 dark:text-gray-200">Exporter Base URL</span>
                <pre className="mt-1 text-xs text-gray-800 dark:text-gray-100 border-1 border-gray-300 dark:border-gray-600 px-2.5 py-2 bg-white dark:bg-gray-900 rounded-md font-mono whitespace-pre-wrap break-all">
                  {exporterUrl}
                </pre>
              </div>
            </div>
          </div>
        </div>
      </div>
    );
  },
};
