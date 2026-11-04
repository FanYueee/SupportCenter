-- Every department hears about its new tickets, not only technical support.
-- 'technical' stays valid so an image from before this change can still queue
-- its notices after a rollback.
ALTER TABLE email_notifications DROP CONSTRAINT email_notifications_kind_check;
UPDATE email_notifications SET kind='department' WHERE kind='technical';
ALTER TABLE email_notifications ADD CONSTRAINT email_notifications_kind_check CHECK (kind IN ('department','assignment','technical'));
