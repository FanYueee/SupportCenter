-- Four priorities. Customers may file tickets up to their account's priority
-- support level, which lapses back to low after priority_until; staff may use
-- any. The former default "normal" becomes "low".
ALTER TABLE tickets DROP CONSTRAINT tickets_priority_check;
UPDATE tickets SET priority='low' WHERE priority='normal';
ALTER TABLE tickets ALTER COLUMN priority SET DEFAULT 'low';
ALTER TABLE tickets ADD CONSTRAINT tickets_priority_check CHECK (priority IN ('low','medium','high','urgent'));
UPDATE ticket_events SET previous='low' WHERE kind='priority' AND previous='normal';
UPDATE ticket_events SET current='low' WHERE kind='priority' AND current='normal';

ALTER TABLE users ADD COLUMN priority_level text NOT NULL DEFAULT 'low' CHECK (priority_level IN ('low','medium','high','urgent'));
ALTER TABLE users ADD COLUMN priority_until timestamptz;
