CREATE INDEX IF NOT EXISTS tickets_priority_updated ON tickets(priority,updated_at DESC,id DESC);
