package maxsvc

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	maxbot "github.com/max-messenger/max-bot-api-client-go/v2"

	"print-kiosk/internal/mailinbox"
	"print-kiosk/internal/stats"
	"print-kiosk/internal/storage"
)

type Status = mailinbox.Status

const (
	StatusWaiting = mailinbox.StatusWaiting
	StatusConfirm = mailinbox.StatusConfirm
	StatusFound   = mailinbox.StatusFound
	StatusTimeout = mailinbox.StatusTimeout
	StatusError   = mailinbox.StatusError
)

type File = mailinbox.File

type PrintSession struct {
	ID        string
	Status    Status
	Error     string
	From      string
	UserID    int64
	Files     []File
	Dir       string
	CreatedAt time.Time
	Deadline  time.Time
}

type ScanSession struct {
	ID        string
	JobID     string
	Code      string
	Status    Status
	Error     string
	UserID    int64
	CreatedAt time.Time
	Deadline  time.Time
}

type Service struct {
	settings *storage.SettingsRepo
	stats    *stats.Repo
	rootDir  string

	mu           sync.RWMutex
	printSess    map[string]*PrintSession
	scanSess     map[string]*ScanSession
	updateMarker int64
	botUsername  string
	lastPaperN   int
	paperAlerted bool
}

func New(settings *storage.SettingsRepo, st *stats.Repo, rootDir string) (*Service, error) {
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		return nil, err
	}
	s := &Service{
		settings:  settings,
		stats:     st,
		rootDir:   rootDir,
		printSess: make(map[string]*PrintSession),
		scanSess:  make(map[string]*ScanSession),
	}
	if v, ok := st.GetKV("max_update_marker"); ok {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			s.updateMarker = n
		}
	}
	return s, nil
}

func (s *Service) creds() (token string, adminID int64, enabled bool) {
	values, err := s.settings.GetAll()
	if err != nil {
		return "", 0, false
	}
	enabled = values[storage.SettingMaxEnabled] == "true"
	token = strings.TrimSpace(values[storage.SettingMaxBotToken])
	adminRaw := strings.TrimSpace(values[storage.SettingMaxAdminID])
	if adminRaw != "" {
		adminID, _ = strconv.ParseInt(adminRaw, 10, 64)
	}
	if !enabled || token == "" || adminID == 0 {
		return token, adminID, false
	}
	return token, adminID, true
}

func (s *Service) Enabled() bool {
	_, _, ok := s.creds()
	return ok
}

func (s *Service) api() (*maxbot.Api, error) {
	token, _, ok := s.creds()
	if !ok {
		return nil, fmt.Errorf("MAX не активирован или не настроен")
	}
	return maxbot.NewApi(token)
}

func (s *Service) BotUsername() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.botUsername
}

func (s *Service) Info(ctx context.Context) (username string, enabled bool, err error) {
	token, _, on := s.creds()
	enabled = on
	if token == "" {
		return "", false, fmt.Errorf("токен MAX не задан")
	}
	api, err := maxbot.NewApi(token)
	if err != nil {
		return "", enabled, err
	}
	info, err := api.Bots.GetMyInfo(ctx)
	if err != nil {
		return "", enabled, err
	}
	username = strings.TrimPrefix(info.Username, "@")
	s.mu.Lock()
	s.botUsername = username
	s.mu.Unlock()
	return username, enabled, nil
}

// Run starts long-polling and the daily report scheduler until ctx is done.
func (s *Service) Run(ctx context.Context) {
	s.refreshBotInfo(ctx)
	s.NotifyStartup(ctx)
	go s.dailyLoop(ctx)

	for {
		if !s.Enabled() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
				continue
			}
		}
		s.pollOnce(ctx)
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

func (s *Service) refreshBotInfo(ctx context.Context) {
	if _, _, err := s.Info(ctx); err != nil {
		slog.Debug("max bot info", "error", err)
	}
}
