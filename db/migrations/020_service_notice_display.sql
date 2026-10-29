ALTER TABLE ticket_messages ADD COLUMN service_notice boolean NOT NULL DEFAULT false;

-- Archive recognizable older availability replies without deleting message data.
-- Future availability notices are rendered above the reply form, not inserted.
UPDATE ticket_messages m SET service_notice=true
FROM automation_settings a
WHERE a.id=1 AND m.source='automation' AND NOT m.internal AND m.body<>''
 AND (m.body=a.off_hours_body OR m.body=a.leave_body)
 AND m.body<>'此客服單已依設定自動結案。如需繼續討論，請重新開啟客服單。';
