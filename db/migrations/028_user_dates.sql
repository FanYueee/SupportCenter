-- When each account was created. Accounts that existed before this column get
-- their earliest recorded activity instead.
ALTER TABLE users ADD COLUMN created_at timestamptz;
UPDATE users u SET created_at=COALESCE(LEAST(
 (SELECT min(created_at) FROM account_events WHERE user_id=u.id),
 (SELECT min(created_at) FROM tickets WHERE user_id=u.id),
 (SELECT min(created_at) FROM ticket_messages WHERE user_id=u.id)), now());
ALTER TABLE users ALTER COLUMN created_at SET DEFAULT now(), ALTER COLUMN created_at SET NOT NULL;

-- Portal users sign in on the Portal, never here; this records when they last used it.
ALTER TABLE users ADD COLUMN portal_seen_at timestamptz;
