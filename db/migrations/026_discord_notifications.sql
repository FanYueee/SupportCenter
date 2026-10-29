-- Optional Discord webhook for staff: one channel, set by an admin.
-- The token is a credential; it is never rendered back to the browser.
CREATE TABLE discord_settings (
 id smallint PRIMARY KEY DEFAULT 1 CHECK (id=1),
 webhook_id text NOT NULL DEFAULT '' CHECK (webhook_id ~ '^([0-9]{1,20})?$'),
 webhook_token text NOT NULL DEFAULT '' CHECK (webhook_token ~ '^[A-Za-z0-9_-]{0,100}$'),
 on_ticket boolean NOT NULL DEFAULT true,
 on_reply boolean NOT NULL DEFAULT true,
 include_preview boolean NOT NULL DEFAULT true,
 last_sent_at timestamptz,
 last_error text NOT NULL DEFAULT '' CHECK (char_length(last_error) <= 200),
 last_error_at timestamptz,
 updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK ((webhook_id='')=(webhook_token=''))
);
INSERT INTO discord_settings(id) VALUES(1);

-- Queued in the same transaction as the ticket or reply, sent by a worker.
CREATE TABLE discord_notifications (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 ticket_id bigint NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
 message_id bigint NOT NULL REFERENCES ticket_messages(id) ON DELETE CASCADE,
 kind text NOT NULL CHECK (kind IN ('ticket','reply')),
 state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','sending','sent','cancelled','failed')),
 attempts smallint NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 5),
 available_at timestamptz NOT NULL DEFAULT now(),
 lease_until timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 sent_at timestamptz
);
CREATE INDEX discord_notifications_ready ON discord_notifications(available_at,id)
 WHERE state IN ('pending','sending');
