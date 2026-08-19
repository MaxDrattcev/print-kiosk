package mailinbox

import (
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"path/filepath"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message/mail"

	"print-kiosk/internal/mailcfg"
)

type Credentials struct {
	Address  string
	Login    string
	Password string
	Host     string
	Port     int
	MaxBytes int64
}

type Attachment struct {
	Name string
	Size int64
	Data []byte
}

// MessageAttachments is one inbox message with printable attachments.
type MessageAttachments struct {
	UID         uint32
	From        string
	Attachments []Attachment
}

// FetchResult is returned by a mailbox scan.
type FetchResult struct {
	Message    *MessageAttachments
	EmptyUIDs  []uint32 // messages seen without printable files (skip later)
	Scanned    int
	HighestUID uint32
}

func ResolveHost(address string) (host string, port int) {
	return mailcfg.IMAPHost(address)
}

func connect(cfg Credentials) (*client.Client, error) {
	host := cfg.Host
	port := cfg.Port
	if host == "" {
		host, port = ResolveHost(cfg.Address)
	}
	if port == 0 {
		port = 993
	}
	login := cfg.Login
	if login == "" {
		login = cfg.Address
	}
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	c, err := client.DialWithDialerTLS(dialer, fmt.Sprintf("%s:%d", host, port), nil)
	if err != nil {
		return nil, fmt.Errorf("подключение к почте: %w", err)
	}
	if err := c.Login(login, cfg.Password); err != nil {
		_ = c.Logout()
		return nil, fmt.Errorf("вход в почту: %w", err)
	}
	return c, nil
}

// Test dials IMAP and authenticates, then logs out. No mailbox changes.
func Test(cfg Credentials) error {
	c, err := connect(cfg)
	if err != nil {
		return err
	}
	_ = c.Logout()
	return nil
}

// HighestUID returns the current highest UID in INBOX (0 if empty).
func HighestUID(cfg Credentials) (uint32, error) {
	c, err := connect(cfg)
	if err != nil {
		return 0, err
	}
	defer c.Logout()

	mbox, err := c.Select("INBOX", true)
	if err != nil {
		return 0, fmt.Errorf("открыть INBOX: %w", err)
	}
	if mbox.Messages == 0 {
		return 0, nil
	}
	if mbox.UidNext > 1 {
		return mbox.UidNext - 1, nil
	}
	return 0, nil
}

// FetchLatestPrintable scans recent INBOX messages and returns the newest one
// with printable attachments that is not in ignore.
// Empty / unsupported messages are listed in EmptyUIDs so the caller can skip them.
func FetchLatestPrintable(cfg Credentials, ignore map[uint32]struct{}, lookback time.Duration) (FetchResult, error) {
	var out FetchResult
	c, err := connect(cfg)
	if err != nil {
		return out, err
	}
	defer c.Logout()

	mbox, err := c.Select("INBOX", false)
	if err != nil {
		return out, fmt.Errorf("открыть INBOX: %w", err)
	}
	if mbox.Messages == 0 {
		return out, nil
	}

	// Recent messages by sequence number — more reliable than UID ranges after deletes.
	const window = uint32(40)
	var from uint32 = 1
	if mbox.Messages > window {
		from = mbox.Messages - window + 1
	}
	seqset := new(imap.SeqSet)
	seqset.AddRange(from, mbox.Messages)

	section := &imap.BodySectionName{Peek: true}
	items := []imap.FetchItem{imap.FetchEnvelope, imap.FetchUid, imap.FetchFlags, section.FetchItem()}
	messages := make(chan *imap.Message, 16)
	done := make(chan error, 1)
	go func() {
		done <- c.Fetch(seqset, items, messages)
	}()

	cutoff := time.Time{}
	if lookback > 0 {
		cutoff = time.Now().Add(-lookback)
	}

	type cand struct {
		uid   uint32
		from  string
		date  time.Time
		atts  []Attachment
		empty bool
	}
	var candidates []cand

	for msg := range messages {
		if msg == nil {
			continue
		}
		out.Scanned++
		if msg.Uid > out.HighestUID {
			out.HighestUID = msg.Uid
		}
		if ignore != nil {
			if _, skip := ignore[msg.Uid]; skip {
				continue
			}
		}

		date := time.Time{}
		if msg.Envelope != nil {
			date = msg.Envelope.Date
		}
		// Keep very recent mail even if envelope date is odd; drop clearly old mail.
		if lookback > 0 && !date.IsZero() && date.Before(cutoff) {
			continue
		}

		r := msg.GetBody(section)
		if r == nil {
			slog.Warn("email body missing", "uid", msg.Uid)
			continue
		}
		mr, err := mail.CreateReader(r)
		if err != nil {
			slog.Warn("email parse failed", "uid", msg.Uid, "error", err)
			continue
		}
		atts, err := readAttachments(mr, cfg.MaxBytes)
		if err != nil {
			slog.Warn("email attachments read failed", "uid", msg.Uid, "error", err)
		}
		if len(atts) == 0 {
			candidates = append(candidates, cand{uid: msg.Uid, empty: true})
			continue
		}
		candidates = append(candidates, cand{
			uid:  msg.Uid,
			from: formatEnvelopeFrom(msg.Envelope),
			date: date,
			atts: atts,
		})
	}
	if err := <-done; err != nil {
		return out, fmt.Errorf("чтение писем: %w", err)
	}

	var best *cand
	for i := range candidates {
		cnd := &candidates[i]
		if cnd.empty {
			out.EmptyUIDs = append(out.EmptyUIDs, cnd.uid)
			continue
		}
		if best == nil || cnd.uid > best.uid {
			best = cnd
		}
	}
	if best != nil {
		out.Message = &MessageAttachments{
			UID:         best.uid,
			From:        best.from,
			Attachments: best.atts,
		}
	}
	return out, nil
}

func formatEnvelopeFrom(env *imap.Envelope) string {
	if env == nil || len(env.From) == 0 || env.From[0] == nil {
		return ""
	}
	a := env.From[0]
	if a.MailboxName == "" || a.HostName == "" {
		return strings.TrimSpace(a.PersonalName)
	}
	addr := a.MailboxName + "@" + a.HostName
	if name := strings.TrimSpace(a.PersonalName); name != "" {
		return name + " <" + addr + ">"
	}
	return addr
}

// DeleteUIDs marks Deleted and expunges the given INBOX UIDs.
func DeleteUIDs(cfg Credentials, uids []uint32) error {
	if len(uids) == 0 {
		return nil
	}
	c, err := connect(cfg)
	if err != nil {
		return err
	}
	defer c.Logout()

	if _, err := c.Select("INBOX", false); err != nil {
		return fmt.Errorf("открыть INBOX: %w", err)
	}

	set := new(imap.SeqSet)
	set.AddNum(uids...)
	item := imap.FormatFlagsOp(imap.AddFlags, true)
	flags := []interface{}{imap.DeletedFlag}
	if err := c.UidStore(set, item, flags, nil); err != nil {
		return fmt.Errorf("пометить письма на удаление: %w", err)
	}
	if err := c.Expunge(nil); err != nil {
		return fmt.Errorf("удалить письма: %w", err)
	}
	return nil
}

func readAttachments(mr *mail.Reader, maxBytes int64) ([]Attachment, error) {
	var out []Attachment
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return out, err
		}
		filename, ok := partFilename(p)
		if !ok || !IsSupportedExt(filepath.Ext(filename)) {
			_, _ = io.Copy(io.Discard, p.Body)
			continue
		}
		limit := maxBytes
		if limit <= 0 {
			limit = 20 * 1024 * 1024
		}
		data, err := io.ReadAll(io.LimitReader(p.Body, limit+1))
		if err != nil {
			continue
		}
		if int64(len(data)) > limit {
			slog.Warn("email attachment too large", "name", filename, "size", len(data), "limit", limit)
			continue
		}
		if len(data) == 0 {
			continue
		}
		out = append(out, Attachment{Name: filename, Size: int64(len(data)), Data: data})
	}
	return out, nil
}

func partFilename(p *mail.Part) (string, bool) {
	switch h := p.Header.(type) {
	case *mail.AttachmentHeader:
		filename, _ := h.Filename()
		filename = sanitizeName(filename)
		if filename != "" {
			return filename, true
		}
		if name := nameFromContentType(h.Get("Content-Type")); name != "" {
			return name, true
		}
		if name := fallbackNameFromContentType(h.Get("Content-Type")); name != "" {
			return name, true
		}
	case *mail.InlineHeader:
		filename := ""
		disp, params, _ := mime.ParseMediaType(h.Get("Content-Disposition"))
		_ = disp
		filename = params["filename"]
		if filename == "" {
			filename = nameFromContentType(h.Get("Content-Type"))
		}
		filename = sanitizeName(filename)
		if filename != "" {
			return filename, true
		}
		if name := fallbackNameFromContentType(h.Get("Content-Type")); name != "" {
			return name, true
		}
	default:
		// Unknown header type — try raw headers.
		if th, ok := p.Header.(interface{ Get(string) string }); ok {
			filename := ""
			disp, params, _ := mime.ParseMediaType(th.Get("Content-Disposition"))
			if strings.EqualFold(disp, "attachment") || params["filename"] != "" {
				filename = params["filename"]
			}
			if filename == "" {
				filename = nameFromContentType(th.Get("Content-Type"))
			}
			filename = sanitizeName(filename)
			if filename != "" {
				return filename, true
			}
			if name := fallbackNameFromContentType(th.Get("Content-Type")); name != "" {
				return name, true
			}
		}
	}
	return "", false
}

func nameFromContentType(v string) string {
	_, params, err := mime.ParseMediaType(v)
	if err != nil {
		return ""
	}
	return sanitizeName(params["name"])
}

func fallbackNameFromContentType(v string) string {
	media, _, err := mime.ParseMediaType(v)
	if err != nil {
		return ""
	}
	switch strings.ToLower(media) {
	case "application/pdf":
		return "document.pdf"
	case "image/jpeg":
		return "image.jpg"
	case "image/png":
		return "image.png"
	case "image/heic", "image/heif":
		return "image.heic"
	case "application/msword":
		return "document.doc"
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return "document.docx"
	case "application/vnd.ms-excel":
		return "document.xls"
	case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return "document.xlsx"
	default:
		return ""
	}
}

func sanitizeName(name string) string {
	name = strings.TrimSpace(name)
	// Decode RFC 2047 if still encoded.
	if strings.Contains(name, "=?") {
		dec := new(mime.WordDecoder)
		if decoded, err := dec.DecodeHeader(name); err == nil {
			name = decoded
		}
	}
	name = filepath.Base(name)
	name = strings.ReplaceAll(name, "\x00", "")
	return strings.TrimSpace(name)
}

func IsSupportedExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx",
		".odt", ".ods", ".odp", ".txt", ".rtf",
		".jpg", ".jpeg", ".png", ".bmp", ".tif", ".tiff", ".webp", ".heic", ".heif":
		return true
	default:
		return false
	}
}
