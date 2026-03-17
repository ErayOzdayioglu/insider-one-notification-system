-- 000002_create_templates.up.sql
-- Message templates with variable substitution support.

CREATE TABLE IF NOT EXISTS templates (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        VARCHAR(255) UNIQUE NOT NULL,
    channel     VARCHAR(10)  NOT NULL CHECK (channel IN ('sms', 'email', 'push')),
    subject     VARCHAR(500),
    content     TEXT         NOT NULL,
    variables   JSONB        DEFAULT '[]',
    is_active   BOOLEAN      DEFAULT true,
    created_at  TIMESTAMPTZ  DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  DEFAULT NOW()
);

-- Foreign key from notifications to templates
ALTER TABLE notifications
    ADD CONSTRAINT fk_notifications_template_id
    FOREIGN KEY (template_id) REFERENCES templates (id);

-- Auto-update updated_at (reuse function from migration 000001)
CREATE TRIGGER trg_templates_updated_at
    BEFORE UPDATE ON templates
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
