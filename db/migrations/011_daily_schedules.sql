ALTER TABLE automation_settings ADD COLUMN weekly_schedule jsonb NOT NULL DEFAULT '[]'::jsonb;
UPDATE automation_settings s SET weekly_schedule=(
 SELECT jsonb_agg(jsonb_build_object(
  'enabled', (s.weekdays & (1 << day_index)) <> 0,
  'ranges', s.office_ranges
 ) ORDER BY day_index)
 FROM generate_series(0,6) AS day_index
);
ALTER TABLE automation_settings DROP COLUMN weekdays, DROP COLUMN office_ranges;
ALTER TABLE automation_settings ADD CONSTRAINT automation_weekly_schedule
 CHECK (jsonb_typeof(weekly_schedule)='array' AND jsonb_array_length(weekly_schedule)=7);
