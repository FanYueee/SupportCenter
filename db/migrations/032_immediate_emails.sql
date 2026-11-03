-- Every public staff or AI reply gets its own email, sent right away and
-- quoting that reply, and requesters get one when they open a ticket.
ALTER TABLE email_settings ADD COLUMN on_create boolean NOT NULL DEFAULT true;

ALTER TABLE customer_emails ADD COLUMN message_id bigint REFERENCES ticket_messages(id) ON DELETE CASCADE;
UPDATE customer_emails e SET message_id=(SELECT max(m.id) FROM ticket_messages m JOIN users u ON u.id=m.user_id
 WHERE m.ticket_id=e.ticket_id AND NOT m.internal AND NOT m.service_notice AND (m.source='ai' OR (m.source='human' AND u.role IN ('support','admin'))))
 WHERE e.kind='reply' AND e.state IN ('pending','sending');
ALTER TABLE customer_emails DROP COLUMN last_reply_at;

ALTER TABLE customer_emails DROP CONSTRAINT customer_emails_kind_check;
ALTER TABLE customer_emails ADD CONSTRAINT customer_emails_kind_check CHECK (kind IN ('created','reply','invite'));
ALTER TABLE customer_emails DROP CONSTRAINT customer_emails_check;
ALTER TABLE customer_emails ADD CONSTRAINT customer_emails_check CHECK ((kind='invite')=(recipient_user_id IS NULL));

-- Only invitations still wait one per address and ticket.
DROP INDEX customer_emails_waiting;
CREATE UNIQUE INDEX customer_emails_waiting ON customer_emails(ticket_id,kind,recipient_email) WHERE state='pending' AND kind='invite';
