-- The site a ticket was opened on, so its emails carry that brand and link back to it.
ALTER TABLE tickets ADD COLUMN brand text NOT NULL DEFAULT 'atomroute' CHECK (brand ~ '^[a-z0-9-]{1,32}$');

-- People can turn off ticket emails in their own settings.
ALTER TABLE users ADD COLUMN email_notify boolean NOT NULL DEFAULT true;

CREATE TABLE email_settings (
 id smallint PRIMARY KEY DEFAULT 1 CHECK (id=1),
 on_reply boolean NOT NULL DEFAULT true,
 include_reply boolean NOT NULL DEFAULT true,
 on_invite boolean NOT NULL DEFAULT true,
 last_sent_at timestamptz,
 last_error text NOT NULL DEFAULT '' CHECK (char_length(last_error) <= 200),
 last_error_at timestamptz,
 updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO email_settings(id) VALUES(1);

-- Emails to customers, queued with the reply or invitation that caused them.
CREATE TABLE customer_emails (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 ticket_id bigint NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
 kind text NOT NULL CHECK (kind IN ('reply','invite')),
 recipient_email text NOT NULL CHECK (char_length(recipient_email) BETWEEN 3 AND 254),
 recipient_user_id bigint REFERENCES users(id) ON DELETE CASCADE,
 actor_user_id bigint REFERENCES users(id) ON DELETE SET NULL,
 state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','sending','sent','cancelled','failed')),
 attempts smallint NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 5),
 available_at timestamptz NOT NULL DEFAULT now(),
 lease_until timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 -- Latest reply this email covers; if the person answers after it, they have seen it.
 last_reply_at timestamptz NOT NULL DEFAULT now(),
 sent_at timestamptz,
 CHECK ((kind='reply')=(recipient_user_id IS NOT NULL))
);
-- One waiting email per person, ticket and kind: quick follow-up replies join it.
CREATE UNIQUE INDEX customer_emails_waiting ON customer_emails(ticket_id,kind,recipient_email) WHERE state='pending';
CREATE INDEX customer_emails_ready ON customer_emails(available_at,id) WHERE state IN ('pending','sending');
CREATE INDEX customer_emails_invites ON customer_emails(created_at,actor_user_id,recipient_email) WHERE kind='invite';
