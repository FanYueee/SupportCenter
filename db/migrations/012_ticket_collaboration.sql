ALTER TABLE users ADD COLUMN email_verified boolean NOT NULL DEFAULT false;

CREATE TABLE ticket_assignees (
 ticket_id bigint NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
 user_id bigint NOT NULL REFERENCES users(id),
 assigned_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(ticket_id,user_id)
);
INSERT INTO ticket_assignees(ticket_id,user_id)
 SELECT id,assigned_to FROM tickets WHERE assigned_to IS NOT NULL;
CREATE INDEX ticket_assignees_user ON ticket_assignees(user_id,ticket_id);

CREATE TABLE ticket_invites (
 ticket_id bigint NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
 email_key text NOT NULL CHECK (length(email_key) BETWEEN 3 AND 254 AND email_key=lower(btrim(email_key))),
 invited_by bigint NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(ticket_id,email_key)
);
CREATE INDEX ticket_invites_email ON ticket_invites(email_key,ticket_id);

ALTER TABLE ticket_events DROP CONSTRAINT ticket_events_kind_check;
ALTER TABLE ticket_events ADD CONSTRAINT ticket_events_kind_check
 CHECK (kind IN ('status','priority','assignment','handoff','invite','uninvite'));
