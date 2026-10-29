package web

import (
	"context"
	"html"
	"io"
	"net/url"
	"regexp"
	"strings"

	"github.com/a-h/templ"
)

// URL characters from RFC 3986. Anything else, including CJK text written
// right after a link, ends it.
var webURL = regexp.MustCompile(`(?i)https?://[A-Za-z0-9\-._~:/?#\[\]@!$&'()*+,;=%]+`)

type textPart struct{ Text, URL string }

// linkParts splits text into plain runs and web links. Only http and https
// URLs with a host become links; punctuation that ends a sentence stays out.
func linkParts(text string) []textPart {
	var parts []textPart
	last := 0
	for _, match := range webURL.FindAllStringIndex(text, -1) {
		start := match[0]
		if start < last {
			continue
		}
		end := start + len(trimURL(text[start:match[1]]))
		u, err := url.Parse(text[start:end])
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			continue
		}
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
		for _, part := range linkParts(text) {
			if part.URL == "" {
				b.WriteString(html.EscapeString(part.Text))
				continue
			}
			b.WriteString(`<a href="` + html.EscapeString(part.URL) + `" target="_blank" rel="noopener noreferrer nofollow ugc">` + html.EscapeString(part.Text) + `</a>`)
		}
		_, err := io.WriteString(w, b.String())
		return err
	})
}
