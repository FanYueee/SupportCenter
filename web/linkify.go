package web

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/a-h/templ"
	"supportcenter/internal/model"
)

// URL characters from RFC 3986. Anything else, including CJK text written
// right after a link, ends it.
var webURL = regexp.MustCompile(`(?i)https?://[A-Za-z0-9\-._~:/?#\[\]@!$&'()*+,;=%]+`)

type textPart struct {
	Text, URL string
	Ticket    int64 // a mentioned ticket number, e.g. from "#6"
}

// A mention is # and a ticket number, not inside a word, so "C#6", "#6a"
// and "&#6;" stay text. Chinese written right before it still counts.
var mention = regexp.MustCompile(`#0*([1-9][0-9]{0,8})`)

func mentionBoundary(c byte) bool {
	return !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '&' || c == '#')
}

// mentionParts splits plain text into runs and ticket mentions.
func mentionParts(text string) []textPart {
	var parts []textPart
	last := 0
	for _, m := range mention.FindAllStringSubmatchIndex(text, -1) {
		if (m[0] > 0 && !mentionBoundary(text[m[0]-1])) || (m[1] < len(text) && !mentionBoundary(text[m[1]])) {
			continue
		}
		id, _ := strconv.ParseInt(text[m[2]:m[3]], 10, 64)
		if m[0] > last {
			parts = append(parts, textPart{Text: text[last:m[0]]})
		}
		parts = append(parts, textPart{Text: text[m[0]:m[1]], Ticket: id})
		last = m[1]
	}
	if last < len(text) {
		parts = append(parts, textPart{Text: text[last:]})
	}
	return parts
}

// MentionedTickets lists the ticket numbers mentioned in the texts, at most 50.
func MentionedTickets(texts ...string) []int64 {
	seen := map[int64]bool{}
	var ids []int64
	for _, text := range texts {
		for _, part := range linkParts(text) {
			if part.URL != "" {
				continue
			}
			for _, m := range mentionParts(part.Text) {
				if m.Ticket > 0 && !seen[m.Ticket] && len(ids) < 50 {
					seen[m.Ticket] = true
					ids = append(ids, m.Ticket)
				}
			}
		}
	}
	return ids
}

// urlRanges finds the web links in text. Only http and https URLs with a
// host count; punctuation that ends a sentence stays out.
func urlRanges(text string) [][2]int {
	var ranges [][2]int
	for _, match := range webURL.FindAllStringIndex(text, -1) {
		start := match[0]
		end := start + len(trimURL(text[start:match[1]]))
		u, err := url.Parse(text[start:end])
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			continue
		}
		ranges = append(ranges, [2]int{start, end})
	}
	return ranges
}

// linkParts splits text into plain runs and web links.
func linkParts(text string) []textPart {
	var parts []textPart
	last := 0
	for _, link := range urlRanges(text) {
		start, end := link[0], link[1]
		if start > last {
			parts = append(parts, textPart{Text: text[last:start]})
		}
		parts = append(parts, textPart{Text: text[start:end], URL: text[start:end]})
		last = end
	}
	if last < len(text) {
		parts = append(parts, textPart{Text: text[last:]})
	}
	return parts
}

// trimURL drops trailing punctuation and a closing bracket the URL did not open.
func trimURL(s string) string {
	for s != "" {
		switch c := s[len(s)-1]; {
		case strings.IndexByte(".,;:!?'\"*", c) >= 0:
		case c == ')' && strings.Count(s, "(") < strings.Count(s, ")"):
		case c == ']' && strings.Count(s, "[") < strings.Count(s, "]"):
		default:
			return s
		}
		s = s[:len(s)-1]
	}
	return s
}

// LinkedText shows text as typed, with web addresses as links that open in a
// new tab. The link text is always the full address, so it cannot disguise
// where it goes.
func LinkedText(text string) templ.Component {
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		var b strings.Builder
		writeLinked(&b, text, nil, "")
		_, err := io.WriteString(w, b.String())
		return err
	})
}

// writeLinked writes escaped text with web addresses as links and mentioned
// tickets the viewer may open as a box with the number and subject, linking
// under base, e.g. "/tickets/". Other mentions stay plain text, so they reveal
// nothing.
func writeLinked(b *strings.Builder, text string, tickets map[int64]model.TicketRef, base string) {
	for _, part := range linkParts(text) {
		if part.URL != "" {
			b.WriteString(`<a href="` + html.EscapeString(part.URL) + `" target="_blank" rel="noopener noreferrer nofollow ugc">` + html.EscapeString(part.Text) + `</a>`)
			continue
		}
		for _, m := range mentionParts(part.Text) {
			ref, ok := tickets[m.Ticket]
			if m.Ticket == 0 || !ok {
				b.WriteString(html.EscapeString(m.Text))
				continue
			}
			b.WriteString(`<a class="ticket-mention" href="` + html.EscapeString(base+strconv.FormatInt(ref.ID, 10)) + `"><span class="ticket-mention-number">` +
				fmt.Sprintf("#%04d", ref.ID) + `</span><span class="ticket-mention-subject">` + html.EscapeString(ref.Subject) + `</span></a>`)
		}
	}
}
