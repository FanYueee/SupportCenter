-- Further ways to sign in to an account, such as a passkey next to Google.
-- The account's first sign-in method stays in users.auth0_id.
CREATE TABLE user_logins (
 subject text PRIMARY KEY,
 user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX user_logins_user ON user_logins(user_id);

ALTER TABLE account_events DROP CONSTRAINT account_events_kind_check;
ALTER TABLE account_events ADD CONSTRAINT account_events_kind_check
 CHECK (kind IN ('login','logout','avatar','name','signature','link'));
