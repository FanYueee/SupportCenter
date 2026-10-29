// Read-only, loopback-only UI preview. This binary is not included in the production image.
package main

import (
	"crypto/rand"
	"crypto/subtle"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"supportcenter/internal/ai"
	"supportcenter/internal/app"
	"supportcenter/internal/i18n"
	"supportcenter/internal/model"
	"supportcenter/web"
)

func main() {
	mux := http.NewServeMux()
	mux.Handle("GET /assets/", app.AssetHandler())
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		p := fixture()
		p.CSRF = rand.Text()
		if existing, err := r.Cookie("support-preview-language-csrf"); err == nil && len(existing.Value) == len(p.CSRF) {
			p.CSRF = existing.Value
		}
		http.SetCookie(w, &http.Cookie{Name: "support-preview-language-csrf", Value: p.CSRF, Path: "/", MaxAge: 600, Secure: false, HttpOnly: true, SameSite: http.SameSiteLaxMode})
		p.Admin = strings.HasPrefix(r.URL.Path, "/admin")
		if p.Admin {
			p.User.ID = 2
			p.User.Role = "admin"
			p.Title = "客服工作台"
			if r.URL.Query().Get("only_self") == "1" {
				p.Staff = p.Staff[:1]
			}
		}
		if role := r.URL.Query().Get("role"); role == "support" || role == "admin" {
			p.User.Role = role
		}
		if r.URL.Path == "/account" || r.URL.Path == "/admin/account" {
			p.Active, p.Title = "account", "個人設定"
			p.Logins = []model.Activity{{Kind: "login", Detail: "Chrome · Windows", IP: "203.0.113.10", UserAgent: "Mozilla/5.0 (Windows NT 10.0) AppleWebKit/537.36 Chrome/120.0", CreatedAt: time.Now().UTC()}}
			p.Activities = []model.Activity{{Kind: "created", TicketID: 1048, IP: "203.0.113.10", UserAgent: "Mozilla/5.0 (Windows NT 10.0) AppleWebKit/537.36 Chrome/120.0", CreatedAt: time.Now().UTC()}}
		}
		if r.URL.Path == "/admin/users" {
			p.Active, p.Title = "users", "使用者"
			last := time.Now().UTC()
			p.Users = []model.UserSummary{{User: model.User{ID: 2, Name: "Jamie", Email: "jamie@example.com", Role: "admin"}, LastLogin: &last}, {User: p.User, LastLogin: &last}}
		}
		if r.URL.Path == "/admin/automation" {
			p.Active, p.Title = "automation", "自動回覆"
			days := make([]model.DaySchedule, 7)
			for i := 0; i < 5; i++ {
				days[i] = model.DaySchedule{Enabled: true, Ranges: []model.TimeRange{{Start: "09:00", End: "18:00"}}}
			}
			days[0].Ranges = []model.TimeRange{{Start: "09:00", End: "12:00"}, {Start: "13:00", End: "18:00"}}
			p.Automation = model.AutomationSettings{Timezone: "Asia/Taipei", Days: days, AutoCloseDays: 7}
		}
		if r.URL.Path == "/notifications" {
			p.Active, p.Title = "notifications", "通知"
			due := time.Now().UTC().Add(7 * 24 * time.Hour)
			p.Notifications = []model.Notification{{ID: 2, TicketID: 1045, Kind: "auto_closed", Subject: "新增 IP 白名單的設定方式", CreatedAt: time.Now().UTC()}, {ID: 1, TicketID: 1044, Kind: "solved", Subject: "申請本月帳單與付款收據", CreatedAt: time.Now().UTC(), AutoCloseAt: &due}}
			if r.URL.Query().Get("closure") == "manual" {
				p.Notifications[0].Kind = "closed"
				p.Notifications[0].TicketID = p.Notifications[1].TicketID
				p.Notifications[0].Subject = p.Notifications[1].Subject
				p.Notifications[1].AutoCloseAt = nil
			}
		}
		if r.URL.Path == "/admin/ai" {
			p.Active, p.Title = "ai", "AI 指令"
			p.AIEnabled = true
			p.AIModel = ai.DefaultModel
			p.AIPrompts = model.AIPrompts{General: "請先確認客戶的需求，再給出簡短步驟。", Billing: "帳務問題請先核對訂單編號，無法查證時交由真人客服。", Technical: "技術問題請先請客戶提供發生時間與錯誤訊息。"}
		}
		if r.URL.Path == "/admin/discord" {
			p.Active, p.Title = "discord", "Discord 通知"
			sent := time.Now().UTC().Add(-3 * time.Minute)
			p.Discord = model.DiscordSettings{WebhookID: "1234567890123456789", OnTicket: true, OnReply: true, IncludePreview: true, LastSentAt: &sent}
			switch r.URL.Query().Get("state") {
			case "empty":
				p.Discord = model.DiscordSettings{OnTicket: true, OnReply: true, IncludePreview: true}
			case "error":
				failed := time.Now().UTC()
				p.Discord.LastError, p.Discord.LastErrorAt, p.DiscordTest = "not_found", &failed, "failed"
			}
		}
		if r.URL.Path == "/admin/departments" {
			p.Active, p.Title = "departments", "客服分組"
		}
		if r.URL.Query().Get("announcements") == "1" {
			p.Announcements = announcementFixture()[:3]
			if r.URL.Query().Get("long") == "1" {
				p.Announcements[0].Title = strings.Repeat("Maintenance", 10)
				p.Announcements[0].Body = "https://example.com/" + strings.Repeat("long", 100) + "\nSecond line"
			}
		}
		if r.URL.Path == "/admin/announcements" || strings.HasPrefix(r.URL.Path, "/admin/announcements/") {
			p.Active, p.Title = "announcements", "公告"
			p.Saved = r.URL.Query().Get("saved") == "1"
			p.ManagedAnnouncements = announcementFixture()
			p.Announcements = p.ManagedAnnouncements[:3]
			p.Announcement = model.Announcement{Color: "blue", Enabled: true}
			if id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/admin/announcements/"), 10, 64); err == nil {
				for _, item := range p.ManagedAnnouncements {
					if item.ID == id {
						p.Announcement = item
					}
				}
			}
		}
		p.Query = r.URL.Query().Get("q")
		p.Filter = r.URL.Query().Get("status")
		p.Phase = r.URL.Query().Get("phase")
		if p.Phase == "" {
			p.Phase = "active"
			if p.Filter == "solved" || p.Filter == "closed" {
				p.Phase = "done"
			}
		}
		p.Sort = r.URL.Query().Get("sort")
		if p.Admin {
			p.PriorityFilter = r.URL.Query().Get("priority")
			p.Assignment = r.URL.Query().Get("assigned")
		}
		p.Counts = model.Counts{}
		filtered := []model.Ticket{}
		for _, t := range p.Tickets {
			if p.PriorityFilter != "" && t.Priority != p.PriorityFilter {
				continue
			}
			if p.Assignment == "mine" && (t.AssigneeID == nil || *t.AssigneeID != p.User.ID) {
				continue
			}
			if p.Assignment == "unassigned" && t.AssigneeID != nil {
				continue
			}
			matches := strings.Contains(strings.ToLower(t.Subject), strings.ToLower(p.Query))
			number := strings.TrimPrefix(p.Query, "#")
			if number != "" && number[0] >= '0' && number[0] <= '9' {
				if id, err := strconv.ParseInt(number, 10, 64); err == nil && id > 0 {
					matches = t.ID == id
				}
			}
			if !matches {
				continue
			}
			p.Counts.All++
			switch t.Status {
			case "new":
				p.Counts.New++
			case "open":
				p.Counts.Open++
			case "waiting":
				p.Counts.Waiting++
			case "on_hold":
				p.Counts.Hold++
			case "solved":
				p.Counts.Solved++
			case "closed":
				p.Counts.Closed++
			}
			phaseMatch := (p.Phase == "done") == (t.Status == "solved" || t.Status == "closed")
			if phaseMatch && (p.Filter == "" || t.Status == p.Filter || (!p.Admin && p.Filter == "open" && t.Status == "on_hold")) {
				filtered = append(filtered, t)
			}
		}
		if !p.Admin {
			p.Counts.Open += p.Counts.Hold
		}
		p.Tickets = filtered
		switch p.Sort {
		case "updated_asc":
			sort.SliceStable(p.Tickets, func(i, j int) bool { return p.Tickets[i].UpdatedAt.Before(p.Tickets[j].UpdatedAt) })
		case "created_desc":
			sort.SliceStable(p.Tickets, func(i, j int) bool { return p.Tickets[i].CreatedAt.After(p.Tickets[j].CreatedAt) })
		case "created_asc":
			sort.SliceStable(p.Tickets, func(i, j int) bool { return p.Tickets[i].CreatedAt.Before(p.Tickets[j].CreatedAt) })
		}
		if r.URL.Path == "/tickets/new" {
			p.Active = "new"
			p.Title = "建立客服單"
			p.Ticket = model.Ticket{}
		} else if r.URL.Path == "/login" {
			p.Active = "login"
			p.Title = "登入"
		} else if strings.HasPrefix(r.URL.Path, "/tickets/") || strings.HasPrefix(r.URL.Path, "/admin/tickets/") {
			p.Active = "detail"
			p.Title = p.Ticket.Subject
			parts := strings.Split(r.URL.Path, "/")
			if id, err := strconv.ParseInt(parts[len(parts)-1], 10, 64); err == nil {
				for _, t := range fixture().Tickets {
					if t.ID == id {
						p.Ticket = t
						p.Title = t.Subject
					}
				}
			}
		}
		if p.Active == "detail" && r.URL.Query().Get("participants") != "" {
			member := r.URL.Query().Get("participants") == "member"
			p.Invites = []model.Participant{{Email: "taylor@example.com", Self: member}, {Email: "future.participant@example.com"}}
			if member {
				p.User = model.User{ID: 3, Name: "Taylor Lee", Email: "taylor@example.com", Role: "user"}
				p.Admin = false
			}
		}
		if p.Active == "detail" && r.URL.Query().Get("ai") != "" {
			p.Ticket.AssistanceMode, p.Ticket.Version = "ai", 1
			switch r.URL.Query().Get("ai") {
			case "processing":
				p.Ticket.Status = "open"
			case "replied":
				p.Ticket.Status = "waiting"
				p.Messages = append(p.Messages, model.Message{ID: 3, AuthorID: 3, Author: "AI 助理", Role: "user", Source: "ai", Body: "請確認連線是否恢復。", CreatedAt: time.Now().UTC()})
			case "solved":
				p.Ticket.Status, p.Ticket.Version = "solved", 2
				at := time.Now().UTC()
				due := at.Add(7 * 24 * time.Hour)
				p.Ticket.SolvedAt, p.Ticket.AutoCloseAt = &at, &due
			case "human":
				p.Ticket.AssistanceMode, p.Ticket.Status = "human", "open"
				at := time.Now().UTC()
				p.Ticket.HandoffAt = &at
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if p.Active == "detail" && !p.Admin && p.Ticket.Status != "solved" && p.Ticket.Status != "closed" {
			switch r.URL.Query().Get("service") {
			case "off_hours":
				p.ServiceNotice = &model.ServiceNotice{Kind: "off_hours", Body: "目前不在服務時間內，我們會在恢復服務後回覆。"}
			case "leave":
				p.ServiceNotice = &model.ServiceNotice{Kind: "leave", Body: "今天臨時休假，明天恢復服務。"}
			}
		}
		if r.Header.Get("HX-Request") == "true" {
			web.Workspace(p).Render(r.Context(), w)
		} else {
			web.Document(p).Render(r.Context(), w)
		}
	})
	mux.HandleFunc("POST /language", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		csrf, err := r.Cookie("support-preview-language-csrf")
		if err != nil || subtle.ConstantTimeCompare([]byte(csrf.Value), []byte(r.PostFormValue("csrf"))) != 1 || csrf.Value == "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		locale := r.PostFormValue("language")
		if locale != "en" && locale != "zh-Hant" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: i18n.PreviewCookieName, Value: locale, Path: "/", MaxAge: 365 * 86400, Secure: false, HttpOnly: true, SameSite: http.SameSiteLaxMode})
		http.Redirect(w, r, "/account", http.StatusSeeOther)
	})
	mux.HandleFunc("POST /", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusMethodNotAllowed)
		web.ErrorPage(405, i18n.T(r.Context(), "這是唯讀預覽，操作不會儲存。")).Render(r.Context(), w)
	})
	log.Println("Read-only UI preview: http://127.0.0.1:8081")
	// ?brand=vproxy previews another hostname's logos.
	branded := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		brand := web.DefaultBrand
		if b := r.URL.Query().Get("brand"); web.KnownBrand(b) {
			brand = b
		}
		mux.ServeHTTP(w, r.WithContext(web.WithBrand(r.Context(), brand)))
	})
	log.Fatal(http.ListenAndServe("127.0.0.1:8081", i18n.Middleware(app.SecurityHeaders(branded))))
}

func announcementFixture() []model.Announcement {
	return []model.Announcement{
		{ID: 3, Title: "服務維護通知", Body: "10 月 2 日 02:00–03:00 進行網路維護。\n維護期間可能短暫中斷連線。", Color: "blue", Enabled: true, Version: 1},
		{ID: 2, Title: "服務已恢復", Body: "連線服務已恢復正常，感謝您的耐心等候。", Color: "green", Enabled: true, Version: 1},
		{ID: 1, Title: "重要提醒", Body: "請勿在客服單中提供帳號密碼或付款卡號。", Color: "red", Enabled: true, Version: 1},
		{ID: 4, Title: "尚未發布的公告", Body: "僅供管理員檢視。", Color: "blue", Enabled: false, Version: 1},
	}
}
func fixture() model.Page {
	now := time.Date(2026, 9, 28, 9, 42, 0, 0, time.UTC)
	p := model.Page{User: model.User{ID: 1, Name: "Alex Chen", Email: "alex@example.com", Role: "user"}, Title: "我的客服單", Active: "list", Page: 1, MessagePage: 1, Preview: true, CSRF: "preview-only", UnreadNotifications: 2}
	subjects := []string{"代理連線出現逾時，想確認節點狀態", "希望了解流量方案的升級方式", "付款完成後，服務尚未開通", "新增 IP 白名單的設定方式", "申請本月帳單與付款收據", "如何更新帳號的聯絡資訊"}
	statuses := []string{"open", "waiting", "new", "closed", "solved", "on_hold"}
	categories := []string{"technical", "billing", "billing", "technical", "billing", "account"}
	priorities := []string{"normal", "normal", "normal", "normal", "normal", "urgent"}
	for i, s := range subjects {
		ticket := model.Ticket{ID: int64(1048 - i), UserID: 1, Subject: s, Status: statuses[i], Category: categories[i], Priority: priorities[i], Owner: "Alex Chen", CreatedAt: now.Add(-time.Duration(48+i*24) * time.Hour), UpdatedAt: now.Add(-time.Duration(i*7) * time.Hour)}
		if ticket.Status == "solved" {
			at := time.Now().UTC()
			due := at.Add(7 * 24 * time.Hour)
			ticket.SolvedAt = &at
			ticket.AutoCloseAt = &due
		}
		if ticket.Status == "closed" {
			at := time.Now().UTC().Add(-time.Hour)
			ticket.AutoClosedAt = &at
		}
		if i == 0 || i == 1 || i == 4 {
			id := int64(2)
			ticket.AssigneeID = &id
			ticket.Assignee = "Jamie"
			ticket.Team = []model.User{{ID: 2, Name: "Jamie", Role: "support"}}
		}
		p.Tickets = append(p.Tickets, ticket)
	}
	p.Ticket = p.Tickets[0]
	p.Messages = []model.Message{{ID: 1, Author: "Alex Chen", Role: "user", Body: "你好，今天早上使用代理服務時，偶爾會出現連線逾時。\n\n已經嘗試重新連線及更換本機網路，但仍有同樣的情況。想請你們協助確認目前的節點狀態，謝謝！", CreatedAt: now.Add(-2 * time.Hour)}, {ID: 2, Author: "Jamie", Role: "support", Body: "你好 Alex，謝謝你提供這些資訊。\n\n我們正在確認相關節點的連線狀態。為了更快定位問題，方便補充受影響的節點地區，以及大約發生的時間嗎？\n\n請直接在這張客服單回覆，我們會持續協助你。", CreatedAt: now.Add(-time.Hour)}}
	p.Staff = []model.User{{ID: 2, Name: "Jamie", Role: "support", Departments: []string{"billing"}}, {ID: 3, Name: "Morgan", Role: "support", Departments: []string{"technical"}}, {ID: 4, Name: "Taylor", Role: "support", Departments: []string{"billing"}}, {ID: 5, Name: "Casey", Role: "support", Departments: []string{"other"}}}
	return p
}
