-- A ticket waiting on the customer gets a reminder email and later closes by
-- itself. A closed ticket locks after a while: no more replies or reopening,
-- so the customer opens a new ticket instead. Zero hours turns a step off.
ALTER TABLE automation_settings
 ADD COLUMN waiting_remind_hours integer NOT NULL DEFAULT 12 CHECK (waiting_remind_hours BETWEEN 0 AND 720),
 ADD COLUMN waiting_close_hours integer NOT NULL DEFAULT 24 CHECK (waiting_close_hours BETWEEN 0 AND 720),
 ADD COLUMN closed_lock_hours integer NOT NULL DEFAULT 72 CHECK (closed_lock_hours BETWEEN 0 AND 8760);

ALTER TABLE tickets ADD COLUMN waiting_at timestamptz, ADD COLUMN reminded_at timestamptz, ADD COLUMN closed_at timestamptz;
-- Tickets already waiting start a fresh period; closed ones date from their last closure.
UPDATE tickets SET waiting_at=now() WHERE status='waiting';
UPDATE tickets t SET closed_at=COALESCE((SELECT max(e.created_at) FROM ticket_events e WHERE e.ticket_id=t.id AND e.kind='status' AND e.current='closed'), t.auto_closed_at, t.updated_at)
 WHERE status='closed';
CREATE INDEX tickets_waiting_due ON tickets(waiting_at,id) WHERE status='waiting';

CREATE OR REPLACE FUNCTION track_ticket_resolution() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status='solved' AND (TG_OP='INSERT' OR OLD.status IS DISTINCT FROM NEW.status) THEN
  NEW.solved_at=statement_timestamp();
 ELSIF NEW.status<>'solved' THEN
  NEW.solved_at=NULL;
 END IF;
 IF NEW.status<>'closed' THEN NEW.auto_closed_at=NULL; END IF;
 -- A staff reply to a ticket already waiting restarts the wait in the UPDATE itself.
 IF NEW.status='waiting' AND (TG_OP='INSERT' OR OLD.status IS DISTINCT FROM NEW.status) THEN
  NEW.waiting_at=statement_timestamp();
  NEW.reminded_at=NULL;
 ELSIF NEW.status<>'waiting' THEN
  NEW.waiting_at=NULL;
  NEW.reminded_at=NULL;
 END IF;
 IF NEW.status='closed' AND (TG_OP='INSERT' OR OLD.status IS DISTINCT FROM NEW.status) THEN
  NEW.closed_at=statement_timestamp();
 ELSIF NEW.status<>'closed' THEN
  NEW.closed_at=NULL;
 END IF;
 RETURN NEW;
END;
$$;

ALTER TABLE customer_emails DROP CONSTRAINT customer_emails_kind_check;
ALTER TABLE customer_emails ADD CONSTRAINT customer_emails_kind_check CHECK (kind IN ('created','reply','reminder','invite'));
