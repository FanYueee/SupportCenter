package web

import (
	"context"
	"fmt"
	"strconv"
	"strings"

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
