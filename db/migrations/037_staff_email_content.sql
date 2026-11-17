-- Staff emails quote the message they are about, and staff now also hear by
-- email when a customer replies. Images from before this change ignore
-- message_id and would send a 'reply' row as a department notice.
ALTER TABLE email_notifications ADD COLUMN message_id bigint REFERENCES ticket_messages(id) ON DELETE CASCADE;
ALTER TABLE email_notifications DROP CONSTRAINT email_notifications_kind_check;
ALTER TABLE email_notifications ADD CONSTRAINT email_notifications_kind_check
 CHECK (kind IN ('department','assignment','reply','technical'));
