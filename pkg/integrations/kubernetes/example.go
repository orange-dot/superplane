package kubernetes

func onDeploymentEventExampleData() map[string]any {
	return map[string]any{
		"data": map[string]any{
			"clusterName":         "production-eu-1",
			"namespace":           "payments",
			"deploymentName":      "checkout-api",
			"eventMode":           EventModeRolloutCompleted,
			"rolloutState":        "completed",
			"reason":              "NewReplicaSetAvailable",
			"message":             "Deployment has successfully progressed.",
			"observedGeneration":  7,
			"generation":          7,
			"resourceVersion":     "182993",
			"desiredReplicas":     3,
			"updatedReplicas":     3,
			"readyReplicas":       3,
			"availableReplicas":   3,
			"unavailableReplicas": 0,
			"timestamp":           "2026-04-19T18:00:00Z",
		},
		"timestamp": "2026-04-19T18:00:00Z",
		"type":      KubernetesDeploymentEventPayloadType,
	}
}

func restartRolloutExampleOutput() map[string]any {
	return map[string]any{
		"data": map[string]any{
			"clusterName":    "production-eu-1",
			"namespace":      "payments",
			"deploymentName": "checkout-api",
			"restartedAt":    "2026-04-19T18:05:00Z",
			"status":         "requested",
		},
		"timestamp": "2026-04-19T18:05:00Z",
		"type":      KubernetesDeploymentRestartPayloadType,
	}
}
