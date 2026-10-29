-- Keep existing relative urgency while making the active choices standard/urgent.
UPDATE tickets SET priority='normal' WHERE priority='low';
UPDATE tickets SET priority='urgent' WHERE priority='high';
ALTER TABLE tickets DROP CONSTRAINT tickets_priority_check;
ALTER TABLE tickets ADD CONSTRAINT tickets_priority_check CHECK (priority IN ('normal','urgent'));
