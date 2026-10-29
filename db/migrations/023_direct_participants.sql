ALTER TABLE ticket_invites ADD COLUMN user_id bigint REFERENCES users(id);
CREATE INDEX ticket_invites_user ON ticket_invites(user_id,ticket_id) WHERE user_id IS NOT NULL;
CREATE INDEX users_email ON users(lower(email),id);

-- Staff accounts are provisioned by administrators independently of email
-- verification. Other accounts must prove email ownership before claiming it.
WITH selected AS (
 SELECT DISTINCT ON (lower(email)) id,lower(email) AS email FROM users
 WHERE email_verified OR role IN ('support','admin')
 ORDER BY lower(email),email_verified DESC,id
)
UPDATE ticket_invites i SET user_id=m.id FROM selected m WHERE i.email_key=m.email;
