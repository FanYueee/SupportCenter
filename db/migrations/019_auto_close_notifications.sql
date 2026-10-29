ALTER TABLE automation_settings ADD COLUMN auto_close_days integer NOT NULL DEFAULT 0
 CHECK (auto_close_days BETWEEN 0 AND 365);
ALTER TABLE tickets ADD COLUMN solved_at timestamptz, ADD COLUMN auto_closed_at timestamptz;
-- Existing resolved tickets receive a fresh grace period when this feature is installed.
UPDATE tickets SET solved_at=now() WHERE status='solved';
CREATE INDEX tickets_solved_due ON tickets(solved_at,id) WHERE status='solved';

CREATE FUNCTION track_ticket_resolution() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status='solved' AND (TG_OP='INSERT' OR OLD.status IS DISTINCT FROM NEW.status) THEN
  NEW.solved_at=statement_timestamp();
 ELSIF NEW.status<>'solved' THEN
  NEW.solved_at=NULL;
 END IF;
 IF NEW.status<>'closed' THEN NEW.auto_closed_at=NULL; END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER tickets_resolution_time BEFORE INSERT OR UPDATE OF status ON tickets
 FOR EACH ROW EXECUTE FUNCTION track_ticket_resolution();

CREATE TABLE user_notifications (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 ticket_id bigint NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
 kind text NOT NULL CHECK (kind IN ('solved','auto_closed')),
 cycle_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 read_at timestamptz,
 UNIQUE(user_id,ticket_id,kind,cycle_at)
);
CREATE INDEX user_notifications_unread ON user_notifications(user_id,id DESC) WHERE read_at IS NULL;
CREATE INDEX user_notifications_user ON user_notifications(user_id,id DESC);
