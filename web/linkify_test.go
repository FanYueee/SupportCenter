package web

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"supportcenter/internal/model"
)

func links(text string) []string {
	out := []string{}
	for _, part := range linkParts(text) {
		if part.URL != "" {
			out = append(out, part.URL)
		}
	}
	return out
}

func TestLinkPartsFindsWebAddresses(t *testing.T) {
	for text, want := range map[string]string{
		"請直接至 https://portal.example.com 註冊登入":          "https://portal.example.com",
		"網址https://portal.example.com註冊":                "https://portal.example.com",
		"請看 https://example.com/a?b=1&c=2。":             "https://example.com/a?b=1&c=2",
		"(see https://example.com/x).":                  "https://example.com/x",
		"https://en.wikipedia.org/wiki/Proxy_(network)": "https://en.wikipedia.org/wiki/Proxy_(network)",
		"結尾 https://example.com/path/,":                 "https://example.com/path/",
		"HTTPS://Example.com/Upper":                     "HTTPS://Example.com/Upper",
		"http://203.0.113.10:8080/status!":              "http://203.0.113.10:8080/status",
		"兩個 https://a.example 和 http://b.example/x?y=1": "https://a.example http://b.example/x?y=1",
		"沒有網址": "",
		"javascript:alert(1) data:text/html,x ftp://x": "",
		"https:// 以及 http://":                          "",
	} {
		if got := strings.Join(links(text), " "); got != want {
			t.Errorf("%q: got %q, want %q", text, got, want)
		}
	}
	// Splitting never loses or reorders text.
	text := "前 https://a.example/x, 中 http://b.example 後"
	var joined strings.Builder
	for _, part := range linkParts(text) {
		joined.WriteString(part.Text)
	}
	if joined.String() != text {
		t.Fatalf("text changed: %q", joined.String())
	}
}

func TestLinkedTextEscapesEverything(t *testing.T) {
	var out bytes.Buffer
	if err := LinkedText(`<b>hi</b> https://example.com/?q=<x>&a='1' "end"`).Render(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	want := `&lt;b&gt;hi&lt;/b&gt; <a href="https://example.com/?q=" target="_blank" rel="noopener noreferrer nofollow ugc">https://example.com/?q=</a>&lt;x&gt;&amp;a=&#39;1&#39; &#34;end&#34;`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	out.Reset()
	LinkedText(`https://example.com/a&b'c`).Render(context.Background(), &out)
	if !strings.Contains(out.String(), `href="https://example.com/a&amp;b&#39;c"`) {
		t.Fatalf("attribute not escaped: %s", out.String())
	}
}

func TestMentionsNeedATicketNumberOutsideWords(t *testing.T) {
	got := MentionedTickets("延續#6，也看 #0012、(#7) 和 https://example.com/#9", "C#3 #4a &#5; ##8 #0 #1234567890")
	if fmt.Sprint(got) != "[6 12 7]" {
		t.Fatalf("got %v", got)
	}
	var out bytes.Buffer
	refs := map[int64]model.TicketRef{6: {ID: 6, Subject: `<img src=x onerror=alert(1)>`}}
	if err := MessageText("看 #6 與 #7", refs, "/tickets/").Render(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	want := `<p>看 <a class="ticket-mention" href="/tickets/6"><span class="ticket-mention-number">#0006</span><span class="ticket-mention-subject">&lt;img src=x onerror=alert(1)&gt;</span></a> 與 #7</p>`
	if out.String() != want {
		t.Fatalf("got  %s\nwant %s", out.String(), want)
	}
}
