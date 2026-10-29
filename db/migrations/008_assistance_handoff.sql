ALTER TABLE tickets ADD COLUMN assistance_mode text NOT NULL DEFAULT 'human'
 CHECK (assistance_mode IN ('ai','human'));
ALTER TABLE tickets ADD COLUMN handoff_at timestamptz;
ALTER TABLE ticket_messages ADD COLUMN source text NOT NULL DEFAULT 'human'
 CHECK (source IN ('human','ai','automation'));
ALTER TABLE ticket_events DROP CONSTRAINT ticket_events_kind_check;
ALTER TABLE ticket_events ADD CONSTRAINT ticket_events_kind_check
 CHECK (kind IN ('status','priority','assignment','handoff'));
CREATE INDEX tickets_human_unassigned_updated ON tickets(updated_at DESC,id DESC)
 WHERE assistance_mode='human' AND assigned_to IS NULL AND status NOT IN ('solved','closed');
