-- 000001_create_notifications.up.sql
-- Core notifications table for the event-driven notification system.

CREATE TABLE IF NOT EXISTS notifications (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    batch_id            UUID,
    idempotency_key     VARCHAR(255) UNIQUE NOT NULL,
    channel             VARCHAR(10)  NOT NULL CHECK (channel IN ('sms', 'email', 'push')),
    priority            VARCHAR(10)  NOT NULL CHECK (priority IN ('high', 'normal', 'low')),
    recipient           VARCHAR(255) NOT NULL,
    subject             VARCHAR(500),
    content             TEXT         NOT NULL,
    template_id         UUID,
    template_vars       JSONB        DEFAULT '{}',
    status              VARCHAR(20)  NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending', 'queued', 'processing', 'sent', 'delivered', 'failed', 'cancelled')),
    scheduled_at        TIMESTAMPTZ,
    attempts            INT          DEFAULT 0,
    max_attempts        INT          DEFAULT 5,
    last_attempt_at     TIMESTAMPTZ,
    next_retry_at       TIMESTAMPTZ,
    provider_message_id VARCHAR(255),
    error_message       TEXT,
    created_at          TIMESTAMPTZ  DEFAULT NOW(),
    updated_at          TIMESTAMPTZ  DEFAULT NOW()
);

-- Query indexes
CREATE INDEX idx_notifications_status          ON notifications (status);
CREATE INDEX idx_notifications_channel_status  ON notifications (channel, status);
CREATE INDEX idx_notifications_batch_id        ON notifications (batch_id);

-- Partial indexes for scheduler and retry workers
CREATE INDEX idx_notifications_scheduled
    ON notifications (scheduled_at)
    WHERE status = 'pending' AND scheduled_at IS NOT NULL;

CREATE INDEX idx_notifications_retry
    ON notifications (next_retry_at)
    WHERE status = 'failed' AND next_retry_at IS NOT NULL;

-- Idempotency key uniqueness is enforced by the UNIQUE constraint on the column;
-- this explicit index covers lookups.
CREATE UNIQUE INDEX idx_notifications_idempotency_key ON notifications (idempotency_key);

-- Auto-update updated_at on every row modification
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_notifications_updated_at
    BEFORE UPDATE ON notifications
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
