package httpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/erayozdayioglu/insider-one-notification-system/internal/config"
	"github.com/erayozdayioglu/insider-one-notification-system/internal/domain/entity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestNotification() *entity.Notification {
	return entity.NewNotification(entity.ChannelSMS, entity.PriorityNormal, "+15551234567", "Hello, World!", "idem-key-1")
}

func newTestConfig(serverURL string) config.WebhookConfig {
	return config.WebhookConfig{
		BaseURL:         serverURL,
		UUID:            "",
		Timeout:         5 * time.Second,
		MaxIdleConns:    10,
		IdleConnTimeout: 30 * time.Second,
	}
}

func TestWebhookClient_SuccessfulDelivery(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var body webhookRequestBody
		err := json.NewDecoder(r.Body).Decode(&body)
		require.NoError(t, err)
		assert.Equal(t, "+15551234567", body.To)
		assert.Equal(t, "sms", body.Channel)
		assert.Equal(t, "Hello, World!", body.Content)
		assert.Empty(t, body.Subject)

		w.WriteHeader(http.StatusAccepted)
		resp := webhookDeliveryResponse{
			MessageID: "msg-abc-123",
			Status:    "accepted",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := NewDeliveryClient(newTestConfig(ts.URL))
	notification := newTestNotification()

	resp, err := client.Send(context.Background(), notification)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "msg-abc-123", resp.MessageID)
	assert.Equal(t, "accepted", resp.Status)
	assert.False(t, resp.Timestamp.IsZero())
}

func TestWebhookClient_SuccessfulDeliveryNonJSONResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
	}))
	defer ts.Close()

	client := NewDeliveryClient(newTestConfig(ts.URL))
	notification := newTestNotification()

	resp, err := client.Send(context.Background(), notification)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Fallback values should be used.
	assert.Contains(t, resp.MessageID, "wh-")
	assert.Equal(t, "accepted", resp.Status)
	assert.False(t, resp.Timestamp.IsZero())
}

func TestWebhookClient_Non2xxResponseReturnsError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "internal server error")
	}))
	defer ts.Close()

	client := NewDeliveryClient(newTestConfig(ts.URL))
	notification := newTestNotification()

	resp, err := client.Send(context.Background(), notification)
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "non-2xx status 500")
	assert.Contains(t, err.Error(), "internal server error")
}

func TestWebhookClient_RequestWithSubject(t *testing.T) {
	var receivedBody webhookRequestBody
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(webhookDeliveryResponse{
			MessageID: "msg-with-subject",
			Status:    "accepted",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		})
	}))
	defer ts.Close()

	client := NewDeliveryClient(newTestConfig(ts.URL))
	notification := entity.NewNotification(entity.ChannelEmail, entity.PriorityHigh, "user@example.com", "Email body content", "idem-key-2")
	subject := "Important Subject"
	notification.Subject = &subject

	resp, err := client.Send(context.Background(), notification)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "msg-with-subject", resp.MessageID)
	assert.Equal(t, "Important Subject", receivedBody.Subject)
	assert.Equal(t, "user@example.com", receivedBody.To)
	assert.Equal(t, "email", receivedBody.Channel)
}

func TestWebhookClient_RequestTimeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sleep longer than the client timeout.
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	cfg := newTestConfig(ts.URL)
	cfg.Timeout = 50 * time.Millisecond

	client := NewDeliveryClient(cfg)
	notification := newTestNotification()

	resp, err := client.Send(context.Background(), notification)
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "executing webhook HTTP request")
}

func TestWebhookClient_InvalidURLReturnsError(t *testing.T) {
	cfg := config.WebhookConfig{
		BaseURL:         "://not-a-valid-url",
		UUID:            "",
		Timeout:         1 * time.Second,
		MaxIdleConns:    10,
		IdleConnTimeout: 30 * time.Second,
	}

	client := NewDeliveryClient(cfg)
	notification := newTestNotification()

	resp, err := client.Send(context.Background(), notification)
	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestWebhookClient_UUIDAppendsToBaseURL(t *testing.T) {
	var requestPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(webhookDeliveryResponse{
			MessageID: "msg-uuid",
			Status:    "accepted",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		})
	}))
	defer ts.Close()

	cfg := newTestConfig(ts.URL)
	cfg.UUID = "test-uuid-123"

	client := NewDeliveryClient(cfg)
	notification := newTestNotification()

	resp, err := client.Send(context.Background(), notification)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "/test-uuid-123", requestPath)
}
