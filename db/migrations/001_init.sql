CREATE TABLE IF NOT EXISTS users (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 auth0_id text NOT NULL UNIQUE,
 name text NOT NULL,
 email text NOT NULL,
 role text NOT NULL DEFAULT 'user' CHECK (role IN ('user','support','admin'))
);
CREATE TABLE IF NOT EXISTS sessions (
 token_hash text PRIMARY KEY,
 user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 csrf text NOT NULL,
 expires_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS sessions_expiry ON sessions(expires_at);
CREATE TABLE IF NOT EXISTS login_flows (
 state_hash text PRIMARY KEY,
 nonce text NOT NULL,
 verifier text NOT NULL,
 expires_at timestamptz NOT NULL
);
CREATE TABLE IF NOT EXISTS tickets (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 user_id bigint NOT NULL REFERENCES users(id),
 subject text NOT NULL CHECK (length(subject) BETWEEN 1 AND 160),
 category text NOT NULL CHECK (category IN ('technical','billing','account','other')),
 status text NOT NULL DEFAULT 'open' CHECK (status IN ('open','waiting','closed')),
 priority text NOT NULL DEFAULT 'normal' CHECK (priority IN ('low','normal','high','urgent')),
 assigned_to bigint REFERENCES users(id),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS tickets_user_updated ON tickets(user_id,updated_at DESC,id DESC);
CREATE INDEX IF NOT EXISTS tickets_status_updated ON tickets(status,updated_at DESC,id DESC);
CREATE INDEX IF NOT EXISTS tickets_updated ON tickets(updated_at DESC,id DESC);
CREATE TABLE IF NOT EXISTS ticket_messages (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 ticket_id bigint NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
 user_id bigint NOT NULL REFERENCES users(id),
 body text NOT NULL CHECK (length(body) BETWEEN 1 AND 20000),
 internal boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS messages_ticket ON ticket_messages(ticket_id,id DESC);
CREATE TABLE IF NOT EXISTS ticket_attachments (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 message_id bigint NOT NULL REFERENCES ticket_messages(id) ON DELETE CASCADE,
 storage_key text NOT NULL UNIQUE,
 name text NOT NULL,
 mime text NOT NULL,
 size bigint NOT NULL CHECK(size > 0)
);
CREATE INDEX IF NOT EXISTS attachments_message ON ticket_attachments(message_id);
