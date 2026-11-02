package web

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/a-h/templ"
)

// EmailView is one email. The same content renders as HTML and as plain
// text; every field is shown as text, never as markup.
type EmailView struct {
	Site        string // the sender's name, e.g. "Example 支援中心"
	Logo        string // absolute URL of the site's logo; the name shows instead without one
	LogoWidth   int
	LogoHeight  int
	Website     string // the footer links here when set
	WebsiteName string
	Owner       string // copyright holder in the footer
	Year        int
	Accent      string // the logo's color for the label, avatar, button and top edge; neutral when ""
	Label       string // what kind of email this is, shown above the heading
	Heading     string
	Intro       []string
	Details     []EmailDetail // e.g. the ticket number and subject
	QuoteAuthor string
	Quote       string
	Note        string
	Action      string
	Link        string
	SettingsURL string
}

type EmailDetail struct{ Label, Value string }

// NewEmailView fills in how the site presents itself; origin is the public
// address, so the logo loads from the site in any email client.
func NewEmailView(site Site, origin string, now time.Time) EmailView {
	v := EmailView{Site: site.Title(), Website: site.Website, Owner: site.holder(), Year: now.Year()}
	if site.Website != "" {
		v.WebsiteName = strings.TrimSpace(site.Name + " 官方網站")
		if site.Name == "" {
			v.WebsiteName = "官方網站"
		}
	}
	if site.Logo != nil {
		v.Logo, v.Accent = origin+site.Logo.URL(), site.Logo.Accent
		v.LogoWidth, v.LogoHeight = site.Logo.fit(56, 260)
	}
	return v
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
	for _, d := range v.Details {
		b.WriteString(d.Label + "：" + d.Value + "\n")
	}
	if len(v.Details) > 0 {
		b.WriteString("\n")
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
	if v.Website != "" {
		b.WriteString(v.WebsiteName + "：" + v.Website + "\n")
	}
	return b.String()
}

// emailColors are the label's text and background and the solid color of the
// avatar, button and top edge.
func emailColors(v EmailView) (text, tint, solid string) {
	if v.Accent == "" {
		return "#3d3d3d", "#eeeeee", "#292929"
	}
	var r, g, b int
	fmt.Sscanf(v.Accent, "#%02x%02x%02x", &r, &g, &b)
	mix := func(c int) int { return c + (255-c)*9/10 }
	return v.Accent, fmt.Sprintf("#%02x%02x%02x", mix(r), mix(g), mix(b)), v.Accent
}

func emailLabelStyle(v EmailView) templ.SafeCSS {
	text, tint, _ := emailColors(v)
	return templ.SafeCSS("padding:5px 12px;border-radius:999px;background:" + tint + ";color:" + text + ";font-size:13px;line-height:1.3;font-weight:600;")
}

func emailAvatarStyle(v EmailView) templ.SafeCSS {
	_, _, solid := emailColors(v)
	return templ.SafeCSS("width:32px;height:32px;border-radius:16px;background:" + solid + ";color:#ffffff;font-size:13px;line-height:32px;font-weight:600;text-align:center;")
}

func emailButtonCellStyle(v EmailView) templ.SafeCSS {
	_, _, solid := emailColors(v)
	return templ.SafeCSS("background:" + solid + ";border-radius:8px;")
}

func emailTopStyle(v EmailView) templ.SafeCSS {
	_, _, solid := emailColors(v)
	return templ.SafeCSS("height:5px;line-height:5px;font-size:0;background:" + solid + ";border-radius:13px 13px 0 0;")
}

// emailInitial is the letter in the reply author's avatar.
func emailInitial(name string) string {
	if name == "AI 助理" {
		return "AI"
	}
	r, _ := utf8.DecodeRuneInString(strings.TrimSpace(name))
	if r == utf8.RuneError {
		return "?"
	}
	return strings.ToUpper(string(r))
}

func emailYear(v EmailView) string { return strconv.Itoa(v.Year) }
