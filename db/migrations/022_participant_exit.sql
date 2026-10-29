ALTER TABLE ticket_events DROP CONSTRAINT ticket_events_kind_check;
ALTER TABLE ticket_events ADD CONSTRAINT ticket_events_kind_check
 CHECK (kind IN ('status','priority','assignment','handoff','invite','uninvite','category','leave'));

CREATE INDEX users_verified_email ON users(lower(email),id) WHERE email_verified;
