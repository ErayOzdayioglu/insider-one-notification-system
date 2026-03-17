package handler

import "time"

// ---------------------------------------------------------------------------
// Notification DTOs
// ---------------------------------------------------------------------------

// CreateNotificationRequest is the request body for creating a single notification.
type CreateNotificationRequest struct {
	IdempotencyKey string            `json:"idempotency_key" binding:"required" example:"notif-abc-123"`
	Channel        string            `json:"channel" binding:"required,oneof=sms email push" example:"email"`
	Priority       string            `json:"priority" binding:"required,oneof=high normal low" example:"normal"`
	Recipient      string            `json:"recipient" binding:"required" example:"user@example.com"`
	Subject        *string           `json:"subject,omitempty" example:"Welcome!"`
	Content        string            `json:"content,omitempty" example:"Hello {{.Name}}, welcome aboard!"`
	TemplateID     *string           `json:"template_id,omitempty" example:"550e8400-e29b-41d4-a716-446655440000"`
	TemplateVars   map[string]string `json:"template_vars,omitempty"`
	ScheduledAt    *time.Time        `json:"scheduled_at,omitempty" example:"2026-03-18T10:00:00Z"`
}

// CreateNotificationResponse is returned after successfully creating a notification.
type CreateNotificationResponse struct {
	ID        string    `json:"id" example:"550e8400-e29b-41d4-a716-446655440000"`
	Status    string    `json:"status" example:"pending"`
	CreatedAt time.Time `json:"created_at" example:"2026-03-17T12:00:00Z"`
}

// BatchCreateRequest is the request body for creating a batch of notifications.
type BatchCreateRequest struct {
	Notifications []CreateNotificationRequest `json:"notifications" binding:"required,min=1,max=1000,dive"`
}

// BatchNotificationSummary is a summary of a single notification in a batch response.
type BatchNotificationSummary struct {
	ID     string `json:"id" example:"550e8400-e29b-41d4-a716-446655440000"`
	Status string `json:"status" example:"pending"`
}

// BatchCreateResponse is returned after successfully creating a batch of notifications.
type BatchCreateResponse struct {
	BatchID       string                     `json:"batch_id" example:"660e8400-e29b-41d4-a716-446655440000"`
	Notifications []BatchNotificationSummary `json:"notifications"`
	Total         int                        `json:"total" example:"5"`
}

// NotificationResponse is the full representation of a notification.
type NotificationResponse struct {
	ID                string            `json:"id"`
	BatchID           *string           `json:"batch_id,omitempty"`
	IdempotencyKey    string            `json:"idempotency_key"`
	Channel           string            `json:"channel"`
	Priority          string            `json:"priority"`
	Recipient         string            `json:"recipient"`
	Subject           *string           `json:"subject,omitempty"`
	Content           string            `json:"content"`
	TemplateID        *string           `json:"template_id,omitempty"`
	TemplateVars      map[string]string `json:"template_vars,omitempty"`
	Status            string            `json:"status"`
	ScheduledAt       *time.Time        `json:"scheduled_at,omitempty"`
	Attempts          int               `json:"attempts"`
	MaxAttempts       int               `json:"max_attempts"`
	LastAttemptAt     *time.Time        `json:"last_attempt_at,omitempty"`
	NextRetryAt       *time.Time        `json:"next_retry_at,omitempty"`
	ProviderMessageID *string           `json:"provider_message_id,omitempty"`
	ErrorMessage      *string           `json:"error_message,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

// CancelNotificationResponse is returned after cancelling a notification.
type CancelNotificationResponse struct {
	ID     string `json:"id" example:"550e8400-e29b-41d4-a716-446655440000"`
	Status string `json:"status" example:"cancelled"`
}

// PaginatedNotificationsResponse wraps a paginated list of notifications.
type PaginatedNotificationsResponse struct {
	Notifications []NotificationResponse `json:"notifications"`
	Total         int64                  `json:"total" example:"42"`
	Offset        int                    `json:"offset" example:"0"`
	Limit         int                    `json:"limit" example:"20"`
}

// ---------------------------------------------------------------------------
// Template DTOs
// ---------------------------------------------------------------------------

// CreateTemplateRequest is the request body for creating a template.
type CreateTemplateRequest struct {
	Name    string  `json:"name" binding:"required" example:"welcome_email"`
	Channel string  `json:"channel" binding:"required,oneof=sms email push" example:"email"`
	Subject *string `json:"subject,omitempty" example:"Welcome, {{.Name}}!"`
	Content string  `json:"content" binding:"required" example:"Hello {{.Name}}, welcome to {{.Company}}!"`
}

// UpdateTemplateRequest is the request body for updating a template.
type UpdateTemplateRequest struct {
	Name     *string `json:"name,omitempty" example:"welcome_email_v2"`
	Channel  *string `json:"channel,omitempty" example:"email"`
	Subject  *string `json:"subject,omitempty" example:"Welcome, {{.Name}}!"`
	Content  *string `json:"content,omitempty" example:"Hello {{.Name}}, welcome to {{.Company}}!"`
	IsActive *bool   `json:"is_active,omitempty" example:"true"`
}

// TemplateResponse is the full representation of a template.
type TemplateResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Channel   string    `json:"channel"`
	Subject   *string   `json:"subject,omitempty"`
	Content   string    `json:"content"`
	Variables []string  `json:"variables"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PaginatedTemplatesResponse wraps a paginated list of templates.
type PaginatedTemplatesResponse struct {
	Templates []TemplateResponse `json:"templates"`
	Total     int64              `json:"total" example:"10"`
	Offset    int                `json:"offset" example:"0"`
	Limit     int                `json:"limit" example:"20"`
}

// RenderTemplateRequest is the request body for rendering a template.
type RenderTemplateRequest struct {
	Variables map[string]string `json:"variables" binding:"required"`
}

// RenderTemplateResponse is returned after rendering a template.
type RenderTemplateResponse struct {
	Subject string `json:"subject,omitempty" example:"Welcome, John!"`
	Content string `json:"content" example:"Hello John, welcome to Insider!"`
}

// ---------------------------------------------------------------------------
// Health DTOs
// ---------------------------------------------------------------------------

// HealthResponse is the response for health checks.
type HealthResponse struct {
	Status string `json:"status" example:"ok"`
}

// ReadyResponse is the response for readiness checks.
type ReadyResponse struct {
	Status   string            `json:"status" example:"ok"`
	Services map[string]string `json:"services"`
}

// ---------------------------------------------------------------------------
// Error DTOs
// ---------------------------------------------------------------------------

// ErrorResponse represents a standard error response.
type ErrorResponse struct {
	Error   string `json:"error" example:"entity not found"`
	Details any    `json:"details,omitempty"`
}

// ValidationErrorDetail represents a single field validation error.
type ValidationErrorDetail struct {
	Field   string `json:"field" example:"channel"`
	Message string `json:"message" example:"channel is required"`
}
