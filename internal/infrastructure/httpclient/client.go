package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/erayozdayioglu/insider-one-notification-system/internal/config"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/entity"
)

// DeliveryResponse represents the response from the webhook delivery endpoint.
type DeliveryResponse struct {
	MessageID string
	Status    string
	Timestamp time.Time
}

// DeliveryClient defines the contract for sending notifications to the
// external webhook provider.
type DeliveryClient interface {
	Send(ctx context.Context, notification *entity.Notification) (*DeliveryResponse, error)
}

// webhookDeliveryResponse is the JSON shape returned by the webhook endpoint.
type webhookDeliveryResponse struct {
	MessageID string `json:"messageId"`
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
}

// webhookRequestBody is the JSON payload sent to the webhook endpoint.
type webhookRequestBody struct {
	To      string `json:"to"`
	Channel string `json:"channel"`
	Content string `json:"content"`
	Subject string `json:"subject,omitempty"`
}

// webhookClient is the concrete implementation of DeliveryClient that
// delivers notifications via HTTP POST to a webhook.site endpoint.
type webhookClient struct {
	httpClient *http.Client
	baseURL    string
	timeout    time.Duration
}

// NewDeliveryClient creates a DeliveryClient backed by an HTTP client with
// connection pooling configured from the application's WebhookConfig.
func NewDeliveryClient(cfg config.WebhookConfig) DeliveryClient {
	transport := &http.Transport{
		MaxIdleConns:        cfg.MaxIdleConns,
		MaxIdleConnsPerHost: cfg.MaxIdleConns,
		IdleConnTimeout:     cfg.IdleConnTimeout,
	}

	baseURL := cfg.BaseURL
	if cfg.UUID != "" {
		baseURL = fmt.Sprintf("%s/%s", cfg.BaseURL, cfg.UUID)
	}

	return &webhookClient{
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   cfg.Timeout,
		},
		baseURL: baseURL,
		timeout: cfg.Timeout,
	}
}

// Send delivers a notification to the webhook endpoint and returns the
// provider's response. It returns an error for non-2xx status codes,
// network failures, and malformed responses.
func (c *webhookClient) Send(ctx context.Context, notification *entity.Notification) (*DeliveryResponse, error) {
	body := webhookRequestBody{
		To:      notification.Recipient,
		Channel: string(notification.Channel),
		Content: notification.Content,
	}
	if notification.Subject != nil && *notification.Subject != "" {
		body.Subject = *notification.Subject
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshalling webhook request body: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	url := c.baseURL
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("creating webhook HTTP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing webhook HTTP request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading webhook response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("webhook returned non-2xx status %d: %s", resp.StatusCode, string(respBody))
	}

	var webhookResp webhookDeliveryResponse
	if err := json.Unmarshal(respBody, &webhookResp); err != nil {
		return nil, fmt.Errorf("unmarshalling webhook response: %w", err)
	}

	ts, err := time.Parse(time.RFC3339, webhookResp.Timestamp)
	if err != nil {
		// Fall back to current time if the timestamp format is unexpected.
		ts = time.Now().UTC()
	}

	return &DeliveryResponse{
		MessageID: webhookResp.MessageID,
		Status:    webhookResp.Status,
		Timestamp: ts,
	}, nil
}
