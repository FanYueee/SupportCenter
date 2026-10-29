ALTER TABLE automation_settings
 ADD COLUMN leave_enabled boolean NOT NULL DEFAULT false,
 ADD COLUMN leave_start_at timestamptz,
 ADD COLUMN leave_end_at timestamptz,
 ADD COLUMN leave_body text NOT NULL DEFAULT '',
 ADD CONSTRAINT automation_leave_window CHECK (
  (leave_start_at IS NULL AND leave_end_at IS NULL) OR
  (leave_start_at IS NOT NULL AND leave_end_at IS NOT NULL AND leave_start_at < leave_end_at)
 ),
 ADD CONSTRAINT automation_leave_body_length CHECK (char_length(leave_body) <= 20000),
 ADD CONSTRAINT automation_leave_enabled CHECK (
  NOT leave_enabled OR
  (leave_start_at IS NOT NULL AND leave_end_at IS NOT NULL AND char_length(btrim(leave_body)) > 0)
 );
