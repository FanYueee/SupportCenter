package app

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"supportcenter/internal/model"
	"supportcenter/web"
)

func portalPerson(user model.User, private bool) map[string]any {
	data := map[string]any{"id": user.ID, "name": user.Name}
	if private {
		data["email"] = user.Email
		data["role"] = user.Role
		data["departments"] = user.Departments
	}
	return data
}
func portalTicket(ticket model.Ticket, ownerID int64, staff bool) map[string]any {
	team := []map[string]any{}
	for _, person := range ticket.Team {
		team = append(team, portalPerson(person, false))
	}
	status := model.StatusForViewer(ticket.Status, staff)
	state := ticket.Status
	if !staff && state == "on_hold" {
		state = "open"
	}
	return map[string]any{"id": ticket.ID, "number": "SC-" + strconv.FormatInt(ticket.ID, 10), "version": ticket.Version,
		"subject": ticket.Subject, "category": ticket.Category, "department": model.DepartmentName(ticket.Category),
		"status": state, "statusLabel": status, "priority": ticket.Priority, "ownerName": ticket.Owner,
		"isOwner": ticket.UserID == ownerID, "team": team, "assignedTo": ticket.AssigneeID,
		"assistanceMode": ticket.AssistanceMode, "handoffAt": ticket.HandoffAt,
		"solvedAt": ticket.SolvedAt, "autoCloseAt": ticket.AutoCloseAt, "autoClosedAt": ticket.AutoClosedAt,
		"lockAt": ticket.LockAt, "locked": ticket.Locked(),
		"createdAt": ticket.CreatedAt, "updatedAt": ticket.UpdatedAt}
}

// portalMessages lists messages with the tagged items each one writes, so the
// Portal can show the tags the way this site does.
func portalMessages(messages []model.Message, refs []model.Reference, staff bool) []map[string]any {
	data := []map[string]any{}
	tags := web.TagLinks(refs)
	for _, message := range messages {
		tagged := []model.Reference{}
		for _, span := range model.ReferenceSpans(message.Body) {
			if ref, ok := tags[message.Body[span.Start:span.End]]; ok && !slices.ContainsFunc(tagged, func(r model.Reference) bool { return r.Token() == ref.Token() }) {
				tagged = append(tagged, ref)
			}
		}
		files := []map[string]any{}
		for _, file := range message.Attachments {
			files = append(files, map[string]any{"id": file.ID, "name": file.Name, "mime": file.MIME, "size": file.Size})
		}
		data = append(data, map[string]any{"id": message.ID, "name": message.Author, "isStaff": message.Role == "admin" || message.Role == "support",
			"message": message.Body, "internal": message.Internal, "source": message.Source, "createdAt": message.CreatedAt, "attachments": files, "references": portalReferences(tagged, staff)})
	}
	return data
}

// portalReferences describes tagged items. A tag in a message is its token,
// e.g. "$5895aa2a-…"; only staff get the staff address.
func portalReferences(refs []model.Reference, staff bool) []map[string]any {
	data := []map[string]any{}
	for _, ref := range refs {
		item := map[string]any{"kind": ref.Key, "kindLabel": ref.Label, "kindLabelEn": ref.LabelEn, "trigger": ref.Trigger, "id": ref.ID, "token": ref.Token(),
			"title": ref.Title, "detail": ref.Detail, "customerUrl": ref.CustomerURL}
		if staff {
			item["staffUrl"] = ref.StaffURL
			item["internal"] = ref.Internal
		}
		data = append(data, item)
	}
	return data
}

// portalNamed reads the items a new ticket is opened about, e.g.
// [{"kind":"service","id":"…"}], as ids by kind; at most ten.
func portalNamed(raw string) (map[string][]string, bool) {
	if raw == "" {
		return nil, true
	}
	var items []struct {
		Kind string `json:"kind"`
		ID   string `json:"id"`
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if len(raw) > 4096 || decoder.Decode(&items) != nil || decoder.More() || len(items) > 10 {
		return nil, false
	}
	named := map[string][]string{}
	for _, item := range items {
		if !model.ReferenceKey.MatchString(item.Kind) || !model.ReferenceID.MatchString(item.ID) {
			return nil, false
		}
		named[item.Kind] = append(named[item.Kind], item.ID)
	}
	return named, true
}

func portalDepartments() []map[string]any {
	departments := []map[string]any{}
	for index, department := range model.Departments {
		departments = append(departments, map[string]any{"id": index + 1, "category": department.Category, "name": department.Name})
	}
	return departments
}
func portalStatuses(staff bool) []map[string]string {
	statuses := []map[string]string{}
	for _, state := range []string{"new", "open", "waiting", "on_hold", "solved", "closed"} {
		if !staff && state == "on_hold" {
			continue
		}
		statuses = append(statuses, map[string]string{"title": state, "label": model.StatusForViewer(state, staff)})
	}
	return statuses
}
func (a *App) portalMetadata(w http.ResponseWriter, r *http.Request) {
	identity := portalCurrent(r)
	announcements, err := a.Store.VisibleAnnouncements(r.Context())
	if err != nil {
		a.portalFailure(w, err)
		return
	}
	notice, err := a.Store.ServiceNotice(r.Context(), time.Now())
	if err != nil {
		a.portalFailure(w, err)
		return
	}
	unread, err := a.Store.UnreadNotifications(r.Context(), identity.User.ID)
	if err != nil {
		a.portalFailure(w, err)
		return
	}
	support, err := a.Store.PrioritySupport(r.Context(), identity.User.ID)
	if err != nil {
		a.portalFailure(w, err)
		return
	}
	portalJSON(w, 200, map[string]any{"version": 1, "departments": portalDepartments(), "statuses": portalStatuses(identity.CanStaff),
		"priorities": model.Priorities, "prioritySupport": portalPrioritySupport(support, identity.CanStaff), "canStaff": identity.CanStaff, "canManageSettings": identity.CanManage,
		"aiEnabled": a.AIEnabled, "mailEnabled": a.MailEnabled, "announcements": portalAnnouncements(announcements), "serviceNotice": portalNotice(notice), "unreadNotifications": unread,
		"attachments": map[string]any{"maxFiles": 3, "maxFileBytes": maxFile, "maxTotalBytes": 30 << 20, "extensions": strings.Split(web.AttachmentTypes, ",")}})
}

func portalAnnouncements(items []model.Announcement) []map[string]any {
	out := []map[string]any{}
	for _, v := range items {
		out = append(out, map[string]any{"id": v.ID, "title": v.Title, "body": v.Body, "color": v.Color, "enabled": v.Enabled, "version": v.Version, "createdAt": v.CreatedAt, "updatedAt": v.UpdatedAt})
	}
	return out
}
func portalNotice(v *model.ServiceNotice) any {
	if v == nil {
		return nil
	}
	return map[string]string{"kind": v.Kind, "body": v.Body}
}
func portalNotificationsDTO(items []model.Notification) []map[string]any {
	out := []map[string]any{}
	for _, v := range items {
		out = append(out, map[string]any{"id": v.ID, "ticketId": v.TicketID, "kind": v.Kind, "subject": v.Subject, "createdAt": v.CreatedAt, "readAt": v.ReadAt, "autoCloseAt": v.AutoCloseAt})
	}
	return out
}
func portalEvents(items []model.Event) []map[string]any {
	out := []map[string]any{}
	for _, v := range items {
		out = append(out, map[string]any{"actor": v.Actor, "kind": v.Kind, "previous": v.Previous, "current": v.Current, "createdAt": v.CreatedAt})
	}
	return out
}
func portalActivities(items []model.Activity) []map[string]any {
	out := []map[string]any{}
	for _, v := range items {
		out = append(out, map[string]any{"kind": v.Kind, "detail": v.Detail, "ticketId": v.TicketID, "createdAt": v.CreatedAt})
	}
	return out
}
func portalAutomation(v model.AutomationSettings) map[string]any {
	return map[string]any{"autoCloseDays": v.AutoCloseDays, "timezone": v.Timezone, "onCreateBody": v.OnCreateBody, "offHoursBody": v.OffHoursBody, "leaveBody": v.LeaveBody, "days": v.Days, "leaveStartAt": v.LeaveStartAt, "leaveEndAt": v.LeaveEndAt, "onCreateEnabled": v.OnCreateEnabled, "offHoursEnabled": v.OffHoursEnabled, "leaveEnabled": v.LeaveEnabled}
}
