ALTER TABLE tickets ADD COLUMN version bigint NOT NULL DEFAULT 1;
ALTER TABLE ticket_messages ADD COLUMN request_key text;
ALTER TABLE ticket_messages ADD COLUMN request_hash text;
CREATE UNIQUE INDEX messages_request_key ON ticket_messages(user_id,request_key) WHERE request_key IS NOT NULL;
CREATE INDEX login_flows_expiry ON login_flows(expires_at);
CREATE INDEX tickets_assignee_updated ON tickets(assigned_to,updated_at DESC,id DESC);
CREATE TABLE ticket_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 ticket_id bigint NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
 user_id bigint NOT NULL REFERENCES users(id),
 kind text NOT NULL CHECK(kind IN ('status','priority','assignment')),
 previous text NOT NULL,
 current text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX events_ticket ON ticket_events(ticket_id,id DESC);
