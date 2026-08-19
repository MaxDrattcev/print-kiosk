package mailout

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"path/filepath"
	"strings"
	"time"

	"print-kiosk/internal/mailcfg"
)

type Credentials struct {
	From     string // mailbox address shown as From
	Login    string
	Password string
	Host     string
	Port     int
}

type Attachment struct {
	Name string
	Data []byte
}

func ResolveHost(address string) (host string, port int) {
	return mailcfg.SMTPHost(address)
}

// SendMail sends a message with one file attachment to toAddr.
func SendMail(cfg Credentials, toAddr, subject, bodyText string, att Attachment) error {
	toAddr = strings.TrimSpace(toAddr)
	if toAddr == "" || !looksLikeEmail(toAddr) {
		return fmt.Errorf("укажите корректный email получателя")
	}
	if cfg.From == "" || cfg.Password == "" {
		return fmt.Errorf("почта киоска не настроена")
	}
	login := cfg.Login
	if login == "" {
		login = cfg.From
	}
	host := cfg.Host
	port := cfg.Port
	if host == "" {
		host, port = ResolveHost(cfg.From)
	}
	if port == 0 {
		port = 587
	}
	if att.Name == "" {
		att.Name = "scan.pdf"
	}
	if len(att.Data) == 0 {
		return fmt.Errorf("файл для отправки пуст")
	}

	msg, err := buildMessage(cfg.From, toAddr, subject, bodyText, att)
	if err != nil {
		return err
	}

	addr := fmt.Sprintf("%s:%d", host, port)
	auth := smtp.PlainAuth("", login, cfg.Password, host)

	switch port {
	case 465:
		return sendSMTPS(addr, host, auth, cfg.From, toAddr, msg)
	default:
		return sendSTARTTLS(addr, host, auth, cfg.From, toAddr, msg)
	}
}

func sendSTARTTLS(addr, host string, auth smtp.Auth, from, to string, msg []byte) error {
	c, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("подключение к SMTP: %w", err)
	}
	defer c.Close()

	if ok, _ := c.Extension("STARTTLS"); ok {
		tlsCfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
		if err := c.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("STARTTLS: %w", err)
		}
	}
	if auth != nil {
		if ok, _ := c.Extension("AUTH"); ok {
			if err := c.Auth(auth); err != nil {
				return fmt.Errorf("авторизация SMTP: %w", err)
			}
		}
	}
	if err := c.Mail(from); err != nil {
		return fmt.Errorf("отправитель: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("получатель: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		_ = w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

func sendSMTPS(addr, host string, auth smtp.Auth, from, to string, msg []byte) error {
	tlsCfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 20 * time.Second}, "tcp", addr, tlsCfg)
	if err != nil {
		return fmt.Errorf("подключение к SMTP(S): %w", err)
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer c.Close()

	if auth != nil {
		if ok, _ := c.Extension("AUTH"); ok {
			if err := c.Auth(auth); err != nil {
				return fmt.Errorf("авторизация SMTP: %w", err)
			}
		}
	}
	if err := c.Mail(from); err != nil {
		return fmt.Errorf("отправитель: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("получатель: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		_ = w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

func buildMessage(from, to, subject, bodyText string, att Attachment) ([]byte, error) {
	var buf bytes.Buffer
	boundary := "printkiosk_" + strings.ReplaceAll(fmt.Sprintf("%d", time.Now().UnixNano()), "-", "")

	writeHeader := func(k, v string) {
		buf.WriteString(k)
		buf.WriteString(": ")
		buf.WriteString(v)
		buf.WriteString("\r\n")
	}

	writeHeader("From", from)
	writeHeader("To", to)
	writeHeader("Subject", mime.QEncoding.Encode("utf-8", subject))
	writeHeader("MIME-Version", "1.0")
	writeHeader("Content-Type", `multipart/mixed; boundary="`+boundary+`"`)
	writeHeader("Date", time.Now().Format(time.RFC1123Z))
	buf.WriteString("\r\n")

	buf.WriteString("--" + boundary + "\r\n")
	buf.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	buf.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
	buf.WriteString(bodyText)
	buf.WriteString("\r\n")

	filename := filepath.Base(att.Name)
	encodedName := mime.QEncoding.Encode("utf-8", filename)
	buf.WriteString("--" + boundary + "\r\n")
	buf.WriteString("Content-Type: application/pdf; name=\"" + filename + "\"\r\n")
	buf.WriteString("Content-Transfer-Encoding: base64\r\n")
	buf.WriteString("Content-Disposition: attachment; filename=\"" + encodedName + "\"\r\n\r\n")

	encoded := base64.StdEncoding.EncodeToString(att.Data)
	for i := 0; i < len(encoded); i += 76 {
		end := i + 76
		if end > len(encoded) {
			end = len(encoded)
		}
		buf.WriteString(encoded[i:end])
		buf.WriteString("\r\n")
	}
	buf.WriteString("--" + boundary + "--\r\n")
	return buf.Bytes(), nil
}

func looksLikeEmail(s string) bool {
	at := strings.LastIndex(s, "@")
	if at < 1 || at == len(s)-1 {
		return false
	}
	dot := strings.LastIndex(s[at+1:], ".")
	return dot > 0 && at+1+dot < len(s)-1
}
