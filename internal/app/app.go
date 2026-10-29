package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
	"supportcenter/internal/database"
	"supportcenter/internal/i18n"
	"supportcenter/internal/model"
	"supportcenter/web"
)

type App struct {
	Store       *database.Store
	AIEnabled   bool
	MailEnabled bool
	Auth        *Auth
	Files       *os.Root
	Origin      string
	// BrandHosts maps extra hostnames, such as support.vproxy.cloud, to the brand
	// they show. They share accounts and data with Origin.
	BrandHosts map[string]string
	Log        *slog.Logger
	AvatarHTTP *http.Client
	avatars    avatarCache
	PortalAPI  *PortalAPIConfig
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	a.registerPortalAPI(mux)
	assets, _ := fs.Sub(web.Assets, "assets")
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServerFS(assets)))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		if _, err := a.session(r); err == nil {
			http.Redirect(w, r, "/tickets", 303)
			return
		}
		a.render(w, r, model.Page{Title: "登入", Active: "login"})
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := a.Store.Pool.Ping(ctx); err != nil {
			http.Error(w, "unavailable", 503)
			return
		}
		w.Write([]byte("ok"))
	})
	signIn := a.loginLimiter(a.login)
	mux.HandleFunc("GET /login", signIn)
	mux.HandleFunc("GET /signup", signIn)
	mux.HandleFunc("GET /auth/callback", a.callback)
	mux.HandleFunc("POST /logout", a.protected(false, a.logout))
	mux.HandleFunc("POST /language", a.protected(false, a.language))
	mux.HandleFunc("GET /notifications", a.protected(false, a.notifications))
	mux.HandleFunc("POST /notifications/read", a.protected(false, a.readAllNotifications))
	mux.HandleFunc("POST /notifications/{id}/read", a.protected(false, a.readNotification))
	mux.HandleFunc("GET /account", a.protected(false, a.account))
	mux.HandleFunc("POST /account/name", a.protected(false, a.updateName))
	mux.HandleFunc("POST /account/signature", a.protected(false, a.updateSignature))
	mux.HandleFunc("GET /tickets", a.protected(false, a.list))
	mux.HandleFunc("GET /avatars/me", a.protected(false, a.myAvatar))
	mux.HandleFunc("GET /tickets/{id}/avatars/{author}", a.protected(false, a.ticketAvatar))
	mux.HandleFunc("GET /tickets/new", a.protected(false, func(w http.ResponseWriter, r *http.Request) {
		p := base(r)
		p.Title = "建立客服單"
		p.Active = "new"
		p.AIEnabled = a.AIEnabled
		a.render(w, r, p)
	}))
	mux.HandleFunc("POST /tickets", a.protected(false, a.create))
	mux.HandleFunc("GET /tickets/{id}", a.protected(false, a.detail))
	mux.HandleFunc("POST /tickets/{id}/reply", a.protected(false, a.reply))
	mux.HandleFunc("POST /tickets/{id}/handoff", a.protected(false, a.handoff))
	mux.HandleFunc("POST /tickets/{id}/resolve", a.protected(false, a.resolveAI))
	mux.HandleFunc("POST /tickets/{id}/invite", a.protected(false, a.invite))
	mux.HandleFunc("POST /tickets/{id}/uninvite", a.protected(false, a.uninvite))
	mux.HandleFunc("POST /tickets/{id}/leave", a.protected(false, a.leaveTicket))
	mux.HandleFunc("POST /tickets/{id}/update", a.protected(false, a.update))
	mux.HandleFunc("GET /attachments/{id}", a.protected(false, a.download))
	mux.HandleFunc("GET /attachments/{id}/preview", a.protected(false, a.preview))
	mux.HandleFunc("GET /admin", a.protected(true, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/admin/tickets", 303) }))
	mux.HandleFunc("GET /admin/users", a.protected(true, a.users))
	mux.HandleFunc("GET /admin/announcements", a.protected(true, a.announcements))
	mux.HandleFunc("GET /admin/announcements/{id}", a.protected(true, a.announcements))
	mux.HandleFunc("POST /admin/announcements", a.protected(true, a.saveAnnouncement))
	mux.HandleFunc("POST /admin/announcements/{id}/update", a.protected(true, a.saveAnnouncement))
	mux.HandleFunc("POST /admin/announcements/{id}/{action}", a.protected(true, a.changeAnnouncement))
	mux.HandleFunc("GET /admin/departments", a.protected(true, a.departments))
	mux.HandleFunc("POST /admin/departments/{id}", a.protected(true, a.saveDepartments))
	mux.HandleFunc("GET /admin/automation", a.protected(true, a.automation))
	mux.HandleFunc("POST /admin/automation", a.protected(true, a.saveAutomation))
	mux.HandleFunc("GET /admin/ai", a.protected(true, a.aiSettings))
	mux.HandleFunc("POST /admin/ai", a.protected(true, a.saveAISettings))
	mux.HandleFunc("GET /admin/tickets", a.protected(true, a.list))
	mux.HandleFunc("GET /admin/tickets/{id}", a.protected(true, a.detail))
	mux.HandleFunc("GET /admin/tickets/{id}/people/{person}/avatar", a.protected(true, a.ticketPersonAvatar))
	mux.HandleFunc("POST /admin/tickets/{id}/reply", a.protected(true, a.reply))
	mux.HandleFunc("POST /admin/tickets/{id}/update", a.protected(true, a.update))
	mux.HandleFunc("POST /admin/tickets/{id}/assign", a.protected(true, a.assign))
	authOrigin := ""
	if a.Auth != nil {
		authOrigin = a.Auth.Issuer
	}
	// Bound concurrent form parsing and attachment writes on a single VPS. The
	// per-client cap keeps one source's slow uploads from holding every slot;
	// signed Portal API calls all arrive from the portal server and skip it.
	slots := make(chan struct{}, 4)
	var clientsMu sync.Mutex
	clients := map[string]int{}
	limited := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			ip := ""
			if !strings.HasPrefix(r.URL.Path, "/api/portal/") {
				ip = ClientRequestInfo(r).IP
			}
			clientsMu.Lock()
			allowed := ip == "" || clients[ip] < 2
			if allowed {
				select {
				case slots <- struct{}{}:
					if ip != "" {
						clients[ip]++
					}
				default:
					allowed = false
				}
			}
			clientsMu.Unlock()
			if allowed {
				defer func() {
					if ip != "" {
						clientsMu.Lock()
						if clients[ip]--; clients[ip] == 0 {
							delete(clients, ip)
						}
						clientsMu.Unlock()
					}
					<-slots
				}()
			} else {
				w.Header().Set("Retry-After", "5")
				if strings.HasPrefix(r.URL.Path, "/api/portal/") {
					portalError(w, http.StatusServiceUnavailable, "busy")
					return
				}
				a.problem(w, r, http.StatusServiceUnavailable, "系統目前忙碌，請稍候再送出；輸入內容仍保留在表單中。")
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
	branded := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, brand := a.site(r)
		limited.ServeHTTP(w, r.WithContext(web.WithBrand(r.Context(), brand)))
	})
	return i18n.Middleware(SecurityHeaders(branded, authOrigin))
}

// site resolves the origin and brand for the request's host. Only configured
// hostnames get their own origin; any other host falls back to the primary one.
func (a *App) site(r *http.Request) (origin, brand string) {
	host := strings.ToLower(r.Host)
	if brand, ok := a.BrandHosts[host]; ok {
		return "https://" + host, brand
	}
	return a.Origin, web.DefaultBrand
}
func SecurityHeaders(next http.Handler, authOrigins ...string) http.Handler {
	formAction := "form-action 'self'"
	for _, origin := range authOrigins {
		if origin != "" {
			formAction += " " + origin
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self' 'wasm-unsafe-eval'; worker-src 'self'; style-src 'self'; img-src 'self' blob:; font-src 'self'; connect-src 'self' blob:; "+formAction+"; base-uri 'none'; frame-ancestors 'none'; object-src 'none'")
		w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// Chromium sends Origin: null for same-origin POSTs under no-referrer.
		// Keep cross-site referrers private while allowing CSRF Origin checks.
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Add("Vary", "HX-Request")
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=3600")
		}
		next.ServeHTTP(w, r)
	})
}
func base(r *http.Request) model.Page {
	s := current(r)
	return model.Page{User: s.User, CSRF: s.CSRF, Submission: token(), Page: 1, MessagePage: 1, Admin: strings.HasPrefix(r.URL.Path, "/admin")}
}
func pageNum(s string) int {
	v, err := strconv.Atoi(s)
	if err != nil || v < 1 {
		return 1
	}
	return min(v, 10000)
}
func idFrom(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}
func valid(s string, values ...string) bool {
	for _, v := range values {
		if s == v {
			return true
		}
	}
	return false
}
func validBody(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsRune(s, 0) && len([]rune(s)) > 0 && len([]rune(s)) <= 20000
}
func (a *App) render(w http.ResponseWriter, r *http.Request, p model.Page) {
	var buf bytes.Buffer
	var err error
	if p.User.ID > 0 && r.Header.Get("HX-Target") != "conversation-activity" && r.Header.Get("HX-Target") != "ticket-sidebar" {
		p.Announcements, err = a.Store.VisibleAnnouncements(r.Context())
		if err != nil {
			a.fail(w, r, err)
			return
		}
	}
	if p.Active == "detail" && !p.Admin && p.Ticket.Status != "closed" && p.Ticket.Status != "solved" && r.Header.Get("HX-Target") != "conversation-activity" {
		p.ServiceNotice, err = a.Store.ServiceNotice(r.Context(), time.Now())
		if err != nil {
			a.fail(w, r, err)
			return
		}
	}
	if p.User.ID > 0 && r.Header.Get("HX-Request") != "true" {
		p.UnreadNotifications, err = a.Store.UnreadNotifications(r.Context(), p.User.ID)
		if err != nil {
			a.fail(w, r, err)
			return
		}
	}
	if r.Header.Get("HX-Request") == "true" {
		err = web.Workspace(p).Render(r.Context(), &buf)
	} else {
		err = web.Document(p).Render(r.Context(), &buf)
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(buf.Bytes())
}
func (a *App) problem(w http.ResponseWriter, r *http.Request, status int, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	web.ErrorPage(status, i18n.T(r.Context(), message)).Render(r.Context(), w)
}
func (a *App) language(w http.ResponseWriter, r *http.Request) {
	locale := r.PostFormValue("language")
	if locale != "en" && locale != "zh-Hant" {
		a.problem(w, r, 422, "語言選項無效。")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: i18n.CookieName, Value: locale, Path: "/", MaxAge: 365 * 86400, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/account", http.StatusSeeOther)
}
func (a *App) fail(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, database.ErrConflict) {
		a.problem(w, r, 409, "客服單已被更新，或這份表單已送出。請重新載入客服單後再操作。")
		return
	}
	if errors.Is(err, database.ErrCommitUnknown) {
		a.problem(w, r, 503, "暫時無法確認儲存結果，請以相同內容重試，避免重複建立。")
		return
	}
	if errors.Is(err, database.ErrNotFound) {
		a.problem(w, r, 404, "找不到此客服單或附件。")
		return
	}
	if errors.Is(err, database.ErrClosed) {
		a.problem(w, r, 409, "客服單已結案，請先重新開啟再回覆。")
		return
	}
	if errors.Is(err, database.ErrInvalid) {
		a.problem(w, r, 422, "請檢查表單選項後重試。")
		return
	}
	// Avoid logging SQL error details or request bodies, which may contain ticket content.
	var dbErr *pgconn.PgError
	if errors.As(err, &dbErr) {
		a.Log.Error("database operation failed", "code", dbErr.Code)
	} else {
		a.Log.Error("request failed", "error_type", fmt.Sprintf("%T", err))
	}
	a.problem(w, r, 500, "暫時無法完成操作，請稍後再試。")
}
func (a *App) list(w http.ResponseWriter, r *http.Request) {
	p := base(r)
	p.Title = "我的客服單"
	if p.Admin {
		p.Title = "客服工作台"
	}
	p.Active = "list"
	p.Page = pageNum(r.URL.Query().Get("page"))
	p.Filter = r.URL.Query().Get("status")
	p.Phase = r.URL.Query().Get("phase")
	if p.Phase == "" {
		p.Phase = "active"
		if p.Filter == "solved" || p.Filter == "closed" {
			p.Phase = "done"
		}
	}
	p.PriorityFilter = r.URL.Query().Get("priority")
	p.Assignment = r.URL.Query().Get("assigned")
	p.Sort = r.URL.Query().Get("sort")
	p.Query = strings.TrimSpace(r.URL.Query().Get("q"))
	validFilter := valid(p.Filter, "", "new", "open", "waiting", "solved", "closed")
	if p.Admin {
		validFilter = validFilter || p.Filter == "on_hold"
	}
	if !validFilter || !valid(p.Phase, "active", "done") || (p.Filter != "" && (p.Phase == "done") != (p.Filter == "solved" || p.Filter == "closed")) || !valid(p.PriorityFilter, "", "normal", "urgent") || !valid(p.Assignment, "", "mine", "unassigned") || !valid(p.Sort, "", "updated_desc", "updated_asc", "created_desc", "created_asc") || (!p.Admin && (p.PriorityFilter != "" || p.Assignment != "")) || !utf8.ValidString(p.Query) || strings.ContainsRune(p.Query, 0) || len([]rune(p.Query)) > 100 {
		a.problem(w, r, 400, "搜尋條件無效。")
		return
	}
	var err error
	p.Tickets, p.Counts, p.HasNext, err = a.Store.List(r.Context(), p.User, p.Admin, p.Filter, p.PriorityFilter, p.Query, p.Page, p.Assignment, p.Sort, p.Phase)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.render(w, r, p)
}
func (a *App) detail(w http.ResponseWriter, r *http.Request) {
	p := base(r)
	id, ok := idFrom(r)
	if !ok {
		a.problem(w, r, 404, "找不到客服單。")
		return
	}
	p.Active = "detail"
	var err error
	p.Ticket, err = a.Store.Ticket(r.Context(), p.User, id, p.Admin)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	p.Title = p.Ticket.Subject
	p.MessagePage = pageNum(r.URL.Query().Get("messages"))
	p.Messages, p.HasMoreMessages, err = a.Store.Messages(r.Context(), p.User, id, p.Admin, p.MessagePage)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	p.Invites, err = a.Store.Invites(r.Context(), p.User, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if p.Admin {
		p.Events, err = a.Store.Events(r.Context(), p.User, id)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		p.Staff, err = a.Store.Staff(r.Context())
		if err != nil {
			a.fail(w, r, err)
			return
		}
	}
	a.render(w, r, p)
}
func (a *App) create(w http.ResponseWriter, r *http.Request) {
	u := current(r).User
	t := model.Ticket{Subject: strings.TrimSpace(r.PostFormValue("subject")), Category: r.PostFormValue("category"), Priority: r.PostFormValue("priority")}
	if a.AIEnabled {
		t.AssistanceMode = "ai"
	}
	body := strings.TrimSpace(r.PostFormValue("body"))
	if !validBody(t.Subject) || len([]rune(t.Subject)) > 160 || !validBody(body) || !valid(t.Category, "other", "billing", "technical") || !valid(t.Priority, "normal", "urgent") {
		a.createError(w, r, t, body, "請填寫主旨（最多 160 字）、描述（最多 20,000 字）並選擇有效類別及優先級。")
		return
	}
	if !submissionKey.MatchString(r.PostFormValue("submission")) {
		a.problem(w, r, 400, "表單已失效，請重新載入後送出。")
		return
	}
	rawParticipants := r.PostFormValue("participants")
	participants, err := database.NormalizeInviteEmails(strings.Split(rawParticipants, ","), u.Email)
	if err != nil || len(rawParticipants) > 5120 {
		a.createError(w, r, t, body, "請輸入有效的參與者 Email，最多 20 位，且不可邀請自己。")
		return
	}
	files, err := a.uploads(r)
	if err != nil {
		a.createError(w, r, t, body, i18n.T(r.Context(), err.Error())+" "+i18n.T(r.Context(), "附件請重新選擇。"))
		return
	}
	id, err := a.Store.CreateWithParticipants(r.Context(), u, t, body, files, participants, operation(r, struct {
		Ticket       model.Ticket
		Body         string
		Participants []string `json:",omitempty"`
	}{t, body, participants}, files))
	if err != nil {
		if !errors.Is(err, database.ErrCommitUnknown) {
			a.removeFiles(files)
		}
		if !errors.Is(err, database.ErrReplayed) {
			a.fail(w, r, err)
			return
		}
	}
	http.Redirect(w, r, fmt.Sprintf("/tickets/%d", id), 303)
}
func (a *App) reply(w http.ResponseWriter, r *http.Request) {
	p := base(r)
	id, ok := idFrom(r)
	if !ok {
		a.problem(w, r, 404, "找不到客服單。")
		return
	}
	if _, err := a.Store.Ticket(r.Context(), p.User, id, p.Admin); err != nil {
		a.fail(w, r, err)
		return
	}
	rawBody := strings.TrimSpace(r.PostFormValue("body"))
	internal := r.PostFormValue("internal") == "true"
	if !validBody(rawBody) {
		a.replyError(w, r, "回覆需介於 1 至 20,000 字。", nil)
		return
	}
	body := rawBody
	if !internal && p.User.Role == "admin" && p.User.Signature != "" {
		body += "\n\n" + p.User.Signature
		if !validBody(body) {
			a.replyError(w, r, "回覆與簽名檔合計不可超過 20,000 字。", nil)
			return
		}
	}
	if internal && !p.Admin {
		a.problem(w, r, 403, "無法新增內部備註。")
		return
	}
	if !submissionKey.MatchString(r.PostFormValue("submission")) {
		a.problem(w, r, 400, "表單已失效，請重新載入後送出。")
		return
	}
	files, err := a.uploads(r)
	if err != nil {
		a.replyError(w, r, err.Error(), nil)
		return
	}
	err = a.Store.Reply(r.Context(), p.User, id, p.Admin, body, internal, files, operation(r, struct {
		Body     string
		Internal bool
	}{body, internal}, files))
	if err != nil {
		if !errors.Is(err, database.ErrCommitUnknown) {
			a.removeFiles(files)
		}
		if !errors.Is(err, database.ErrReplayed) {
			a.replyError(w, r, err.Error(), err)
			return
		}
	}
	a.afterMutation(w, r, id, p.Admin)
}
func (a *App) handoff(w http.ResponseWriter, r *http.Request) {
	id, ok := idFrom(r)
	if !ok {
		a.problem(w, r, 404, "找不到客服單。")
		return
	}
	if err := a.Store.RequestHuman(r.Context(), current(r).User, id); err != nil {
		a.fail(w, r, err)
		return
	}
	a.afterMutation(w, r, id, false)
}

func (a *App) update(w http.ResponseWriter, r *http.Request) {
	p := base(r)
	id, ok := idFrom(r)
	if !ok {
		a.problem(w, r, 404, "找不到客服單。")
		return
	}
	status, priority := r.PostFormValue("status"), r.PostFormValue("priority")
	team := []int64{}
	validStatus := valid(status, "new", "open", "closed")
	if p.Admin {
		validStatus = valid(status, "new", "open", "waiting", "on_hold", "solved", "closed")
	}
	if !validStatus || (p.Admin && !valid(priority, "normal", "urgent")) {
		a.problem(w, r, 422, "請選擇有效狀態與優先級。")
		return
	}
	if p.Admin {
		selected := append([]string(nil), r.PostForm["assignees"]...)
		if len(selected) == 0 && r.PostFormValue("assignee") != "" {
			selected = append(selected, r.PostFormValue("assignee"))
		}
		switch r.PostFormValue("assignee_action") {
		case "":
		case "add_self":
			alreadySelected := false
			for _, raw := range selected {
				if raw == "self" || raw == strconv.FormatInt(p.User.ID, 10) {
					alreadySelected = true
					break
				}
			}
			if !alreadySelected {
				selected = append(selected, "self")
			}
		case "remove_self":
			withoutSelf := selected[:0]
			for _, raw := range selected {
				if raw != "self" && raw != strconv.FormatInt(p.User.ID, 10) {
					withoutSelf = append(withoutSelf, raw)
				}
			}
			selected = withoutSelf
		default:
			a.problem(w, r, 422, "無效的指派操作。")
			return
		}
		for _, raw := range selected {
			if raw == "self" {
				team = append(team, p.User.ID)
				continue
			}
			v, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || v < 1 {
				a.problem(w, r, 422, "請選擇有效的客服。")
				return
			}
			team = append(team, v)
		}
	}
	version, err := strconv.ParseInt(r.PostFormValue("version"), 10, 64)
	if err != nil || version < 1 {
		a.problem(w, r, 400, "表單已失效，請重新載入客服單。")
		return
	}
	if err := a.Store.UpdateTeamCategory(r.Context(), p.User, id, p.Admin, status, priority, r.PostFormValue("category"), team, version); err != nil {
		a.fail(w, r, err)
		return
	}
	a.afterMutation(w, r, id, p.Admin)
}

func (a *App) assign(w http.ResponseWriter, r *http.Request) {
	p := base(r)
	id, ok := idFrom(r)
	if !ok || !p.Admin {
		a.problem(w, r, 404, "找不到客服單。")
		return
	}
	personID, err := strconv.ParseInt(r.PostFormValue("assignee_id"), 10, 64)
	if err != nil || personID < 1 {
		a.problem(w, r, 422, "請選擇有效的客服。")
		return
	}
	version, err := strconv.ParseInt(r.PostFormValue("version"), 10, 64)
	if err != nil || version < 1 {
		a.problem(w, r, 400, "表單已失效，請重新載入客服單。")
		return
	}
	ticket, err := a.Store.Ticket(r.Context(), p.User, id, true)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	team := make([]int64, 0, len(ticket.Team)+1)
	assigned := false
	for _, member := range ticket.Team {
		if member.ID == personID {
			assigned = true
			continue
		}
		team = append(team, member.ID)
	}
	if !assigned {
		team = append(team, personID)
	}
	if err = a.Store.UpdateTeam(r.Context(), p.User, id, true, ticket.Status, ticket.Priority, team, version); err != nil {
		a.fail(w, r, err)
		return
	}
	a.afterMutation(w, r, id, true)
}
func (a *App) afterMutation(w http.ResponseWriter, r *http.Request, id int64, admin bool) {
	prefix := "/tickets/"
	if admin {
		prefix = "/admin/tickets/"
	}
	target := prefix + strconv.FormatInt(id, 10)
	if r.Header.Get("HX-Request") == "true" {
		r.URL.RawQuery = ""
		w.Header().Set("HX-Replace-Url", target)
		a.detail(w, r)
	} else {
		http.Redirect(w, r, target, 303)
	}
}

// Bounded login rate limits prevent unauthenticated database growth; the
// per-client limit keeps one source from exhausting the global allowance.
func (a *App) loginLimiter(next http.HandlerFunc) http.HandlerFunc {
	var mu sync.Mutex
	var start time.Time
	count := 0
	clients := map[string]int{}
	return func(w http.ResponseWriter, r *http.Request) {
		ip := ClientRequestInfo(r).IP
		mu.Lock()
		if time.Since(start) > time.Minute {
			start = time.Now()
			count = 0
			clear(clients)
		}
		allowed := count < 600 && clients[ip] < 5
		if allowed {
			count++
			clients[ip]++
		}
		mu.Unlock()
		if !allowed {
			w.Header().Set("Retry-After", "60")
			a.problem(w, r, 429, "登入請求過多，請稍候再試。")
			return
		}
		next(w, r)
	}
}
func (a *App) Cleanup(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			_, e1 := a.Store.Pool.Exec(cleanupCtx, `DELETE FROM sessions WHERE expires_at<=now()`)
			_, e2 := a.Store.Pool.Exec(cleanupCtx, `DELETE FROM login_flows WHERE expires_at<=now()`)
			_, e3 := a.Store.Pool.Exec(cleanupCtx, `UPDATE email_notifications SET state='cancelled',lease_until=NULL WHERE state='pending' AND created_at<now()-interval '7 days'`)
			_, e4 := a.Store.Pool.Exec(cleanupCtx, `DELETE FROM email_notifications WHERE state IN ('sent','cancelled','failed') AND created_at<now()-interval '30 days'`)
			cancel()
			if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
				a.Log.Error("expired record cleanup failed")
			}
		}
	}
}

func (a *App) createError(w http.ResponseWriter, r *http.Request, t model.Ticket, body, message string) {
	p := base(r)
	p.Active = "new"
	p.Title = "建立客服單"
	p.AIEnabled = a.AIEnabled
	p.Ticket = t
	p.DraftBody = body
	p.DraftParticipants = r.PostFormValue("participants")
	p.Error = i18n.T(r.Context(), message)
	if submissionKey.MatchString(r.PostFormValue("submission")) {
		p.Submission = r.PostFormValue("submission")
	}
	var buf bytes.Buffer
	if err := web.Document(p).Render(r.Context(), &buf); err != nil {
		a.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(422)
	w.Write(buf.Bytes())
}

// Native form failures retain the reply. HTMX leaves the existing form intact.
func (a *App) replyError(w http.ResponseWriter, r *http.Request, message string, cause error) {
	status := 422
	if cause != nil {
		switch {
		case errors.Is(cause, database.ErrConflict):
			status = 409
			message = "客服單已更新，請重新載入後再試。"
		case errors.Is(cause, database.ErrClosed):
			status = 409
			message = "客服單已結案，請重新開啟後再回覆。"
		case errors.Is(cause, database.ErrNotFound):
			a.fail(w, r, cause)
			return
		default:
			a.fail(w, r, cause)
			return
		}
	}
	if r.Header.Get("HX-Request") == "true" {
		a.problem(w, r, status, message)
		return
	}
	p := base(r)
	id, ok := idFrom(r)
	if !ok {
		a.problem(w, r, 404, "找不到客服單。")
		return
	}
	var err error
	p.Ticket, err = a.Store.Ticket(r.Context(), p.User, id, p.Admin)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	p.Messages, p.HasMoreMessages, err = a.Store.Messages(r.Context(), p.User, id, p.Admin, 1)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if p.Admin {
		p.Staff, err = a.Store.Staff(r.Context())
		if err != nil {
			a.fail(w, r, err)
			return
		}
	}
	p.Active = "detail"
	p.Title = p.Ticket.Subject
	p.Error = i18n.T(r.Context(), message) + " " + i18n.T(r.Context(), "附件請重新選擇。")
	p.DraftBody = r.PostFormValue("body")
	p.DraftInternal = r.PostFormValue("internal") == "true"
	p.Submission = r.PostFormValue("submission")
	var buf bytes.Buffer
	if err = web.Document(p).Render(r.Context(), &buf); err != nil {
		a.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}

// BrandHostsFromEnvironment parses BRAND_HOSTS, e.g. "support.vproxy.cloud=vproxy".
func BrandHostsFromEnvironment(value, primaryHost string) (map[string]string, error) {
	hosts := map[string]string{}
	for _, entry := range strings.Split(value, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		host, brand, ok := strings.Cut(entry, "=")
		if !ok || !portalHost.MatchString(host) || strings.Contains(host, "..") || host == primaryHost || !web.KnownBrand(brand) {
			return nil, errors.New("BRAND_HOSTS must list hostname=brand pairs with a known brand")
		}
		hosts[host] = brand
	}
	return hosts, nil
}
