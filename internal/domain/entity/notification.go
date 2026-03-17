package entity

import (
	"fmt"
	"net/mail"
	"time"

	"github.com/google/uuid"
)

// Channel represents the delivery channel for a notification.
type Channel string

const (
	ChannelSMS   Channel = "sms"
	ChannelEmail Channel = "email"
	ChannelPush  Channel = "push"
)

// AllChannels returns all valid channel values.
func AllChannels() []Channel {
	return []Channel{ChannelSMS, ChannelEmail, ChannelPush}
}

// IsValid checks whether the channel is one of the allowed values.
func (c Channel) IsValid() bool {
	switch c {
	case ChannelSMS, ChannelEmail, ChannelPush:
		return true
	}
	return false
}

// Priority determines the processing order of notifications.
type Priority string

const (
	PriorityHigh   Priority = "high"
	PriorityNormal Priority = "normal"
	PriorityLow    Priority = "low"
)

// AllPriorities returns all valid priority values.
func AllPriorities() []Priority {
	return []Priority{PriorityHigh, PriorityNormal, PriorityLow}
}

// IsValid checks whether the priority is one of the allowed values.
func (p Priority) IsValid() bool {
	switch p {
	case PriorityHigh, PriorityNormal, PriorityLow:
		return true
	}
	return false
}

// Status represents the current lifecycle state of a notification.
type Status string

const (
	StatusPending    Status = "pending"
	StatusQueued     Status = "queued"
	StatusProcessing Status = "processing"
	StatusDelivered  Status = "delivered"
	StatusFailed     Status = "failed"
	StatusCancelled  Status = "cancelled"
)

// IsValid checks whether the status is one of the allowed values.
func (s Status) IsValid() bool {
	switch s {
	case StatusPending, StatusQueued, StatusProcessing, StatusDelivered, StatusFailed, StatusCancelled:
		return true
	}
	return false
}

// IsTerminal returns true if the status represents a final state that
// cannot transition further.
func (s Status) IsTerminal() bool {
	switch s {
	case StatusDelivered, StatusFailed, StatusCancelled:
		return true
	}
	return false
}

const defaultMaxAttempts = 5

// Notification is the core domain entity representing a message to be
// delivered through one of the supported channels.
type Notification struct {
	ID             uuid.UUID
	BatchID        *uuid.UUID
	IdempotencyKey string
	Channel        Channel
	Priority       Priority

	Recipient    string
	Subject      *string
	Content      string
	TemplateID   *uuid.UUID
	TemplateVars map[string]string

	Status      Status
	ScheduledAt *time.Time

	Attempts     int
	MaxAttempts  int
	LastAttemptAt *time.Time
	NextRetryAt  *time.Time

	ProviderMessageID *string
	ErrorMessage      *string

	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewNotification creates a Notification with sensible defaults and a
// generated UUID. Callers should set channel-specific fields afterwards
// and call Validate before persisting.
func NewNotification(channel Channel, priority Priority, recipient, content, idempotencyKey string) *Notification {
	now := time.Now().UTC()
	return &Notification{
		ID:             uuid.New(),
		IdempotencyKey: idempotencyKey,
		Channel:        channel,
		Priority:       priority,
		Recipient:      recipient,
		Content:        content,
		Status:         StatusPending,
		MaxAttempts:    defaultMaxAttempts,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

// Validate checks that the notification's fields satisfy all business rules.
// It returns a list of validation errors (empty when valid).
func (n *Notification) Validate() []error {
	var errs []error

	if n.ID == uuid.Nil {
		errs = append(errs, fmt.Errorf("id must not be nil"))
	}
	if n.IdempotencyKey == "" {
		errs = append(errs, fmt.Errorf("idempotency_key must not be empty"))
	}
	if !n.Channel.IsValid() {
		errs = append(errs, fmt.Errorf("channel %q is not valid", n.Channel))
	}
	if !n.Priority.IsValid() {
		errs = append(errs, fmt.Errorf("priority %q is not valid", n.Priority))
	}
	if n.Recipient == "" {
		errs = append(errs, fmt.Errorf("recipient must not be empty"))
	}
	if n.Content == "" && n.TemplateID == nil {
		errs = append(errs, fmt.Errorf("content must not be empty when no template is specified"))
	}
	if !n.Status.IsValid() {
		errs = append(errs, fmt.Errorf("status %q is not valid", n.Status))
	}
	if n.MaxAttempts <= 0 {
		errs = append(errs, fmt.Errorf("max_attempts must be greater than zero"))
	}

	// Channel-specific recipient validation.
	if n.Channel.IsValid() {
		errs = append(errs, n.validateRecipient()...)
	}

	// Email channel requires a subject.
	if n.Channel == ChannelEmail && (n.Subject == nil || *n.Subject == "") && n.TemplateID == nil {
		errs = append(errs, fmt.Errorf("subject is required for email notifications without a template"))
	}

	if n.ScheduledAt != nil && n.ScheduledAt.Before(n.CreatedAt) {
		errs = append(errs, fmt.Errorf("scheduled_at must not be in the past"))
	}

	return errs
}

// validateRecipient performs channel-specific checks on the Recipient field.
func (n *Notification) validateRecipient() []error {
	var errs []error
	switch n.Channel {
	case ChannelEmail:
		if _, err := mail.ParseAddress(n.Recipient); err != nil {
			errs = append(errs, fmt.Errorf("recipient is not a valid email address: %w", err))
		}
	case ChannelSMS:
		if len(n.Recipient) < 7 {
			errs = append(errs, fmt.Errorf("recipient phone number is too short"))
		}
	case ChannelPush:
		if len(n.Recipient) < 1 {
			errs = append(errs, fmt.Errorf("recipient device token must not be empty"))
		}
	}
	return errs
}

// IsRetryable returns true when the notification has failed but has not
// yet exhausted its retry budget and is not in a terminal cancelled state.
func (n *Notification) IsRetryable() bool {
	if n.Status == StatusCancelled || n.Status == StatusDelivered {
		return false
	}
	return n.Attempts < n.MaxAttempts
}
