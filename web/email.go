package web

import (
	"bytes"
	"context"
	"strings"
)

// EmailView is one customer email. The same content renders as HTML and as
// plain text; every field is shown as text, never as markup.
type EmailView struct {
	Site        string // the sender's name, e.g. "Example 支援中心"
	Heading     string
	Intro       []string
	QuoteAuthor string
	Quote       string
	Note        string
	Action      string
	Link        string
	SettingsURL string
}

// EmailHTML renders the HTML part of the email.
func EmailHTML(ctx context.Context, v EmailView) (string, error) {
	var out bytes.Buffer
	err := customerEmail(v).Render(ctx, &out)
	return out.String(), err
}

// EmailText renders the plain-text part of the email.
func EmailText(v EmailView) string {
	var b strings.Builder
	b.WriteString(v.Heading + "\n\n")
	for _, line := range v.Intro {
		b.WriteString(line + "\n\n")
	}
	if v.Quote != "" {
		b.WriteString(v.QuoteAuthor + "：\n")
		for _, line := range strings.Split(v.Quote, "\n") {
			b.WriteString("> " + line + "\n")
		}
		b.WriteString("\n")
	}
	if v.Note != "" {
		b.WriteString(v.Note + "\n\n")
	}
	b.WriteString(v.Action + "：" + v.Link + "\n\n")
	b.WriteString("—\n" + v.Site + "\n這是系統自動寄出的通知，請勿直接回覆。\n")
	if v.SettingsURL != "" {
		b.WriteString("不想收到客服單通知，可在個人設定關閉：" + v.SettingsURL + "\n")
	}
	return b.String()
}
