package notify

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type Config struct {
	Host, Port, From, Username, Password, TLSMode string
}

func (c Config) Enabled() bool { return c.Host != "" }

func (c Config) Validate() error {
	if !c.Enabled() {
		if c.From != "" || c.Username != "" || c.Password != "" {
			return errors.New("SMTP_HOST is required when SMTP settings are provided")
		}
		return nil
	}
	port, err := strconv.Atoi(c.Port)
	if err != nil || port < 1 || port > 65535 {
		return errors.New("SMTP_PORT must be a valid port")
	}
	address, err := mail.ParseAddress(c.From)
	if err != nil || address.Address != c.From || strings.ContainsAny(c.From, "\r\n\x00") {
		return errors.New("SMTP_FROM must be one email address")
	}
	if strings.ContainsAny(c.Host, "\r\n\x00/ ") || c.Host == "" {
		return errors.New("SMTP_HOST must be a hostname")
	}
	if (c.Username == "") != (c.Password == "") {
		return errors.New("SMTP_USERNAME and SMTP_PASSWORD must both be set")
	}
	if c.TLSMode != "starttls" && c.TLSMode != "implicit" {
		return errors.New("SMTP_TLS_MODE must be starttls or implicit")
	}
	return nil
}

// Message is one email. HTML is optional; when set, the email carries both
// versions so clients without HTML still read the text.
type Message struct {
	To, FromName, Subject, Text, HTML string
}

type SMTP struct{ Config Config }

func (m SMTP) Send(ctx context.Context, msg Message) error {
	address, err := mail.ParseAddress(msg.To)
	if err != nil || address.Address != msg.To || strings.ContainsAny(msg.To, "\r\n\x00") {
		return errors.New("invalid notification recipient")
	}
	cfg := m.Config
	if err := cfg.Validate(); err != nil {
		return err
	}
	if !cfg.Enabled() {
		return errors.New("SMTP is not configured")
	}
	data, err := cfg.compose(msg, time.Now())
	if err != nil {
		return err
	}
	endpoint := net.JoinHostPort(cfg.Host, cfg.Port)
	conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", endpoint)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err = conn.SetDeadline(time.Now().Add(20 * time.Second)); err != nil {
		return err
	}
	security := &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}
	if cfg.TLSMode == "implicit" {
		secure := tls.Client(conn, security)
		if err = secure.HandshakeContext(ctx); err != nil {
			return err
		}
		conn = secure
	}
	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return err
	}
	defer client.Close()
	if cfg.TLSMode == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("SMTP server does not support STARTTLS")
		}
		if err = client.StartTLS(security); err != nil {
			return err
		}
	}
	if cfg.Username != "" {
		if err = client.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)); err != nil {
			return err
		}
	}
	if err = client.Mail(cfg.From); err != nil {
		return err
	}
	if err = client.Rcpt(msg.To); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err = writer.Write(data); err != nil {
		writer.Close()
		return err
	}
	if err = writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}

// compose builds the RFC 5322 message. Header values are stripped of control
// characters, so ticket text can never add headers.
func (c Config) compose(msg Message, now time.Time) ([]byte, error) {
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	from := (&mail.Address{Name: headerText(msg.FromName), Address: c.From}).String()
	domain := c.From[strings.LastIndex(c.From, "@")+1:]
	var data bytes.Buffer
	fmt.Fprintf(&data, "From: %s\r\nTo: <%s>\r\nSubject: %s\r\nDate: %s\r\nMessage-ID: <%x@%s>\r\nAuto-Submitted: auto-generated\r\nX-Auto-Response-Suppress: All\r\nMIME-Version: 1.0\r\n",
		from, msg.To, mime.QEncoding.Encode("utf-8", headerText(msg.Subject)), now.UTC().Format(time.RFC1123Z), id, domain)
	if msg.HTML == "" {
		data.WriteString("Content-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n")
		if err := writeQuotedPrintable(&data, msg.Text); err != nil {
			return nil, err
		}
		return data.Bytes(), nil
	}
	boundary := fmt.Sprintf("supportcenter-%x", id)
	fmt.Fprintf(&data, "Content-Type: multipart/alternative; boundary=\"%s\"\r\n\r\n", boundary)
	for _, part := range []struct{ kind, body string }{{"text/plain", msg.Text}, {"text/html", msg.HTML}} {
		fmt.Fprintf(&data, "--%s\r\nContent-Type: %s; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n", boundary, part.kind)
		if err := writeQuotedPrintable(&data, part.body); err != nil {
			return nil, err
		}
		data.WriteString("\r\n")
	}
	fmt.Fprintf(&data, "--%s--\r\n", boundary)
	return data.Bytes(), nil
}

func writeQuotedPrintable(out *bytes.Buffer, text string) error {
	encoded := quotedprintable.NewWriter(out)
	if _, err := encoded.Write([]byte(text)); err != nil {
		return err
	}
	return encoded.Close()
}

// headerText keeps one line of at most 200 characters.
func headerText(s string) string {
	s = strings.Join(strings.FieldsFunc(s, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }), " ")
	if r := []rune(s); len(r) > 200 {
		s = string(r[:200])
	}
	return s
}
