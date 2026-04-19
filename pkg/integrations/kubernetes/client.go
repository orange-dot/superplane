package kubernetes

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/superplanehq/superplane/pkg/core"
)

type Client struct {
	http         core.HTTPContext
	baseURL      string
	sharedSecret string
}

func NewClient(httpCtx core.HTTPContext, baseURL string, sharedSecret string) *Client {
	return &Client{
		http:         httpCtx,
		baseURL:      strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		sharedSecret: strings.TrimSpace(sharedSecret),
	}
}

func (c *Client) Healthz() error {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/healthz", nil)
	if err != nil {
		return fmt.Errorf("failed to build health request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call exporter: %w", err)
	}
	defer resp.Body.Close()

	if err := checkStatus(resp); err != nil {
		return err
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func (c *Client) RestartRollout(command RestartRolloutCommand) (*RestartRolloutPayload, error) {
	body, err := json.Marshal(command)
	if err != nil {
		return nil, fmt.Errorf("failed to encode restart command: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/commands/restart-rollout", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to build restart request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.sharedSecret != "" {
		req.Header.Set("Authorization", "Bearer "+c.sharedSecret)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call exporter: %w", err)
	}
	defer resp.Body.Close()

	if err := checkStatus(resp); err != nil {
		return nil, err
	}

	payload := RestartRolloutPayload{}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("failed to decode restart response: %w", err)
	}

	return &payload, nil
}

func checkStatus(resp *http.Response) error {
	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = http.StatusText(resp.StatusCode)
		}
		return fmt.Errorf("unexpected exporter response %d: %s", resp.StatusCode, message)
	}
	return nil
}
