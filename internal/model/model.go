package model

import (
	"strconv"
	"strings"
	"time"
)

type User struct {
	ID                           int64
	Name, Email, Role, Signature string
	Departments                  []string
}

// Participants are shown only by email, so an invitation never reveals
// whether the address belongs to a registered account. Self marks the
// viewer's own invitation.
type Participant struct {
	Email string
	Self  bool
}

func (u User) Staff() bool { return u.Role == "support" || u.Role == "admin" }

type Ticket struct {
	Version, ID, UserID                                                  int64
	Subject, Category, Status, Priority, Owner, Assignee, AssistanceMode string
	Team                                                                 []User
	AssigneeID                                                           *int64
	HandoffAt                                                            *time.Time
	SolvedAt, AutoCloseAt, AutoClosedAt                                  *time.Time
	CreatedAt, UpdatedAt                                                 time.Time
}
type Attachment struct {
	ID                        int64
	Name, Key, MIME, Checksum string
	Size                      int64
}
type Message struct {
	ID, AuthorID               int64
	Author, Role, Body, Source string
	Internal                   bool
	CreatedAt                  time.Time
	Attachments                []Attachment
}
type Counts struct{ All, New, Open, Waiting, Hold, Solved, Closed int }
type TimeRange struct {
	Start string `json:"start"`
	End   string `json:"end"`
}
type DaySchedule struct {
	Enabled bool        `json:"enabled"`
	Ranges  []TimeRange `json:"ranges"`
}
type AutomationSettings struct {
	AutoCloseDays                                   int
	Timezone, OnCreateBody, OffHoursBody, LeaveBody string
	Days                                            []DaySchedule
	LeaveStartAt, LeaveEndAt                        *time.Time
	OnCreateEnabled, OffHoursEnabled, LeaveEnabled  bool
}
type Notification struct {
	ID, TicketID        int64
	Kind, Subject       string
	CreatedAt           time.Time
	ReadAt, AutoCloseAt *time.Time
}
type ServiceNotice struct {
	Kind, Body string
}
type AIPrompts struct {
	General, Billing, Technical string
}

// DiscordSettings never carries the webhook token, so a page cannot leak it.
// LastError is a short code (see web.discordError), not Discord's response.
type DiscordSettings struct {
	WebhookID                         string
	OnTicket, OnReply, IncludePreview bool
	LastError                         string
	LastSentAt, LastErrorAt           *time.Time
}

func (d DiscordSettings) Configured() bool { return d.WebhookID != "" }

// EmailSettings controls the emails sent to customers. LastError is a short
// code (see web.mailError), not the SMTP server's reply.
type EmailSettings struct {
	OnReply, IncludeReply, OnInvite bool
	LastError                       string
	LastSentAt, LastErrorAt         *time.Time
}

type Activity struct {
	Kind, Detail, IP, UserAgent string
	TicketID                    int64
	CreatedAt                   time.Time
}
type UserSummary struct {
	User      User
	LastLogin *time.Time
}
type Department struct {
	Category, Name string
}

var Departments = []Department{
	{Category: "other", Name: "一般支援部門"},
	{Category: "billing", Name: "帳務部門"},
	{Category: "technical", Name: "技術部門"},
}

func DepartmentName(category string) string {
	for _, department := range Departments {
		if department.Category == category {
			return department.Name
		}
	}
	return "未分組"
}

func (u User) InDepartment(category string) bool {
	for _, item := range u.Departments {
		if item == category {
			return true
		}
	}
	return false
}

func DeviceLabel(agent string) string {
	browser, platform := "瀏覽器", "未知裝置"
	switch {
	case strings.Contains(agent, "Edg/"):
		browser = "Edge"
	case strings.Contains(agent, "Firefox/"):
		browser = "Firefox"
	case strings.Contains(agent, "Chrome/"):
		browser = "Chrome"
	case strings.Contains(agent, "Safari/"):
		browser = "Safari"
	}
	switch {
	case strings.Contains(agent, "Android"):
		platform = "Android"
	case strings.Contains(agent, "iPhone"), strings.Contains(agent, "iPad"):
		platform = "iOS"
	case strings.Contains(agent, "Windows"):
		platform = "Windows"
	case strings.Contains(agent, "Mac OS"):
		platform = "macOS"
	case strings.Contains(agent, "Linux"):
		platform = "Linux"
	}
	return browser + " · " + platform
}

func ActivityBrowser(a Activity) string {
	if a.UserAgent != "" {
		return DeviceLabel(a.UserAgent)
	}
	if a.Kind == "login" && a.Detail != "" {
		return a.Detail
	}
	return "瀏覽器未記錄"
}

func ActivityIP(a Activity) string {
	if a.IP == "" {
		return "IP 未記錄"
	}
	return "IP " + a.IP
}
func ActivityText(a Activity) string {
	switch a.Kind {
	case "login":
		return "登入成功"
	case "logout":
		return "登出"
	case "avatar":
		if a.Detail == "initial" {
			return "頭像改為姓名首字"
		}
		return "頭像改為 Gravatar"
	case "name":
		return "變更顯示名稱"
	case "signature":
		return "更新簽名檔"
	case "created":
		return "建立客服單"
	case "reply":
		return "回覆客服單"
	case "note":
		return "新增內部備註"
	case "status":
		return "狀態改為「" + Status(a.Detail) + "」"
	case "priority":
		return "優先級改為「" + Priority(a.Detail) + "」"
	case "category":
		return "問題類別改為「" + Category(a.Detail) + "」"
	case "assignment":
		return "變更負責客服"
	case "invite":
		return "邀請使用者加入客服單"
	case "uninvite":
		return "移除受邀使用者"
	case "leave":
		return "退出客服單"
	case "handoff":
		return "轉由真人客服處理"
	}
	return "帳號操作"
}
func RoleLabel(role string) string {
	switch role {
	case "admin":
		return "管理員"
	case "support":
		return "客服"
	default:
		return "使用者"
	}
}

type Announcement struct {
	ID                   int64
	Title, Body, Color   string
	Enabled              bool
	Version              int64
	CreatedAt, UpdatedAt time.Time
}

func (v Announcement) Icon() string {
	switch v.Color {
	case "green":
		return "status-solved"
	case "red":
		return "warning"
	default:
		return "info"
	}
}
func (v Announcement) ColorLabel() string {
	switch v.Color {
	case "green":
		return "完成"
	case "red":
		return "重要"
	default:
		return "資訊"
	}
}

type Page struct {
	Announcements, ManagedAnnouncements                                                                       []Announcement
	Announcement                                                                                              Announcement
	ServiceNotice                                                                                             *ServiceNotice
	Notifications                                                                                             []Notification
	UnreadNotifications                                                                                       int
	User                                                                                                      User
	CSRF, Title, Active, Query, Filter, Phase, PriorityFilter, Error, DraftBody, Submission, Assignment, Sort string
	Tickets                                                                                                   []Ticket
	Events                                                                                                    []Event
	DraftInternal                                                                                             bool
	Ticket                                                                                                    Ticket
	Messages                                                                                                  []Message
	Staff                                                                                                     []User
	Departments                                                                                               []Department
	Counts                                                                                                    Counts
	Activities, Logins                                                                                        []Activity
	Users                                                                                                     []UserSummary
	Invites                                                                                                   []Participant
	DraftParticipants                                                                                         string
	Automation                                                                                                AutomationSettings
	AIPrompts                                                                                                 AIPrompts
	Discord                                                                                                   DiscordSettings
	DiscordTest                                                                                               string
	Email                                                                                                     EmailSettings
	MailFrom, MailTest                                                                                        string
	EmailNotify                                                                                               bool
	ActivityPage, LoginPage                                                                                   int
	HasMoreActivities, HasMoreLogins, Saved                                                                   bool
	Page, MessagePage                                                                                         int
	HasNext, HasMoreMessages, Admin, Preview, AIEnabled, MailEnabled                                          bool
	AIModel                                                                                                   string
}

func Status(s string) string {
	switch s {
	case "new":
		return "尚未受理"
	case "open":
		return "處理中"
	case "waiting":
		return "等待使用者回覆"
	case "on_hold":
		return "等待其他單位回覆"
	case "solved":
		return "已解決"
	case "closed":
		return "已結案"
	}
	return s
}
func StatusForViewer(s string, staff bool) string {
	if staff {
		return Status(s)
	}
	switch s {
	case "new":
		return "待受理"
	case "waiting":
		return "待您回覆"
	case "on_hold":
		return "處理中"
	}
	return Status(s)
}

func VisibleStatus(s string, staff bool) string {
	if !staff && s == "on_hold" {
		return "open"
	}
	return s
}
func Priority(s string) string {
	switch s {
	case "low":
		return "低"
	case "normal":
		return "標準"
	case "high":
		return "高"
	case "urgent":
		return "緊急"
	}
	return s
}
func Category(s string) string {
	switch s {
	case "technical":
		return "技術支援"
	case "billing":
		return "帳務支援"
	case "account", "other":
		return "一般支援"
	}
	return s
}
func Initial(s string) string {
	for _, r := range s {
		return string(r)
	}
	return "U"
}

type Operation struct{ Key, Hash string }
type Event struct {
	Actor, Kind, Previous, Current string
	CreatedAt                      time.Time
}

func EventText(e Event, staff []User) string {
	switch e.Kind {
	case "status":
		return Status(e.Previous) + " → " + Status(e.Current)
	case "priority":
		return "優先級：" + Priority(e.Previous) + " → " + Priority(e.Current)
	case "category":
		return "問題類別：" + Category(e.Previous) + " → " + Category(e.Current)
	case "assignment":
		name := func(id string) string {
			if id == "" {
				return "未指派"
			}
			labels := []string{}
			for _, item := range strings.Split(id, ",") {
				label := "客服 #" + item
				for _, u := range staff {
					if strconv.FormatInt(u.ID, 10) == item {
						label = u.Name
						break
					}
				}
				labels = append(labels, label)
			}
			return strings.Join(labels, "、")
		}
		return "指派：" + name(e.Previous) + " → " + name(e.Current)
	case "handoff":
		return "轉由真人客服處理"
	case "invite":
		return "邀請：" + e.Current
	case "uninvite":
		return "移除邀請：" + e.Previous
	case "leave":
		return "退出客服單"
	}
	return ""
}
