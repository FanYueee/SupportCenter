CREATE TABLE staff_departments (
 user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 category text NOT NULL CHECK (category IN ('other','billing','technical')),
 PRIMARY KEY(user_id,category)
);
CREATE INDEX staff_departments_category ON staff_departments(category,user_id);

ALTER TABLE ticket_events DROP CONSTRAINT ticket_events_kind_check;
ALTER TABLE ticket_events ADD CONSTRAINT ticket_events_kind_check
 CHECK (kind IN ('status','priority','assignment','handoff','invite','uninvite','category'));

ALTER TABLE ticket_messages DROP CONSTRAINT ticket_messages_source_check;
ALTER TABLE ticket_messages ADD CONSTRAINT ticket_messages_source_check
 CHECK (source IN ('human','ai','automation','system'));

CREATE TABLE email_notifications (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 ticket_id bigint NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
 recipient_user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 kind text NOT NULL CHECK (kind IN ('technical','assignment')),
 state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','sending','sent','cancelled','failed')),
 attempts smallint NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 5),
 available_at timestamptz NOT NULL DEFAULT now(),
 lease_until timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 sent_at timestamptz
);
CREATE INDEX email_notifications_ready ON email_notifications(available_at,id)
 WHERE state IN ('pending','sending');
