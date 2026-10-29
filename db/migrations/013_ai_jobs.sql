CREATE TABLE ai_jobs (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 ticket_id bigint NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
 trigger_message_id bigint NOT NULL UNIQUE REFERENCES ticket_messages(id) ON DELETE CASCADE,
 state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','processing','done','cancelled','failed')),
 attempts integer NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 3),
 available_at timestamptz NOT NULL DEFAULT now(),
 lease_until timestamptz,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ai_jobs_ready ON ai_jobs(available_at,id)
 WHERE state IN ('pending','processing');
