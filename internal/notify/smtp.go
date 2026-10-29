package notify

import (
	"bytes"
	"context"
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

type SMTP struct{ Config Config }

func (m SMTP) Send(ctx context.Context, to, subject, body string) error {
	address, err := mail.ParseAddress(to)
	if err != nil || address.Address != to || strings.ContainsAny(to, "\r\n\x00") {
		return errors.New("invalid notification recipient")
	}
	cfg := m.Config
	if err := cfg.Validate(); err != nil {
		return err
	}
	if !cfg.Enabled() {
		return errors.New("SMTP is not configured")
	}
	var data bytes.Buffer
	fmt.Fprintf(&data, "From: <%s>\r\nTo: <%s>\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n", cfg.From, to, mime.QEncoding.Encode("utf-8", subject), time.Now().UTC().Format(time.RFC1123Z))
	encoded := quotedprintable.NewWriter(&data)
	if _, err = encoded.Write([]byte(body)); err != nil {
		return err
	}
	if err = encoded.Close(); err != nil {
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
	if err = client.Rcpt(to); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err = writer.Write(data.Bytes()); err != nil {
		writer.Close()
		return err
	}
	if err = writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}
