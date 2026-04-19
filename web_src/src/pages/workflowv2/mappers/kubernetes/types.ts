export interface DeploymentEventPayload {
  clusterName?: string;
  namespace?: string;
  deploymentName?: string;
  eventMode?: string;
  rolloutState?: string;
  reason?: string;
  message?: string;
  observedGeneration?: number;
  generation?: number;
  resourceVersion?: string;
  desiredReplicas?: number;
  updatedReplicas?: number;
  readyReplicas?: number;
  availableReplicas?: number;
  unavailableReplicas?: number;
  timestamp?: string;
}

export interface OnDeploymentEventConfiguration {
  namespace?: string;
  deploymentName?: string;
  eventMode?: string;
}

export interface OnDeploymentEventMetadata {
  clusterName?: string;
  namespace?: string;
  deploymentName?: string;
  eventMode?: string;
  webhookUrl?: string;
  exporterUrl?: string;
  sharedSecretConfigured?: boolean;
  helmInstallCommand?: string;
  helmValuesSnippet?: string;
}

export interface RestartRolloutConfiguration {
  namespace?: string;
  deploymentName?: string;
}

export interface RestartRolloutMetadata {
  clusterName?: string;
  namespace?: string;
  deploymentName?: string;
  exporterUrl?: string;
}

export interface RestartRolloutPayload {
  clusterName?: string;
  namespace?: string;
  deploymentName?: string;
  restartedAt?: string;
  status?: string;
}
