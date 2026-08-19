package mailinbox

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Status string

const (
	StatusWaiting Status = "waiting"
	StatusConfirm Status = "confirm"
	StatusFound   Status = "found"
	StatusTimeout Status = "timeout"
	StatusError   Status = "error"
)

type File struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	Path    string `json:"-"`
	Printed bool   `json:"printed"`
}

type Session struct {
	ID          string
	Status      Status
	Error       string
	From        string
	Files       []File
	Dir         string
	SourceUID   uint32
	CreatedAt   time.Time
	Deadline    time.Time
	WaitTimeout time.Duration
	Lookback    time.Duration
	MailDeleted bool
	IgnoredUIDs map[uint32]struct{}
	MailCfg     Credentials
}

type Service struct {
	rootDir  string
	mu       sync.RWMutex
	sessions map[string]*Session
}

func NewService(rootDir string) (*Service, error) {
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		return nil, err
	}
	return &Service{
		rootDir:  rootDir,
		sessions: make(map[string]*Session),
	}, nil
}

func (s *Service) Get(id string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[id]
	return sess, ok
}

func (s *Service) GetFile(sessionID, fileID string) (*Session, *File, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[sessionID]
	if !ok {
		return nil, nil, fmt.Errorf("сессия не найдена")
	}
	if sess.Status != StatusFound {
		return nil, nil, fmt.Errorf("сначала подтвердите письмо")
	}
	for i := range sess.Files {
		if sess.Files[i].ID == fileID {
			return sess, &sess.Files[i], nil
		}
	}
	return nil, nil, fmt.Errorf("файл не найден")
}

// MarkPrinted marks an attachment as already printed, deletes its local copy,
// and removes the session directory when nothing remains to print.
func (s *Service) MarkPrinted(sessionID, fileID string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[sessionID]
	if !ok {
		return nil, fmt.Errorf("сессия не найдена")
	}

	var removePath string
	found := false
	for i := range sess.Files {
		if sess.Files[i].ID == fileID {
			found = true
			if !sess.Files[i].Printed {
				sess.Files[i].Printed = true
				removePath = sess.Files[i].Path
				sess.Files[i].Path = ""
			}
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("файл не найден")
	}

	if removePath != "" {
		_ = os.Remove(removePath)
	}

	remaining := 0
	for _, f := range sess.Files {
		if !f.Printed {
			remaining++
		}
	}
	if remaining == 0 && sess.Dir != "" {
		_ = os.RemoveAll(sess.Dir)
		sess.Dir = ""
	}

	return sess, nil
}

// TakeDeleteUID returns source message UID once, for mailbox cleanup after print.
func (s *Service) TakeDeleteUID(sessionID string) (uint32, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[sessionID]
	if !ok || sess.SourceUID == 0 || sess.MailDeleted {
		return 0, false
	}
	sess.MailDeleted = true
	return sess.SourceUID, true
}

// ResetDeleteFlag allows a later cleanup retry if IMAP delete failed.
func (s *Service) ResetDeleteFlag(sessionID string, uid uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[sessionID]
	if !ok {
		return
	}
	sess.MailDeleted = false
	if uid > 0 {
		sess.SourceUID = uid
	}
}

// Confirm accepts the pending message, cleans it from the mailbox, and opens files for printing.
func (s *Service) Confirm(sessionID string) (*Session, error) {
	s.mu.Lock()
	sess, ok := s.sessions[sessionID]
	if !ok {
		s.mu.Unlock()
		return nil, fmt.Errorf("сессия не найдена")
	}
	if sess.Status != StatusConfirm {
		s.mu.Unlock()
		return nil, fmt.Errorf("нет письма для подтверждения")
	}
	if len(sess.Files) == 0 {
		s.mu.Unlock()
		return nil, fmt.Errorf("в письме нет файлов")
	}
	sess.Status = StatusFound
	sess.Error = ""
	uid := sess.SourceUID
	cfg := sess.MailCfg
	alreadyDeleted := sess.MailDeleted
	if sess.IgnoredUIDs == nil {
		sess.IgnoredUIDs = make(map[uint32]struct{})
	}
	if uid > 0 {
		sess.IgnoredUIDs[uid] = struct{}{}
	}
	s.mu.Unlock()

	// Delete only the confirmed letter — older inbox mail must stay for other users.
	if !alreadyDeleted && uid > 0 {
		if err := DeleteUIDs(cfg, []uint32{uid}); err != nil {
			slog.Warn("email cleanup after confirm failed", "session", sessionID, "uid", uid, "error", err)
		} else {
			s.mu.Lock()
			if sess, ok := s.sessions[sessionID]; ok {
				sess.MailDeleted = true
			}
			s.mu.Unlock()
			slog.Info("email cleaned after confirm", "session", sessionID, "uid", uid)
		}
	}

	s.mu.RLock()
	sess = s.sessions[sessionID]
	s.mu.RUnlock()
	if sess == nil {
		return nil, fmt.Errorf("сессия не найдена")
	}
	return sess, nil
}

// Reject discards the pending message and continues waiting for another email.
// The rejected letter is ignored (and removed alone); older letters stay in the inbox.
func (s *Service) Reject(sessionID string) (*Session, error) {
	s.mu.Lock()
	sess, ok := s.sessions[sessionID]
	if !ok {
		s.mu.Unlock()
		return nil, fmt.Errorf("сессия не найдена")
	}
	if sess.Status != StatusConfirm {
		s.mu.Unlock()
		return nil, fmt.Errorf("нет письма для отклонения")
	}
	for _, f := range sess.Files {
		if f.Path != "" {
			_ = os.Remove(f.Path)
		}
	}
	uid := sess.SourceUID
	cfg := sess.MailCfg
	if sess.IgnoredUIDs == nil {
		sess.IgnoredUIDs = make(map[uint32]struct{})
	}
	if uid > 0 {
		sess.IgnoredUIDs[uid] = struct{}{}
	}
	sess.Files = nil
	sess.From = ""
	sess.SourceUID = 0
	sess.MailDeleted = false
	sess.Status = StatusWaiting
	sess.Error = ""
	// Fresh wait window so a re-sent letter is not cut off by the first timer.
	waitFor := sess.WaitTimeout
	if waitFor < 30*time.Second {
		waitFor = 2 * time.Minute
	}
	sess.Deadline = time.Now().Add(waitFor)
	s.mu.Unlock()

	// Remove only the rejected message so it is not offered again; keep others.
	if uid > 0 {
		if err := DeleteUIDs(cfg, []uint32{uid}); err != nil {
			slog.Warn("email reject cleanup failed", "session", sessionID, "uid", uid, "error", err)
		} else {
			slog.Info("rejected email removed", "session", sessionID, "uid", uid)
		}
	}

	s.mu.RLock()
	sess = s.sessions[sessionID]
	s.mu.RUnlock()
	if sess == nil {
		return nil, fmt.Errorf("сессия не найдена")
	}
	return sess, nil
}

// Start creates a wait session and polls mailbox for new attachments.
func (s *Service) Start(cfg Credentials, pollEvery, timeout time.Duration) (*Session, error) {
	if cfg.Address == "" || cfg.Password == "" {
		return nil, fmt.Errorf("почта не настроена в админ-панели")
	}
	if cfg.Login == "" {
		cfg.Login = cfg.Address
	}
	if cfg.Host == "" {
		cfg.Host, cfg.Port = ResolveHost(cfg.Address)
	}
	if pollEvery < 3*time.Second {
		pollEvery = 3 * time.Second
	}
	if timeout < 30*time.Second {
		timeout = 2 * time.Minute
	}

	// Connectivity check only — do not freeze a UID baseline (that skipped
	// already-delivered attachment mail after empty messages).
	if _, err := HighestUID(cfg); err != nil {
		return nil, err
	}

	id := uuid.NewString()
	dir := filepath.Join(s.rootDir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	now := time.Now()
	sess := &Session{
		ID:          id,
		Status:      StatusWaiting,
		Dir:         dir,
		CreatedAt:   now,
		Deadline:    now.Add(timeout),
		WaitTimeout: timeout,
		Lookback:    30 * time.Minute,
		Files:       nil,
		IgnoredUIDs: make(map[uint32]struct{}),
		MailCfg:     cfg,
	}
	s.mu.Lock()
	s.sessions[id] = sess
	s.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	go s.pollLoop(ctx, cancel, id, cfg, pollEvery)

	return sess, nil
}

func (s *Service) markTimeout(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return
	}
	switch sess.Status {
	case StatusWaiting, StatusConfirm:
		for _, f := range sess.Files {
			if f.Path != "" {
				_ = os.Remove(f.Path)
			}
		}
		sess.Files = nil
		sess.From = ""
		sess.Status = StatusTimeout
		sess.Error = "Время ожидания истекло (2 минуты). Попробуйте снова"
	}
}

func (s *Service) pollLoop(ctx context.Context, cancel context.CancelFunc, id string, cfg Credentials, every time.Duration) {
	defer cancel()
	ticker := time.NewTicker(every)
	defer ticker.Stop()

	s.pollOnce(id, cfg)

	for {
		select {
		case <-ctx.Done():
			s.markTimeout(id)
			return
		case <-ticker.C:
			s.mu.RLock()
			sess, ok := s.sessions[id]
			status := StatusError
			var deadline time.Time
			if ok {
				status = sess.Status
				deadline = sess.Deadline
			}
			s.mu.RUnlock()
			if !ok {
				return
			}
			switch status {
			case StatusFound, StatusTimeout, StatusError:
				return
			case StatusConfirm:
				// Give time to answer Yes/No; deadline was refreshed when letter was found.
				if !deadline.IsZero() && time.Now().After(deadline) {
					s.markTimeout(id)
					return
				}
				continue
			case StatusWaiting:
				if !deadline.IsZero() && time.Now().After(deadline) {
					s.markTimeout(id)
					return
				}
				s.pollOnce(id, cfg)
			}
		}
	}
}

func (s *Service) pollOnce(id string, cfg Credentials) {
	s.mu.RLock()
	sess, ok := s.sessions[id]
	if !ok || sess.Status != StatusWaiting {
		s.mu.RUnlock()
		return
	}
	lookback := sess.Lookback
	dir := sess.Dir
	ignore := copyUIDSet(sess.IgnoredUIDs)
	s.mu.RUnlock()

	result, err := FetchLatestPrintable(cfg, ignore, lookback)
	if err != nil {
		slog.Warn("email poll failed", "session", id, "error", err)
		s.mu.Lock()
		if sess, ok := s.sessions[id]; ok && sess.Status == StatusWaiting {
			sess.Error = err.Error()
		}
		s.mu.Unlock()
		return
	}

	s.mu.Lock()
	sess, ok = s.sessions[id]
	if !ok || sess.Status != StatusWaiting {
		s.mu.Unlock()
		return
	}
	if sess.IgnoredUIDs == nil {
		sess.IgnoredUIDs = make(map[uint32]struct{})
	}
	for _, uid := range result.EmptyUIDs {
		sess.IgnoredUIDs[uid] = struct{}{}
	}
	if result.Message == nil {
		sess.Error = ""
		s.mu.Unlock()
		slog.Debug("email poll empty", "session", id, "scanned", result.Scanned)
		return
	}

	msg := result.Message
	files := make([]File, 0, len(msg.Attachments))
	for _, att := range msg.Attachments {
		fid := uuid.NewString()
		path := filepath.Join(dir, fid+filepath.Ext(att.Name))
		if err := os.WriteFile(path, att.Data, 0o644); err != nil {
			slog.Warn("save email attachment", "error", err)
			continue
		}
		files = append(files, File{
			ID:   fid,
			Name: att.Name,
			Size: att.Size,
			Path: path,
		})
	}
	if len(files) == 0 {
		sess.IgnoredUIDs[msg.UID] = struct{}{}
		sess.Error = "В письме нет подходящих файлов для печати"
		s.mu.Unlock()
		return
	}
	sess.Files = files
	sess.From = msg.From
	sess.SourceUID = msg.UID
	sess.Status = StatusConfirm
	sess.Error = ""
	sess.MailDeleted = false
	// Pause the wait clock: user needs time to confirm without losing the session.
	waitFor := sess.WaitTimeout
	if waitFor < 30*time.Second {
		waitFor = 2 * time.Minute
	}
	sess.Deadline = time.Now().Add(waitFor)
	// Do not ignore yet — only after confirm/reject. Do not delete from mailbox until confirm.
	s.mu.Unlock()

	slog.Info("email attachments awaiting confirm",
		"session", id,
		"count", len(files),
		"from", msg.From,
		"uid", msg.UID,
		"scanned", result.Scanned,
	)
}

func copyUIDSet(in map[uint32]struct{}) map[uint32]struct{} {
	if len(in) == 0 {
		return map[uint32]struct{}{}
	}
	out := make(map[uint32]struct{}, len(in))
	for uid := range in {
		out[uid] = struct{}{}
	}
	return out
}
