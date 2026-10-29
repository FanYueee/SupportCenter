ALTER TABLE users ADD COLUMN avatar_source text NOT NULL DEFAULT 'gravatar'
 CHECK (avatar_source IN ('gravatar','initial'));
ALTER TABLE users ADD COLUMN name_locked boolean NOT NULL DEFAULT false;
ALTER TABLE users ADD COLUMN signature text NOT NULL DEFAULT '' CHECK (length(signature) <= 500);

CREATE TABLE account_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 kind text NOT NULL CHECK (kind IN ('login','logout','avatar','name','signature')),
 detail text NOT NULL DEFAULT '' CHECK (length(detail) <= 100),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX account_events_user_kind_recent ON account_events(user_id,kind,created_at DESC,id DESC);
CREATE INDEX ticket_messages_user_recent ON ticket_messages(user_id,created_at DESC,id DESC);
CREATE INDEX ticket_events_user_recent ON ticket_events(user_id,created_at DESC,id DESC);
CREATE INDEX tickets_user_created ON tickets(user_id,created_at DESC,id DESC);
