package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	kubernetesintegration "github.com/superplanehq/superplane/pkg/integrations/kubernetes"
)

const (
	defaultListenAddr   = ":8080"
	defaultPollInterval = 15 * time.Second
	defaultKubeAPIURL   = "https://kubernetes.default.svc"
	defaultTokenPath    = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	defaultCAPath       = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
)

type config struct {
	ClusterName            string
	SuperplaneWebhookURL   string
	SuperplaneSharedSecret string
	WatchNamespace         string
	ListenAddr             string
	PollInterval           time.Duration
	KubeAPIURL             string
	ServiceAccountToken    string
	ServiceAccountCAPath   string
}

type app struct {
	cfg              config
	kubeClient       *http.Client
	superplaneClient *http.Client
	logger           *log.Logger
}

type deploymentResource struct {
	Metadata deploymentMetadata `json:"metadata"`
	Spec     deploymentSpec     `json:"spec"`
	Status   deploymentStatus   `json:"status"`
}

type deploymentMetadata struct {
	Name            string `json:"name"`
	Namespace       string `json:"namespace"`
	ResourceVersion string `json:"resourceVersion"`
	Generation      int64  `json:"generation"`
}

type deploymentSpec struct {
	Replicas *int32 `json:"replicas"`
}

type deploymentStatus struct {
	ObservedGeneration  int64                 `json:"observedGeneration"`
	Replicas            int32                 `json:"replicas"`
	UpdatedReplicas     int32                 `json:"updatedReplicas"`
	ReadyReplicas       int32                 `json:"readyReplicas"`
	AvailableReplicas   int32                 `json:"availableReplicas"`
	UnavailableReplicas int32                 `json:"unavailableReplicas"`
	Conditions          []deploymentCondition `json:"conditions"`
}

type deploymentCondition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

type deploymentListResource struct {
	Items []deploymentResource `json:"items"`
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	kubeClient, err := buildKubeHTTPClient(cfg.ServiceAccountCAPath)
	if err != nil {
		log.Fatalf("failed to build kube client: %v", err)
	}

	application := &app{
		cfg:              cfg,
		kubeClient:       kubeClient,
		superplaneClient: buildOutboundHTTPClient(),
		logger:           log.New(os.Stdout, "kubernetes-exporter: ", log.LstdFlags|log.Lmsgprefix),
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	server := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: application.routes(),
	}

	go application.runPollLoop(ctx)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	scopeDescription := "all deployments in cluster"
	if cfg.WatchNamespace != "" {
		scopeDescription = fmt.Sprintf("deployments in namespace %s", cfg.WatchNamespace)
	}
	application.logger.Printf("listening on %s for %s", cfg.ListenAddr, scopeDescription)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server failed: %v", err)
	}
}

func loadConfig() (config, error) {
	cfg := config{
		ClusterName:            strings.TrimSpace(os.Getenv("WATCH_CLUSTER_NAME")),
		SuperplaneWebhookURL:   strings.TrimSpace(os.Getenv("SUPERPLANE_WEBHOOK_URL")),
		SuperplaneSharedSecret: strings.TrimSpace(os.Getenv("SUPERPLANE_SHARED_SECRET")),
		WatchNamespace:         strings.TrimSpace(os.Getenv("WATCH_NAMESPACE")),
		ListenAddr:             strings.TrimSpace(os.Getenv("LISTEN_ADDR")),
		KubeAPIURL:             strings.TrimSpace(os.Getenv("KUBE_API_URL")),
		ServiceAccountCAPath:   strings.TrimSpace(os.Getenv("KUBE_CA_PATH")),
	}

	if cfg.ClusterName == "" {
		cfg.ClusterName = "kubernetes-cluster"
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = defaultListenAddr
	}
	if cfg.KubeAPIURL == "" {
		cfg.KubeAPIURL = defaultKubeAPIURL
	}
	if cfg.ServiceAccountCAPath == "" {
		cfg.ServiceAccountCAPath = defaultCAPath
	}

	pollInterval := strings.TrimSpace(os.Getenv("POLL_INTERVAL"))
	if pollInterval == "" {
		cfg.PollInterval = defaultPollInterval
	} else {
		duration, err := time.ParseDuration(pollInterval)
		if err != nil {
			return cfg, fmt.Errorf("invalid POLL_INTERVAL: %w", err)
		}
		cfg.PollInterval = duration
	}

	tokenPath := strings.TrimSpace(os.Getenv("KUBE_TOKEN_PATH"))
	if tokenPath == "" {
		tokenPath = defaultTokenPath
	}

	tokenBytes, err := os.ReadFile(filepath.Clean(tokenPath))
	if err != nil {
		return cfg, fmt.Errorf("failed to read service account token: %w", err)
	}
	cfg.ServiceAccountToken = strings.TrimSpace(string(tokenBytes))

	switch {
	case cfg.SuperplaneWebhookURL == "":
		return cfg, fmt.Errorf("SUPERPLANE_WEBHOOK_URL is required")
	case cfg.SuperplaneSharedSecret == "":
		return cfg, fmt.Errorf("SUPERPLANE_SHARED_SECRET is required")
	case cfg.ServiceAccountToken == "":
		return cfg, fmt.Errorf("service account token is empty")
	default:
		return cfg, nil
	}
}

func buildKubeHTTPClient(caPath string) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()

	if strings.TrimSpace(caPath) == "" {
		return &http.Client{Transport: transport, Timeout: 20 * time.Second}, nil
	}

	caBytes, err := os.ReadFile(filepath.Clean(caPath))
	if err != nil {
		return nil, fmt.Errorf("failed to read CA certificate: %w", err)
	}

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caBytes) {
		return nil, fmt.Errorf("failed to parse CA certificate")
	}

	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	return &http.Client{Transport: transport, Timeout: 20 * time.Second}, nil
}

func buildOutboundHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	return &http.Client{Transport: transport, Timeout: 20 * time.Second}
}

func (a *app) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", a.handleHealthz)
	mux.HandleFunc("/commands/restart-rollout", a.handleRestartRollout)
	return mux
}

func (a *app) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"clusterName":    a.cfg.ClusterName,
		"watchNamespace": a.cfg.WatchNamespace,
		"watchScope":     watchScope(a.cfg.WatchNamespace),
	})
}

func (a *app) handleRestartRollout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	if !authorizeBearer(r, a.cfg.SuperplaneSharedSecret) {
		http.Error(w, "invalid bearer authorization", http.StatusUnauthorized)
		return
	}

	command := kubernetesintegration.RestartRolloutCommand{}
	if err := json.NewDecoder(r.Body).Decode(&command); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, "failed to decode command", http.StatusBadRequest)
		return
	}

	namespace, deployment, err := a.resolveTarget(command)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	payload, err := a.restartRollout(r.Context(), namespace, deployment, time.Now().UTC())
	if err != nil {
		a.logger.Printf("restart rollout failed: %v", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	writeJSON(w, http.StatusOK, payload)
}

func (a *app) resolveTarget(command kubernetesintegration.RestartRolloutCommand) (string, string, error) {
	namespace := strings.TrimSpace(command.Namespace)
	deployment := strings.TrimSpace(command.DeploymentName)
	if namespace == "" {
		return "", "", fmt.Errorf("namespace is required")
	}
	if deployment == "" {
		return "", "", fmt.Errorf("deploymentName is required")
	}

	if a.cfg.WatchNamespace != "" && namespace != a.cfg.WatchNamespace {
		return "", "", fmt.Errorf("exporter scope does not include namespace %s", namespace)
	}

	return namespace, deployment, nil
}

func (a *app) runPollLoop(ctx context.Context) {
	ticker := time.NewTicker(a.cfg.PollInterval)
	defer ticker.Stop()

	var lastResourceVersions map[string]string
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			deployments, err := a.fetchDeployments(ctx)
			if err != nil {
				a.logger.Printf("fetch deployments failed: %v", err)
				continue
			}

			changedDeployments, nextResourceVersions := diffDeploymentResourceVersions(lastResourceVersions, deployments)
			lastResourceVersions = nextResourceVersions

			for _, deployment := range changedDeployments {
				payload := normalizeDeploymentEvent(a.cfg.ClusterName, deployment, time.Now().UTC())
				if err := a.emitDeploymentEvent(ctx, payload); err != nil {
					a.logger.Printf("emit deployment event failed for %s/%s: %v", deployment.Metadata.Namespace, deployment.Metadata.Name, err)
				}
			}
		}
	}
}

func (a *app) emitDeploymentEvent(ctx context.Context, payload kubernetesintegration.DeploymentEventPayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to encode deployment event: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.SuperplaneWebhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to build webhook request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.cfg.SuperplaneSharedSecret)

	resp, err := a.superplaneClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call superplane webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("superplane webhook returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func (a *app) fetchDeployments(ctx context.Context) ([]deploymentResource, error) {
	var result deploymentListResource

	endpoint := fmt.Sprintf("%s/apis/apps/v1/deployments", strings.TrimRight(a.cfg.KubeAPIURL, "/"))
	if a.cfg.WatchNamespace != "" {
		endpoint = fmt.Sprintf(
			"%s/apis/apps/v1/namespaces/%s/deployments",
			strings.TrimRight(a.cfg.KubeAPIURL, "/"),
			url.PathEscape(a.cfg.WatchNamespace),
		)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build kube request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.ServiceAccountToken)

	resp, err := a.kubeClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call kube API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("kube API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode deployment payload: %w", err)
	}

	return result.Items, nil
}

func (a *app) restartRollout(ctx context.Context, namespace string, deployment string, restartedAt time.Time) (*kubernetesintegration.RestartRolloutPayload, error) {
	patchBody, err := json.Marshal(map[string]any{
		"spec": map[string]any{
			"template": map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]string{
						"kubectl.kubernetes.io/restartedAt": restartedAt.Format(time.RFC3339),
					},
				},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to encode rollout patch: %w", err)
	}

	endpoint := fmt.Sprintf(
		"%s/apis/apps/v1/namespaces/%s/deployments/%s",
		strings.TrimRight(a.cfg.KubeAPIURL, "/"),
		url.PathEscape(namespace),
		url.PathEscape(deployment),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, endpoint, bytes.NewReader(patchBody))
	if err != nil {
		return nil, fmt.Errorf("failed to build patch request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.ServiceAccountToken)
	req.Header.Set("Content-Type", "application/merge-patch+json")

	resp, err := a.kubeClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call kube API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("kube API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	return &kubernetesintegration.RestartRolloutPayload{
		ClusterName:    a.cfg.ClusterName,
		Namespace:      namespace,
		DeploymentName: deployment,
		RestartedAt:    restartedAt.Format(time.RFC3339),
		Status:         "requested",
	}, nil
}

func diffDeploymentResourceVersions(previous map[string]string, deployments []deploymentResource) ([]deploymentResource, map[string]string) {
	next := make(map[string]string, len(deployments))
	if previous == nil {
		for _, deployment := range deployments {
			resourceVersion := strings.TrimSpace(deployment.Metadata.ResourceVersion)
			if resourceVersion == "" {
				continue
			}
			next[deploymentKey(deployment)] = resourceVersion
		}
		return nil, next
	}

	changed := make([]deploymentResource, 0)
	for _, deployment := range deployments {
		resourceVersion := strings.TrimSpace(deployment.Metadata.ResourceVersion)
		if resourceVersion == "" {
			continue
		}

		key := deploymentKey(deployment)
		next[key] = resourceVersion
		if previous[key] != resourceVersion {
			changed = append(changed, deployment)
		}
	}

	return changed, next
}

func deploymentKey(deployment deploymentResource) string {
	return strings.TrimSpace(deployment.Metadata.Namespace) + "/" + strings.TrimSpace(deployment.Metadata.Name)
}

func normalizeDeploymentEvent(clusterName string, deployment deploymentResource, now time.Time) kubernetesintegration.DeploymentEventPayload {
	desiredReplicas := int32(1)
	if deployment.Spec.Replicas != nil {
		desiredReplicas = *deployment.Spec.Replicas
	}

	eventMode, rolloutState, reason, message := classifyDeployment(deployment, desiredReplicas)

	return kubernetesintegration.DeploymentEventPayload{
		ClusterName:         clusterName,
		Namespace:           deployment.Metadata.Namespace,
		DeploymentName:      deployment.Metadata.Name,
		EventMode:           eventMode,
		RolloutState:        rolloutState,
		Reason:              reason,
		Message:             message,
		ObservedGeneration:  deployment.Status.ObservedGeneration,
		Generation:          deployment.Metadata.Generation,
		ResourceVersion:     deployment.Metadata.ResourceVersion,
		DesiredReplicas:     desiredReplicas,
		UpdatedReplicas:     deployment.Status.UpdatedReplicas,
		ReadyReplicas:       deployment.Status.ReadyReplicas,
		AvailableReplicas:   deployment.Status.AvailableReplicas,
		UnavailableReplicas: deployment.Status.UnavailableReplicas,
		Timestamp:           now.Format(time.RFC3339),
	}
}

func classifyDeployment(deployment deploymentResource, desiredReplicas int32) (string, string, string, string) {
	progressing := findCondition(deployment.Status.Conditions, "Progressing")
	if progressing != nil && strings.EqualFold(progressing.Reason, "ProgressDeadlineExceeded") {
		return kubernetesintegration.EventModeRolloutFailed, "failed", progressing.Reason, progressing.Message
	}

	if deployment.Status.ObservedGeneration >= deployment.Metadata.Generation &&
		deployment.Status.UpdatedReplicas >= desiredReplicas &&
		deployment.Status.ReadyReplicas >= desiredReplicas &&
		deployment.Status.AvailableReplicas >= desiredReplicas &&
		deployment.Status.UnavailableReplicas == 0 {
		reason, message := conditionSummary(progressing, findCondition(deployment.Status.Conditions, "Available"))
		return kubernetesintegration.EventModeRolloutCompleted, "completed", reason, message
	}

	reason, message := conditionSummary(progressing, findCondition(deployment.Status.Conditions, "Available"))
	return kubernetesintegration.EventModeAnyChange, "progressing", reason, message
}

func conditionSummary(conditions ...*deploymentCondition) (string, string) {
	for _, condition := range conditions {
		if condition == nil {
			continue
		}
		reason := strings.TrimSpace(condition.Reason)
		message := strings.TrimSpace(condition.Message)
		if reason != "" || message != "" {
			return reason, message
		}
	}

	return "", ""
}

func findCondition(conditions []deploymentCondition, conditionType string) *deploymentCondition {
	for i := range conditions {
		if strings.EqualFold(conditions[i].Type, conditionType) {
			return &conditions[i]
		}
	}

	return nil
}

func authorizeBearer(r *http.Request, secret string) bool {
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(authorization, "Bearer ") {
		return false
	}

	token := strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer "))
	expected := strings.TrimSpace(secret)
	return token != "" && expected != "" && subtle.ConstantTimeCompare([]byte(token), []byte(expected)) == 1
}

func watchScope(namespace string) string {
	if strings.TrimSpace(namespace) == "" {
		return "cluster"
	}

	return "namespace"
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
