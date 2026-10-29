-- Preserve existing statuses; only newly created tickets start as new.
ALTER TABLE tickets ALTER COLUMN status SET DEFAULT 'new';
ALTER TABLE tickets DROP CONSTRAINT tickets_status_check;
ALTER TABLE tickets ADD CONSTRAINT tickets_status_check
 CHECK (status IN ('new','open','waiting','on_hold','solved','closed'));
