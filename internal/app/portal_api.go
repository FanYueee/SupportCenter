package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"supportcenter/internal/database"
	"supportcenter/internal/model"
)

func (a *App) registerPortalAPI(mux *http.ServeMux) {
	if a.PortalAPI == nil {
		return
	}
	prefix := "/api/portal/v1"
	add := func(pattern string, staff bool, handler http.HandlerFunc) {
		mux.HandleFunc(pattern, a.portalProtected(staff, handler))
	}
	mux.HandleFunc("POST "+prefix+"/store/priority-support", a.portalSigned(portalStoreIssuer, false, a.portalSetPrioritySupport))
	add("GET "+prefix+"/metadata", false, a.portalMetadata)
	add("GET "+prefix+"/tickets", false, a.portalList)
	add("POST "+prefix+"/tickets", false, a.portalCreate)
	add("GET "+prefix+"/tickets/{id}", false, a.portalDetail)
	add("POST "+prefix+"/tickets/{id}/reply", false, a.portalReply)
	add("POST "+prefix+"/tickets/{id}/update", false, a.portalUpdate)
	for _, action := range []string{"invite", "uninvite", "leave", "handoff", "resolve"} {
		add("POST "+prefix+"/tickets/{id}/"+action, false, a.portalAction)
	}
	add("GET "+prefix+"/attachments/{id}", false, a.portalDownload)
	add("GET "+prefix+"/attachments/{id}/preview", false, a.portalDownload)
	add("GET "+prefix+"/tickets/{ticket}/attachments/{id}", false, a.portalDownload)
	add("GET "+prefix+"/tickets/{ticket}/attachments/{id}/preview", false, a.portalDownload)
	add("GET "+prefix+"/notifications", false, a.portalNotifications)
	add("POST "+prefix+"/notifications/read-all", false, a.portalReadNotification)
	add("POST "+prefix+"/notifications/{id}/read", false, a.portalReadNotification)
	add("GET "+prefix+"/staff/tickets", true, a.portalList)
	add("GET "+prefix+"/staff/tickets/{id}", true, a.portalDetail)
	add("POST "+prefix+"/staff/tickets/{id}/reply", true, a.portalReply)
	add("POST "+prefix+"/staff/tickets/{id}/update", true, a.portalUpdate)
	add("GET "+prefix+"/staff/attachments/{id}", true, a.portalDownload)
	add("GET "+prefix+"/staff/attachments/{id}/preview", true, a.portalDownload)
	add("GET "+prefix+"/staff/tickets/{ticket}/attachments/{id}", true, a.portalDownload)
	add("GET "+prefix+"/staff/tickets/{ticket}/attachments/{id}/preview", true, a.portalDownload)
	add("GET "+prefix+"/staff/settings", true, a.portalSettings)
	add("POST "+prefix+"/staff/settings/{section}", true, a.portalSaveSettings)
	add("GET "+prefix+"/account", false, a.portalAccount)
}

func portalID(r *http.Request) (int64, bool) {
	value := r.PathValue("id")
	if len(value) > 16 || value == "" || strings.Trim(value, "0123456789") != "" {
		return 0, false
	}
	id, err := strconv.ParseInt(value, 10, 64)
	return id, err == nil && id > 0 && id <= 9007199254740991
}

func portalPage(r *http.Request, key string) (int, bool) {
	values, ok := r.URL.Query()[key]
	if !ok {
		return 1, true
	}
	if len(values) != 1 || values[0] == "" || len(values[0]) > 4 {
		return 0, false
	}
	page, err := strconv.Atoi(values[0])
	return page, err == nil && page >= 1 && page <= 1000
}
func portalDecode(w http.ResponseWriter, r *http.Request, dst any) bool {
	media, params, typeErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if typeErr != nil || media != "application/json" || (params["charset"] != "" && !strings.EqualFold(params["charset"], "utf-8")) {
		portalError(w, 415, "json_required")
		return false
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil || !utf8.Valid(raw) {
		portalError(w, 400, "invalid_json")
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(dst) != nil {
		portalError(w, 400, "invalid_json")
		return false
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		portalError(w, 400, "invalid_json")
		return false
	}
	return true
}
func (a *App) portalList(w http.ResponseWriter, r *http.Request) {
	identity := portalCurrent(r)
	q := r.URL.Query()
	page, ok := portalPage(r, "page")
	status, phase, priority, assigned, sort, search := q.Get("status"), q.Get("phase"), q.Get("priority"), q.Get("assigned"), q.Get("sort"), strings.TrimSpace(q.Get("q"))
	priority = portalPriority(priority)
	for key, values := range q {
		if len(values) != 1 || !valid(key, "page", "status", "phase", "priority", "assigned", "sort", "q", "category") {
			ok = false
		}
	}
	if !ok || !valid(status, "", "new", "open", "waiting", "on_hold", "solved", "closed") || !valid(phase, "", "active", "done") || (priority != "" && !slices.Contains(model.Priorities, priority)) || !valid(assigned, "", "mine", "unassigned") || !valid(sort, "", "updated_desc", "updated_asc", "created_desc", "created_asc") || len([]rune(search)) > 100 || !utf8.ValidString(search) || strings.ContainsRune(search, 0) || !identity.Staff && (priority != "" || assigned != "" || status == "on_hold") {
		portalError(w, 400, "invalid_query")
		return
	}
	list, counts, more, err := a.Store.PortalList(r.Context(), identity.User, identity.Staff, status, priority, search, page, assigned, q.Get("category"), sort, phase)
	if err != nil {
		a.portalFailure(w, err)
		return
	}
	tickets := []map[string]any{}
	for _, ticket := range list {
		tickets = append(tickets, portalTicket(ticket, identity.User.ID, identity.Staff))
	}
	if !identity.Staff {
		counts.Hold = 0
	}
	portalJSON(w, 200, map[string]any{"tickets": tickets, "counts": counts, "hasMore": more, "pageSize": 20, "page": page})
}
func (a *App) portalDetail(w http.ResponseWriter, r *http.Request) {
	identity := portalCurrent(r)
	id, ok := portalID(r)
	page, validPage := portalPage(r, "messages")
	if !ok {
		portalError(w, 404, "not_found")
		return
	}
	if !validPage {
		portalError(w, 400, "invalid_query")
		return
	}
	ticket, err := a.Store.Ticket(r.Context(), identity.User, id, identity.Staff)
	if err != nil {
		a.portalFailure(w, err)
		return
	}
	messages, more, err := a.Store.Messages(r.Context(), identity.User, id, identity.Staff, page)
	if err != nil {
		a.portalFailure(w, err)
		return
	}
	invited, err := a.Store.Invites(r.Context(), identity.User, id)
	if err != nil {
		a.portalFailure(w, err)
		return
	}
	invites := []map[string]any{}
	for _, person := range invited {
		invites = append(invites, map[string]any{"email": person.Email})
	}
	own := ticket.UserID == identity.User.ID
	data := map[string]any{"ticket": portalTicket(ticket, identity.User.ID, identity.Staff), "messages": portalMessages(messages), "messagePage": page, "hasMoreMessages": more, "participants": invites,
		"capabilities": map[string]bool{"canReply": ticket.Status != "closed" || identity.Staff, "canClose": own && !identity.Staff && ticket.Status != "closed", "canReopen": own && ticket.Status == "closed",
			"canAdminReply": identity.Staff, "canChangeStatus": identity.Staff, "canInvite": own && !identity.Staff, "canLeave": !own && !identity.Staff,
			"canInternalNote": identity.Staff, "canHandoff": own && !identity.Staff && ticket.AssistanceMode == "ai" && ticket.Status != "closed",
			"canResolveAI": own && !identity.Staff && ticket.AssistanceMode == "ai" && ticket.Status == "waiting"}}
	if identity.Staff {
		events, err := a.Store.Events(r.Context(), identity.User, id)
		if err != nil {
			a.portalFailure(w, err)
			return
		}
		staff, err := a.Store.PortalStaff(r.Context())
		if err != nil {
			a.portalFailure(w, err)
			return
		}
		people := []map[string]any{}
		for _, user := range staff {
			people = append(people, portalPerson(user, false))
		}
		data["events"] = portalEvents(events)
		data["staff"] = people
		data["selfStaffId"] = identity.User.ID
		data["statuses"] = portalStatuses(true)
	}
	portalJSON(w, 200, data)
}
func (a *App) portalForm(w http.ResponseWriter, r *http.Request, fields ...string) bool {
	media, params, typeErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if typeErr != nil || media != "multipart/form-data" || params["boundary"] == "" {
		portalError(w, 415, "multipart_required")
		return false
	}
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		portalError(w, 400, "invalid_form")
		return false
	}
	for key, values := range r.MultipartForm.Value {
		if !valid(key, fields...) || (key != "participants" && len(values) != 1) {
			r.MultipartForm.RemoveAll()
			portalError(w, 400, "invalid_form")
			return false
		}
	}
	for key := range r.MultipartForm.File {
		if key != "attachments" {
			r.MultipartForm.RemoveAll()
			portalError(w, 400, "invalid_form")
			return false
		}
	}
	return true
}
func (a *App) portalCreate(w http.ResponseWriter, r *http.Request) {
	identity := portalCurrent(r)
	if !a.portalForm(w, r, "subject", "body", "category", "priority", "assistance", "submission", "participants") {
		return
	}
	defer r.MultipartForm.RemoveAll()
	subject, body, category, priority, mode := strings.TrimSpace(r.PostFormValue("subject")), strings.TrimSpace(r.PostFormValue("body")), r.PostFormValue("category"), r.PostFormValue("priority"), r.PostFormValue("assistance")
	if mode == "" {
		mode = "human"
	}
	priority = portalPriority(priority)
	if !utf8.ValidString(subject) || strings.ContainsRune(subject, 0) || len([]rune(subject)) < 1 || len([]rune(subject)) > 150 || !validBody(body) || !valid(category, "other", "billing", "technical") || !slices.Contains(model.Priorities, priority) || !valid(mode, "human", "ai") || (mode == "ai" && !a.AIEnabled) || !submissionKey.MatchString(r.PostFormValue("submission")) {
		portalError(w, 422, "invalid")
		return
	}
	support, err := a.prioritySupport(r.Context(), identity.User.ID, identity.CanStaff)
	if err != nil {
		a.portalFailure(w, err)
		return
	}
	if !model.Allows(support.Level, priority) {
		portalError(w, 403, "priority_locked")
		return
	}
	participants, err := database.NormalizeInviteEmails(r.PostForm["participants"], identity.User.Email)
	if err != nil {
		a.portalFailure(w, err)
		return
	}
	ticket := model.Ticket{Subject: subject, Category: category, Priority: priority, AssistanceMode: mode}
	files, err := a.uploads(r)
	if err != nil {
		portalError(w, 422, "invalid_attachment")
		return
	}
	op := operation(r, struct {
		Ticket       model.Ticket
		Body         string
		Participants []string
	}{ticket, body, participants}, files)
	id, err := a.Store.CreateWithParticipants(r.Context(), identity.User, ticket, body, files, participants, op)
	if err != nil && !errors.Is(err, database.ErrCommitUnknown) {
		a.removeFiles(files)
	}
	if err != nil && !errors.Is(err, database.ErrReplayed) {
		a.portalFailure(w, err)
		return
	}
	portalJSON(w, 200, map[string]any{"id": id, "replayed": errors.Is(err, database.ErrReplayed)})
}
func (a *App) portalReply(w http.ResponseWriter, r *http.Request) {
	identity := portalCurrent(r)
	id, ok := portalID(r)
	if !ok {
		portalError(w, 404, "not_found")
		return
	}
	// Authorize before storing any private attachment.
	if _, err := a.Store.Ticket(r.Context(), identity.User, id, identity.Staff); err != nil {
		a.portalFailure(w, err)
		return
	}
	if !a.portalForm(w, r, "body", "internal", "status", "version", "submission") {
		return
	}
	defer r.MultipartForm.RemoveAll()
	body := strings.TrimSpace(r.PostFormValue("body"))
	internal := r.PostFormValue("internal") == "true"
	status := r.PostFormValue("status")
	version, _ := strconv.ParseInt(r.PostFormValue("version"), 10, 64)
	if !validBody(body) || !submissionKey.MatchString(r.PostFormValue("submission")) || (!identity.Staff && (internal || status != "")) || internal && status != "" || !valid(r.PostFormValue("internal"), "", "true", "false") {
		portalError(w, 422, "invalid")
		return
	}
	if !internal && identity.User.Role == "admin" && identity.User.Signature != "" {
		body += "\n\n" + identity.User.Signature
		if !validBody(body) {
			portalError(w, 422, "invalid")
			return
		}
	}
	files, err := a.uploads(r)
	if err != nil {
		portalError(w, 422, "invalid_attachment")
		return
	}
	op := operation(r, struct {
		Body, Status string
		Internal     bool
		Version      int64
	}{body, status, internal, version}, files)
	err = a.Store.PortalReply(r.Context(), identity.User, id, identity.Staff, body, internal, files, status, version, op)
	if err != nil && !errors.Is(err, database.ErrCommitUnknown) {
		a.removeFiles(files)
	}
	if err != nil && !errors.Is(err, database.ErrReplayed) {
		a.portalFailure(w, err)
		return
	}
	portalJSON(w, 200, map[string]any{"id": id, "replayed": errors.Is(err, database.ErrReplayed)})
}
func (a *App) portalUpdate(w http.ResponseWriter, r *http.Request) {
	identity := portalCurrent(r)
	id, ok := portalID(r)
	if !ok {
		portalError(w, 404, "not_found")
		return
	}
	var input struct {
		Version  int64    `json:"version"`
		Status   *string  `json:"status"`
		Priority *string  `json:"priority"`
		Category *string  `json:"category"`
		Team     *[]int64 `json:"team"`
	}
	if !portalDecode(w, r, &input) {
		return
	}
	if input.Version < 1 {
		portalError(w, 422, "invalid")
		return
	}
	ticket, err := a.Store.Ticket(r.Context(), identity.User, id, identity.Staff)
	if err != nil {
		a.portalFailure(w, err)
		return
	}
	status, priority, category := ticket.Status, ticket.Priority, ticket.Category
	team := []int64{}
	for _, person := range ticket.Team {
		team = append(team, person.ID)
	}
	if input.Status != nil {
		status = *input.Status
	}
	if input.Priority != nil {
		priority = portalPriority(*input.Priority)
	}
	if input.Category != nil {
		category = *input.Category
	}
	if input.Team != nil {
		team = *input.Team
	}
	if !valid(status, "new", "open", "waiting", "on_hold", "solved", "closed") || !slices.Contains(model.Priorities, priority) || !valid(category, "other", "billing", "technical") || !identity.Staff && (input.Priority != nil || input.Category != nil || input.Team != nil) {
		portalError(w, 422, "invalid")
		return
	}
	err = a.Store.UpdateTeamCategory(r.Context(), identity.User, id, identity.Staff, status, priority, category, team, input.Version)
	if err != nil {
		a.portalFailure(w, err)
		return
	}
	portalJSON(w, 200, map[string]any{"id": id})
}
func (a *App) portalAction(w http.ResponseWriter, r *http.Request) {
	identity := portalCurrent(r)
	id, ok := portalID(r)
	if !ok {
		portalError(w, 404, "not_found")
		return
	}
	var input struct {
		Email   string `json:"email"`
		Version int64  `json:"version"`
	}
	if !portalDecode(w, r, &input) {
		return
	}
	var err error
	switch r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:] {
	case "invite":
		err = a.Store.Invite(r.Context(), identity.User, id, input.Email)
	case "uninvite":
		err = a.Store.Uninvite(r.Context(), identity.User, id, input.Email)
	case "leave":
		err = a.Store.LeaveTicket(r.Context(), identity.User, id)
	case "handoff":
		err = a.Store.RequestHuman(r.Context(), identity.User, id)
	case "resolve":
		if input.Version < 1 {
			portalError(w, 422, "invalid")
			return
		}
		err = a.Store.ResolveAI(r.Context(), identity.User, id, input.Version)
	default:
		portalError(w, 404, "not_found")
		return
	}
	if err != nil {
		a.portalFailure(w, err)
		return
	}
	portalJSON(w, 200, map[string]any{"id": id})
}
func (a *App) portalDownload(w http.ResponseWriter, r *http.Request) {
	identity := portalCurrent(r)
	id, ok := portalID(r)
	if !ok {
		portalError(w, 404, "not_found")
		return
	}
	if _, err := a.Store.Attachment(r.Context(), identity.User, id); err != nil {
		a.portalFailure(w, err)
		return
	}
	if ticket := r.PathValue("ticket"); ticket != "" {
		ticketID, err := strconv.ParseInt(ticket, 10, 64)
		if err != nil || ticketID < 1 || ticketID > 9007199254740991 {
			portalError(w, 404, "not_found")
			return
		}
		var matches bool
		if err = a.Store.Pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM ticket_attachments a JOIN ticket_messages m ON m.id=a.message_id WHERE a.id=$1 AND m.ticket_id=$2)`, id, ticketID).Scan(&matches); err != nil {
			a.portalFailure(w, err)
			return
		}
		if !matches {
			portalError(w, 404, "not_found")
			return
		}
	}
	r = r.WithContext(context.WithValue(r.Context(), sessionKey{}, Session{User: identity.User}))
	if strings.HasSuffix(r.URL.Path, "/preview") {
		a.preview(w, r)
	} else {
		a.download(w, r)
	}
}
func (a *App) portalNotifications(w http.ResponseWriter, r *http.Request) {
	identity := portalCurrent(r)
	page, ok := portalPage(r, "page")
	if !ok {
		portalError(w, 400, "invalid_query")
		return
	}
	notifications, more, err := a.Store.Notifications(r.Context(), identity.User.ID, page)
	if err != nil {
		a.portalFailure(w, err)
		return
	}
	unread, err := a.Store.UnreadNotifications(r.Context(), identity.User.ID)
	if err != nil {
		a.portalFailure(w, err)
		return
	}
	portalJSON(w, 200, map[string]any{"items": portalNotificationsDTO(notifications), "hasMore": more, "unreadCount": unread})
}
func (a *App) portalReadNotification(w http.ResponseWriter, r *http.Request) {
	identity := portalCurrent(r)
	var err error
	if strings.HasSuffix(r.URL.Path, "/read-all") {
		err = a.Store.ReadAllNotifications(r.Context(), identity.User.ID)
	} else {
		id, ok := portalID(r)
		if !ok {
			portalError(w, 404, "not_found")
			return
		}
		_, err = a.Store.ReadNotification(r.Context(), identity.User.ID, id)
	}
	if err != nil {
		a.portalFailure(w, err)
		return
	}
	portalJSON(w, 200, map[string]bool{"read": true})
}
func (a *App) portalAccount(w http.ResponseWriter, r *http.Request) {
	identity := portalCurrent(r)
	page, ok := portalPage(r, "page")
	if !ok {
		portalError(w, 400, "invalid_query")
		return
	}
	activities, more, err := a.Store.Activities(r.Context(), identity.User.ID, page)
	if err != nil {
		a.portalFailure(w, err)
		return
	}
	support, err := a.Store.PrioritySupport(r.Context(), identity.User.ID)
	if err != nil {
		a.portalFailure(w, err)
		return
	}
	portalJSON(w, 200, map[string]any{"name": identity.User.Name, "activities": portalActivities(activities), "hasMore": more, "prioritySupport": portalPrioritySupport(support, identity.CanStaff)})
}
func (a *App) portalSettings(w http.ResponseWriter, r *http.Request) {
	identity := portalCurrent(r)
	if !identity.Manage {
		portalError(w, 403, "forbidden")
		return
	}
	automation, err := a.Store.Automation(r.Context())
	if err != nil {
		a.portalFailure(w, err)
		return
	}
	prompts, err := a.Store.AIPrompts(r.Context())
	if err != nil {
		a.portalFailure(w, err)
		return
	}
	staff, err := a.Store.PortalStaff(r.Context())
	if err != nil {
		a.portalFailure(w, err)
		return
	}
	announcements, err := a.Store.ManagedAnnouncements(r.Context(), identity.User)
	if err != nil {
		a.portalFailure(w, err)
		return
	}
	people := []map[string]any{}
	for _, person := range staff {
		people = append(people, portalPerson(person, true))
	}
	portalJSON(w, 200, map[string]any{"automation": portalAutomation(automation), "prompts": map[string]string{"general": prompts.General, "billing": prompts.Billing, "technical": prompts.Technical}, "staff": people, "announcements": portalAnnouncements(announcements),
		"signature": identity.User.Signature, "name": identity.User.Name, "aiEnabled": a.AIEnabled, "mailEnabled": a.MailEnabled})
}
func (a *App) portalSaveSettings(w http.ResponseWriter, r *http.Request) {
	identity := portalCurrent(r)
	if !identity.Manage {
		portalError(w, 403, "forbidden")
		return
	}
	var err error
	switch r.PathValue("section") {
	case "signature":
		var input struct {
			Value string `json:"value"`
		}
		if !portalDecode(w, r, &input) {
			return
		}
		err = a.Store.SetSignature(r.Context(), identity.User, input.Value)
	case "name":
		var input struct {
			Value string `json:"value"`
		}
		if !portalDecode(w, r, &input) {
			return
		}
		err = a.Store.SetDisplayName(r.Context(), identity.User, input.Value)
	case "automation":
		var input model.AutomationSettings
		if !portalDecode(w, r, &input) {
			return
		}
		err = a.Store.SaveAutomation(r.Context(), input)
	case "prompts":
		var input model.AIPrompts
		if !portalDecode(w, r, &input) {
			return
		}
		err = a.Store.SaveAIPrompts(r.Context(), input)
	case "departments":
		var input struct {
			UserID     int64    `json:"userId"`
			Categories []string `json:"categories"`
		}
		if !portalDecode(w, r, &input) {
			return
		}
		err = a.Store.SaveStaffDepartments(r.Context(), identity.User, input.UserID, input.Categories)
	case "announcement":
		var input model.Announcement
		if !portalDecode(w, r, &input) {
			return
		}
		_, err = a.Store.SaveAnnouncement(r.Context(), identity.User, input)
	case "announcement-action":
		var input struct {
			ID, Version int64
			Action      string
		}
		if !portalDecode(w, r, &input) {
			return
		}
		err = a.Store.ChangeAnnouncement(r.Context(), identity.User, input.ID, input.Version, input.Action)
	default:
		portalError(w, 404, "not_found")
		return
	}
	if err != nil {
		a.portalFailure(w, err)
		return
	}
	portalJSON(w, 200, map[string]bool{"saved": true})
}
