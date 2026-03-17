-- 000002_create_templates.down.sql

ALTER TABLE notifications DROP CONSTRAINT IF EXISTS fk_notifications_template_id;
DROP TRIGGER IF EXISTS trg_templates_updated_at ON templates;
DROP TABLE IF EXISTS templates;
