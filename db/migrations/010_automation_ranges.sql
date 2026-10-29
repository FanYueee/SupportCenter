ALTER TABLE automation_settings ADD COLUMN office_ranges jsonb NOT NULL DEFAULT '[]'::jsonb;
UPDATE automation_settings SET office_ranges=jsonb_build_array(
 jsonb_build_object('start',office_start,'end',office_end));
ALTER TABLE automation_settings DROP COLUMN office_start, DROP COLUMN office_end;
ALTER TABLE automation_settings ADD CONSTRAINT automation_ranges_array
 CHECK (jsonb_typeof(office_ranges)='array' AND jsonb_array_length(office_ranges)<=8);
