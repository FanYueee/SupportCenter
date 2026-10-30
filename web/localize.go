package web

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"supportcenter/internal/i18n"
	"supportcenter/internal/model"
)

func pageTitle(ctx context.Context, p model.Page) string {
	if p.Active == "detail" {
		return p.Title
	}
	return i18n.T(ctx, p.Title)
}

func pageLabel(ctx context.Context, number int, newest bool) string {
	if i18n.Lang(ctx) == "en" {
		if newest {
			return fmt.Sprintf("Page %d (newest messages on page 1)", number)
		}
		return fmt.Sprintf("Page %d", number)
	}
	if newest {
		return fmt.Sprintf("第 %d 頁（最新訊息在第 1 頁）", number)
	}
	return fmt.Sprintf("第 %d 頁", number)
}

func assignmentLabel(ctx context.Context, ticket model.Ticket, person model.User, self bool) string {
	assigned := teamHas(ticket, person.ID)
	if self {
		if assigned {
			return i18n.T(ctx, "取消指派自己")
		}
		return i18n.T(ctx, "指派自己")
	}
	if assigned {
		return i18n.T(ctx, "取消指派") + " " + person.Name
	}
	return i18n.T(ctx, "指派") + " " + person.Name
}

func authorName(ctx context.Context, message model.Message) string {
	if message.Source == "ai" || message.Source == "automation" {
		return i18n.T(ctx, message.Author)
	}
	return message.Author
}
func notificationLabel(ctx context.Context, count int) string {
	if count == 0 {
		return i18n.T(ctx, "通知")
	}
	if i18n.Lang(ctx) == "en" {
		return fmt.Sprintf("Notifications · %d unread", count)
	}
	return fmt.Sprintf("通知 · %d 則未讀", count)
}
func messageBody(ctx context.Context, message model.Message) string {
	if message.Source == "automation" && message.Body == "此客服單已依設定自動結案。如需繼續討論，請重新開啟客服單。" {
		return i18n.T(ctx, message.Body)
	}
	return message.Body
}

func activityBrowser(ctx context.Context, a model.Activity) string {
	label := model.ActivityBrowser(a)
	parts := strings.Split(label, " · ")
	if len(parts) == 2 {
		return i18n.T(ctx, parts[0]) + " · " + i18n.T(ctx, parts[1])
	}
	return i18n.T(ctx, label)
}

// prioritySupportHint names the priorities that need a priority support
// upgrade, or nothing when every priority is open.
func prioritySupportHint(ctx context.Context, support model.PrioritySupport) string {
	locked := []string{}
	for _, priority := range model.Priorities {
		if !model.Allows(support.Level, priority) {
			locked = append(locked, i18n.T(ctx, model.Priority(priority)))
		}
	}
	if len(locked) == 0 {
		return ""
	}
	separator := "、"
	if i18n.Lang(ctx) == "en" {
		separator = ", "
	}
	return strings.ReplaceAll(i18n.T(ctx, "{levels}需升級優先支援才能使用。"), "{levels}", strings.Join(locked, separator))
}

// prioritySupportDate is the last day of priority support in the site's
// timezone, as a date field shows it; empty when it does not end.
func prioritySupportDate(support model.PrioritySupport, location *time.Location) string {
	if support.Until == nil || support.Level == "low" {
		return ""
	}
	if location == nil {
		location = time.UTC
	}
	return support.Until.In(location).Add(-time.Nanosecond).Format(time.DateOnly)
}

// prioritySupportStatus tells admins when an upgrade ends or ended.
func prioritySupportStatus(ctx context.Context, support model.PrioritySupport, location *time.Location) string {
	date := strings.ReplaceAll(prioritySupportDate(support, location), "-", "/")
	switch {
	case date == "":
		return ""
	case support.Current(time.Now()) == "low":
		return strings.ReplaceAll(i18n.T(ctx, "{date} 已到期"), "{date}", date)
	}
	return strings.ReplaceAll(i18n.T(ctx, "至 {date}"), "{date}", date)
}

func daySummaryLocalized(ctx context.Context, day model.DaySchedule) string {
	if !day.Enabled {
		return i18n.T(ctx, "休息")
	}
	parts := make([]string, 0, len(day.Ranges))
	for _, period := range day.Ranges {
		parts = append(parts, period.Start+"–"+period.End)
	}
	if len(parts) == 0 {
		return i18n.T(ctx, "未設定")
	}
	separator := "、"
	if i18n.Lang(ctx) == "en" {
		separator = ", "
	}
	return strings.Join(parts, separator)
}

func activityText(ctx context.Context, a model.Activity) string {
	switch a.Kind {
	case "status":
		return i18n.T(ctx, "狀態改為") + "「" + i18n.T(ctx, model.Status(a.Detail)) + "」"
	case "priority":
		return i18n.T(ctx, "優先級改為") + "「" + i18n.T(ctx, model.Priority(a.Detail)) + "」"
	case "category":
		return i18n.T(ctx, "問題類別改為") + "「" + i18n.T(ctx, model.Category(a.Detail)) + "」"
	}
	return i18n.T(ctx, model.ActivityText(a))
}

func eventText(ctx context.Context, e model.Event, staff []model.User) string {
	arrow := " → "
	switch e.Kind {
	case "status":
		return i18n.T(ctx, model.Status(e.Previous)) + arrow + i18n.T(ctx, model.Status(e.Current))
	case "priority":
		return i18n.T(ctx, "優先級") + "：" + i18n.T(ctx, model.Priority(e.Previous)) + arrow + i18n.T(ctx, model.Priority(e.Current))
	case "category":
		return i18n.T(ctx, "問題類別") + "：" + i18n.T(ctx, model.Category(e.Previous)) + arrow + i18n.T(ctx, model.Category(e.Current))
	case "assignment":
		name := func(ids string) string {
			if ids == "" {
				return i18n.T(ctx, "未指派")
			}
			labels := []string{}
			for _, id := range strings.Split(ids, ",") {
				label := i18n.T(ctx, "客服") + " #" + id
				for _, u := range staff {
					if id == strconv.FormatInt(u.ID, 10) {
						label = u.Name
						break
					}
				}
				labels = append(labels, label)
			}
			return strings.Join(labels, ", ")
		}
		return i18n.T(ctx, "指派") + "：" + name(e.Previous) + arrow + name(e.Current)
	case "invite":
		return i18n.T(ctx, "邀請") + "：" + e.Current
	case "uninvite":
		return i18n.T(ctx, "移除邀請") + "：" + e.Previous
	}
	return i18n.T(ctx, model.EventText(e, staff))
}

func systemMessage(ctx context.Context, body string) string {
	const before, middle, after = "問題類別由「", "」改為「", "」"
	if !strings.HasPrefix(body, before) || !strings.HasSuffix(body, after) {
		return body
	}
	parts := strings.SplitN(strings.TrimSuffix(strings.TrimPrefix(body, before), after), middle, 2)
	if len(parts) != 2 {
		return body
	}
	valid := func(s string) bool { return s == "一般支援" || s == "帳務支援" || s == "技術支援" }
	if !valid(parts[0]) || !valid(parts[1]) {
		return body
	}
	if i18n.Lang(ctx) == "en" {
		return "Category changed from " + i18n.T(ctx, parts[0]) + " to " + i18n.T(ctx, parts[1])
	}
	return body
}

// discordError explains a stored Discord delivery code; see notify.DiscordError.
func discordError(code string) string {
	switch code {
	case "not_found":
		return "Discord 找不到這個 Webhook，可能已被刪除或網址有誤。"
	case "rejected":
		return "Discord 拒絕了這則訊息。"
	case "unavailable":
		return "Discord 暫時無法使用，稍後會自動重試。"
	case "rate_limited":
		return "Discord 限制了傳送頻率，請稍後再試。"
	}
	return "無法連線到 Discord，稍後會自動重試。"
}

// mailError explains a stored SMTP failure code; see notify.MailErrorCode.
func mailError(code string) string {
	switch code {
	case "auth":
		return "SMTP 登入失敗，請確認帳號和密碼。"
	case "rejected":
		return "SMTP 伺服器拒絕了這封信，請確認寄件地址可以使用這個帳號寄信。"
	case "unavailable":
		return "SMTP 伺服器暫時無法處理，稍後會自動重試。"
	case "tls":
		return "無法和 SMTP 伺服器建立安全連線，請確認主機名稱、連接埠和加密方式。"
	}
	return "無法連線到 SMTP 伺服器，請確認主機名稱和連接埠。"
}
