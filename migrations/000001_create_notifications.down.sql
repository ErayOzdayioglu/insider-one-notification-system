-- 000001_create_notifications.down.sql

DROP TRIGGER IF EXISTS trg_notifications_updated_at ON notifications;
DROP FUNCTION IF EXISTS update_updated_at_column();
DROP TABLE IF EXISTS notifications;
