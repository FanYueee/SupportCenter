package web

import (
	"context"
	"html"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/a-h/templ"
	"supportcenter/internal/model"
)

// Messages understand a small part of Markdown: **bold**, *italic*,
// ~~strikethrough~~, `code`, fenced code blocks, quotes, lists and headings.
// Text is always escaped and only these tags are written. There are no
// [text](url) links: web addresses stay links that show the full address.
var (
	mdFence    = regexp.MustCompile("^ {0,3}```")
	mdQuote    = regexp.MustCompile(`^ {0,3}> ?(.*)$`)
	mdBullet   = regexp.MustCompile(`^ {0,3}[-*+][ \t]+(.*)$`)
	mdNumber   = regexp.MustCompile(`^ {0,3}([0-9]{1,9})[.)][ \t]+(.*)$`)
	mdHeading  = regexp.MustCompile(`^ {0,3}#{1,3}[ \t]+(.+?)[ \t#]*$`)
	mdEmphasis = regexp.MustCompile(`\*\*([^\s*](?:[^\n]*?[^\s*])?)\*\*|~~([^\s~](?:[^\n]*?[^\s~])?)~~|\*([^\s*](?:[^\n*]*?[^\s*])?)\*`)
)

// MessageText renders a ticket message: the Markdown above, web links, and
// mentioned tickets the viewer may open (see writeLinked).
func MessageText(text string, tickets map[int64]model.TicketRef, base string) templ.Component {
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		r := markdown{tickets: tickets, base: base}
		r.blocks(text)
		_, err := io.WriteString(w, r.b.String())
		return err
	})
}

type markdown struct {
	b       strings.Builder
	tickets map[int64]model.TicketRef
	base    string
}

func (r *markdown) blocks(text string) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var paragraph []string
	flush := func() {
		// Blank lines next to a block give way to the gap between blocks.
		for len(paragraph) > 0 && strings.TrimSpace(paragraph[0]) == "" {
			paragraph = paragraph[1:]
		}
		for len(paragraph) > 0 && strings.TrimSpace(paragraph[len(paragraph)-1]) == "" {
			paragraph = paragraph[:len(paragraph)-1]
		}
		if len(paragraph) > 0 {
			r.wrap("p", strings.Join(paragraph, "\n"))
		}
		paragraph = nil
	}
	// run collects the following lines that match, as their first group.
	run := func(i *int, line *regexp.Regexp) []string {
		var items []string
		for ; *i < len(lines) && line.MatchString(lines[*i]); *i++ {
			m := line.FindStringSubmatch(lines[*i])
			items = append(items, m[len(m)-1])
		}
		*i--
		return items
	}
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		switch {
		case mdFence.MatchString(line):
			flush()
			var code []string
			for i++; i < len(lines) && !mdFence.MatchString(lines[i]); i++ {
				code = append(code, lines[i])
			}
			r.b.WriteString("<pre><code>" + html.EscapeString(strings.Join(code, "\n")) + "</code></pre>")
		case mdQuote.MatchString(line):
			flush()
			r.wrap("blockquote", strings.Join(run(&i, mdQuote), "\n"))
		case mdBullet.MatchString(line):
			flush()
			r.list("<ul>", "</ul>", run(&i, mdBullet))
		case mdNumber.MatchString(line):
			flush()
			open := "<ol>"
			if start, _ := strconv.Atoi(mdNumber.FindStringSubmatch(line)[1]); start != 1 {
				open = `<ol start="` + strconv.Itoa(start) + `">`
			}
			r.list(open, "</ol>", run(&i, mdNumber))
		case mdHeading.MatchString(line):
			flush()
			r.b.WriteString(`<p class="message-heading">`)
			r.inline(mdHeading.FindStringSubmatch(line)[1])
			r.b.WriteString("</p>")
		default:
			paragraph = append(paragraph, line)
		}
	}
	flush()
}

func (r *markdown) wrap(tag, text string) {
	r.b.WriteString("<" + tag + ">")
	r.inline(text)
	r.b.WriteString("</" + tag + ">")
}

func (r *markdown) list(open, close string, items []string) {
	r.b.WriteString(open)
	for _, item := range items {
		r.wrap("li", item)
	}
	r.b.WriteString(close)
}

// inline formats one block's text. Nothing inside `code` is formatted,
// linked or mentioned; a code span ends on its line.
func (r *markdown) inline(text string) {
	for text != "" {
		start := strings.IndexByte(text, '`')
		if start < 0 {
			break
		}
		end := strings.IndexByte(text[start+1:], '`')
		if end <= 0 || strings.Contains(text[start+1:start+1+end], "\n") {
			r.emphasis(text[:start+1])
			text = text[start+1:]
			continue
		}
		r.emphasis(text[:start])
		r.b.WriteString("<code>" + html.EscapeString(text[start+1:start+1+end]) + "</code>")
		text = text[start+2+end:]
	}
	r.emphasis(text)
}

func (r *markdown) emphasis(text string) {
	// A marker inside a web address, e.g. https://example.com/a*b*, is part of it.
	links := urlRanges(text)
	inLink := func(at int) bool {
		for _, l := range links {
			if at >= l[0] && at < l[1] {
				return true
			}
		}
		return false
	}
	last := 0
	for _, m := range mdEmphasis.FindAllStringSubmatchIndex(text, -1) {
		if inLink(m[0]) || inLink(m[1]-1) {
			continue
		}
		tag, inner := "strong", m[2:4]
		if m[4] >= 0 {
			tag, inner = "s", m[4:6]
		} else if m[6] >= 0 {
			tag, inner = "em", m[6:8]
		}
		writeLinked(&r.b, text[last:m[0]], r.tickets, r.base)
		r.b.WriteString("<" + tag + ">")
		r.emphasis(text[inner[0]:inner[1]])
		r.b.WriteString("</" + tag + ">")
		last = m[1]
	}
	writeLinked(&r.b, text[last:], r.tickets, r.base)
}
