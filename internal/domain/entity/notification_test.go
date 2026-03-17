package entity

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validSMSNotification() *Notification {
	n := NewNotification(ChannelSMS, PriorityNormal, "+15551234567", "Hello via SMS", "idem-sms-1")
	return n
}

func validEmailNotification() *Notification {
	n := NewNotification(ChannelEmail, PriorityHigh, "user@example.com", "Hello via Email", "idem-email-1")
	subject := "Test Subject"
	n.Subject = &subject
	return n
}

func validPushNotification() *Notification {
	n := NewNotification(ChannelPush, PriorityLow, "device-token-abc123", "Hello via Push", "idem-push-1")
	return n
}

func TestNotification_Validate(t *testing.T) {
	tests := []struct {
		name      string
		modify    func(n *Notification)
		wantErrs  int
		errSubstr string
	}{
		{
			name:     "valid SMS notification",
			modify:   func(n *Notification) {},
			wantErrs: 0,
		},
		{
			name: "valid email notification",
			modify: func(n *Notification) {
				n.Channel = ChannelEmail
				n.Recipient = "user@example.com"
				subject := "Subject"
				n.Subject = &subject
			},
			wantErrs: 0,
		},
		{
			name: "valid push notification",
			modify: func(n *Notification) {
				n.Channel = ChannelPush
				n.Recipient = "device-token-123"
			},
			wantErrs: 0,
		},
		{
			name: "missing idempotency key",
			modify: func(n *Notification) {
				n.IdempotencyKey = ""
			},
			wantErrs:  1,
			errSubstr: "idempotency_key",
		},
		{
			name: "invalid channel",
			modify: func(n *Notification) {
				n.Channel = "fax"
			},
			wantErrs:  1,
			errSubstr: "channel",
		},
		{
			name: "missing recipient",
			modify: func(n *Notification) {
				n.Recipient = ""
			},
			wantErrs:  1,
			errSubstr: "recipient",
		},
		{
			name: "missing content without template",
			modify: func(n *Notification) {
				n.Content = ""
				n.TemplateID = nil
			},
			wantErrs:  1,
			errSubstr: "content",
		},
		{
			name: "content empty but template set is valid",
			modify: func(n *Notification) {
				n.Content = ""
				tid := uuid.New()
				n.TemplateID = &tid
			},
			wantErrs: 0,
		},
		{
			name: "invalid priority",
			modify: func(n *Notification) {
				n.Priority = "urgent"
			},
			wantErrs:  1,
			errSubstr: "priority",
		},
		{
			name: "invalid status",
			modify: func(n *Notification) {
				n.Status = "unknown"
			},
			wantErrs:  1,
			errSubstr: "status",
		},
		{
			name: "invalid email recipient",
			modify: func(n *Notification) {
				n.Channel = ChannelEmail
				n.Recipient = "not-an-email"
				subject := "Subject"
				n.Subject = &subject
			},
			wantErrs:  1,
			errSubstr: "valid email",
		},
		{
			name: "SMS phone number too short",
			modify: func(n *Notification) {
				n.Channel = ChannelSMS
				n.Recipient = "123"
			},
			wantErrs:  1,
			errSubstr: "too short",
		},
		{
			name: "email missing subject without template",
			modify: func(n *Notification) {
				n.Channel = ChannelEmail
				n.Recipient = "user@example.com"
				n.Subject = nil
				n.TemplateID = nil
			},
			wantErrs:  1,
			errSubstr: "subject",
		},
		{
			name: "max_attempts zero is invalid",
			modify: func(n *Notification) {
				n.MaxAttempts = 0
			},
			wantErrs:  1,
			errSubstr: "max_attempts",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := validSMSNotification()
			tt.modify(n)
			errs := n.Validate()
			if tt.wantErrs == 0 {
				assert.Empty(t, errs, "expected no validation errors")
			} else {
				require.NotEmpty(t, errs, "expected validation errors")
				assert.GreaterOrEqual(t, len(errs), tt.wantErrs)
				found := false
				for _, e := range errs {
					if assert.NotNil(t, e) && containsSubstring(e.Error(), tt.errSubstr) {
						found = true
					}
				}
				assert.True(t, found, "expected error containing %q, got %v", tt.errSubstr, errs)
			}
		})
	}
}

func TestNotification_IsRetryable(t *testing.T) {
	tests := []struct {
		name     string
		status   Status
		attempts int
		max      int
		want     bool
	}{
		{
			name:     "retryable when attempts less than max",
			status:   StatusFailed,
			attempts: 2,
			max:      5,
			want:     true,
		},
		{
			name:     "not retryable when attempts equal max",
			status:   StatusFailed,
			attempts: 5,
			max:      5,
			want:     false,
		},
		{
			name:     "not retryable when attempts exceed max",
			status:   StatusFailed,
			attempts: 6,
			max:      5,
			want:     false,
		},
		{
			name:     "not retryable when cancelled",
			status:   StatusCancelled,
			attempts: 0,
			max:      5,
			want:     false,
		},
		{
			name:     "not retryable when delivered",
			status:   StatusDelivered,
			attempts: 1,
			max:      5,
			want:     false,
		},
		{
			name:     "retryable when pending with attempts remaining",
			status:   StatusPending,
			attempts: 0,
			max:      5,
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := &Notification{
				Status:      tt.status,
				Attempts:    tt.attempts,
				MaxAttempts: tt.max,
			}
			assert.Equal(t, tt.want, n.IsRetryable())
		})
	}
}

func TestNewNotification(t *testing.T) {
	n := NewNotification(ChannelSMS, PriorityHigh, "+15551234567", "hello", "key-1")
	assert.NotEqual(t, uuid.Nil, n.ID)
	assert.Equal(t, ChannelSMS, n.Channel)
	assert.Equal(t, PriorityHigh, n.Priority)
	assert.Equal(t, "+15551234567", n.Recipient)
	assert.Equal(t, "hello", n.Content)
	assert.Equal(t, "key-1", n.IdempotencyKey)
	assert.Equal(t, StatusPending, n.Status)
	assert.Equal(t, 5, n.MaxAttempts)
}

func TestChannel_IsValid(t *testing.T) {
	tests := []struct {
		channel Channel
		valid   bool
	}{
		{ChannelSMS, true},
		{ChannelEmail, true},
		{ChannelPush, true},
		{"fax", false},
		{"", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.valid, tt.channel.IsValid(), "channel %q", tt.channel)
	}
}

func TestPriority_IsValid(t *testing.T) {
	tests := []struct {
		priority Priority
		valid    bool
	}{
		{PriorityHigh, true},
		{PriorityNormal, true},
		{PriorityLow, true},
		{"urgent", false},
		{"", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.valid, tt.priority.IsValid(), "priority %q", tt.priority)
	}
}

func TestStatus_IsValid(t *testing.T) {
	tests := []struct {
		status Status
		valid  bool
	}{
		{StatusPending, true},
		{StatusQueued, true},
		{StatusProcessing, true},
		{StatusDelivered, true},
		{StatusFailed, true},
		{StatusCancelled, true},
		{"unknown", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.valid, tt.status.IsValid(), "status %q", tt.status)
	}
}

func TestStatus_IsTerminal(t *testing.T) {
	tests := []struct {
		status   Status
		terminal bool
	}{
		{StatusDelivered, true},
		{StatusFailed, true},
		{StatusCancelled, true},
		{StatusPending, false},
		{StatusQueued, false},
		{StatusProcessing, false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.terminal, tt.status.IsTerminal(), "status %q", tt.status)
	}
}

func containsSubstring(s, substr string) bool {
	if substr == "" {
		return true
	}
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && contains(s, substr))
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
