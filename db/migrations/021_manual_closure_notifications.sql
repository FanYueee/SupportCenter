ALTER TABLE user_notifications DROP CONSTRAINT user_notifications_kind_check;
ALTER TABLE user_notifications ADD CONSTRAINT user_notifications_kind_check
 CHECK (kind IN ('solved','closed','auto_closed'));

-- Repair missing manual-closure notices for recipients of a resolved notice.
-- Preserve the original notification and its read state; recheck membership.
INSERT INTO user_notifications(user_id,ticket_id,kind,cycle_at,created_at)
SELECT DISTINCT n.user_id,t.id,'closed',e.created_at,e.created_at
FROM user_notifications n
JOIN tickets t ON t.id=n.ticket_id
JOIN users u ON u.id=n.user_id
JOIN LATERAL (
 SELECT created_at FROM ticket_events
 WHERE ticket_id=t.id AND kind='status' AND current='closed'
 ORDER BY id DESC LIMIT 1
) e ON true
WHERE n.kind='solved' AND t.status='closed' AND t.auto_closed_at IS NULL
 AND e.created_at>=n.cycle_at
 AND (t.user_id=u.id OR (u.email_verified AND EXISTS(
  SELECT 1 FROM ticket_invites i WHERE i.ticket_id=t.id AND i.email_key=lower(u.email))))
ON CONFLICT(user_id,ticket_id,kind,cycle_at) DO NOTHING;
