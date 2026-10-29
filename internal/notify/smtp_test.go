package notify

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"testing"
	"time"
)

func TestComposeBuildsSafeMultipartEmail(t *testing.T) {
	cfg := Config{From: "noreply@support.example.com"}
	raw, err := cfg.compose(Message{
		To:       "customer@example.com",
		FromName: "Example Cloud 支援中心",
		Subject:  "客服單 #0001 有新回覆：Hi\r\nBcc: victim@example.com",
		Text:     "第一行\n第二行",
		HTML:     "<p>第一行</p>",
	}, time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	from, err := mail.ParseAddress(msg.Header.Get("From"))
	if err != nil || from.Name != "Example Cloud 支援中心" || from.Address != "noreply@support.example.com" {
		t.Fatalf("From: %q %v", msg.Header.Get("From"), err)
	}
	subject, _ := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if msg.Header.Get("Bcc") != "" || subject != "客服單 #0001 有新回覆：Hi Bcc: victim@example.com" {
		t.Fatalf("header injection: Bcc=%q Subject=%q", msg.Header.Get("Bcc"), subject)
	}
	if msg.Header.Get("Auto-Submitted") != "auto-generated" || !strings.HasSuffix(msg.Header.Get("Message-ID"), "@support.example.com>") {
		t.Fatal("missing automatic-mail headers")
	}
	media, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || media != "multipart/alternative" {
		t.Fatal("not multipart/alternative", media, err)
	}
	parts := multipart.NewReader(msg.Body, params["boundary"])
	for _, want := range []struct{ kind, body string }{{"text/plain", "第一行\r\n第二行"}, {"text/html", "<p>第一行</p>"}} {
		part, err := parts.NextRawPart()
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(quotedprintable.NewReader(part))
		if !strings.HasPrefix(part.Header.Get("Content-Type"), want.kind) || strings.TrimRight(string(body), "\r\n") != want.body {
			t.Fatalf("%s part: %q", want.kind, body)
		}
	}
}
