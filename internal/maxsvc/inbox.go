package maxsvc

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/max-messenger/max-bot-api-client-go/v2/model"

	"print-kiosk/internal/mailinbox"
	"print-kiosk/internal/storage"
)

func (s *Service) pollOnce(ctx context.Context) {
	api, err := s.api()
	if err != nil {
		return
	}
	s.mu.RLock()
	marker := s.updateMarker
	s.mu.RUnlock()

	updates, next, err := api.Subscriptions.GetUpdates(ctx, marker)
	if err != nil {
		slog.Debug("max get updates", "error", err)
		return
	}
	s.mu.Lock()
	if next > 0 {
		s.updateMarker = next
		_ = s.stats.SetKV("max_update_marker", strconv.FormatInt(next, 10))
	}
	s.mu.Unlock()

	for _, u := range updates {
		if u.UpdateType != model.UpdateMessageCreated || u.Message == nil {
			continue
		}
		s.handleIncoming(ctx, u)
	}
}

func (s *Service) handleIncoming(ctx context.Context, u model.Update) {
	text := ""
	if u.Message != nil {
		text = strings.TrimSpace(u.Message.Body.Text)
	}
	userID := u.UserID
	fromName := ""
	if u.User != nil {
		fromName = strings.TrimSpace(u.User.Name)
		if fromName == "" {
			fromName = strings.TrimSpace(u.User.Username)
		}
	}
	if fromName == "" {
		fromName = fmt.Sprintf("user %d", userID)
	}

	upper := strings.ToUpper(strings.TrimSpace(text))
	s.mu.Lock()
	for _, sess := range s.scanSess {
		if sess.Status != StatusWaiting || sess.Code == "" {
			continue
		}
		if strings.Contains(upper, sess.Code) {
			sess.UserID = userID
			sess.Status = StatusFound
			sess.Error = ""
			s.mu.Unlock()
			_ = s.sendUserText(ctx, userID, "Код принят. Отправляем скан…")
			return
		}
	}
	s.mu.Unlock()

	atts := collectPrintable(u)
	if len(atts) == 0 {
		return
	}

	s.mu.Lock()
	var target *PrintSession
	for _, sess := range s.printSess {
		if sess.Status == StatusWaiting {
			target = sess
			break
		}
	}
	s.mu.Unlock()
	if target == nil {
		return
	}

	files, err := s.downloadAttachments(ctx, target.Dir, atts)
	if err != nil || len(files) == 0 {
		s.mu.Lock()
		if sess, ok := s.printSess[target.ID]; ok && sess.Status == StatusWaiting {
			sess.Error = "Не удалось скачать файл из MAX"
		}
		s.mu.Unlock()
		return
	}

	s.mu.Lock()
	if sess, ok := s.printSess[target.ID]; ok && sess.Status == StatusWaiting {
		sess.Files = files
		sess.From = fromName
		sess.UserID = userID
		sess.Status = StatusConfirm
		sess.Error = ""
		sess.Deadline = time.Now().Add(2 * time.Minute)
	}
	s.mu.Unlock()
}

type remoteFile struct {
	Name string
	URL  string
	Size int64
}

func collectPrintable(u model.Update) []remoteFile {
	if u.Message == nil {
		return nil
	}
	var out []remoteFile
	for _, a := range u.Message.Body.Attachments {
		switch a.Type {
		case model.AttachFile, model.AttachImage:
			url := strings.TrimSpace(a.Payload.URL)
			if url == "" {
				continue
			}
			name := strings.TrimSpace(a.FileName)
			if name == "" {
				name = filepath.Base(url)
			}
			if name == "" || name == "." || name == "/" {
				if a.Type == model.AttachImage {
					name = "image.jpg"
				} else {
					name = "document.bin"
				}
			}
			ext := strings.ToLower(filepath.Ext(name))
			if a.Type == model.AttachImage && ext == "" {
				name += ".jpg"
				ext = ".jpg"
			}
			if !mailinbox.IsSupportedExt(ext) && a.Type != model.AttachImage {
				continue
			}
			if a.Type == model.AttachImage && !mailinbox.IsSupportedExt(ext) {
				continue
			}
			out = append(out, remoteFile{Name: name, URL: url, Size: int64(a.Size)})
		}
	}
	return out
}

func (s *Service) maxBytes() int64 {
	limit := int64(20 << 20)
	if s.settings == nil {
		return limit
	}
	values, err := s.settings.GetAll()
	if err != nil {
		return limit
	}
	mb, _ := strconv.Atoi(values[storage.SettingEmailMaxFileSizeMB])
	if mb < 1 {
		mb = 20
	}
	return int64(mb) * 1024 * 1024
}

func (s *Service) downloadAttachments(ctx context.Context, dir string, atts []remoteFile) ([]File, error) {
	limit := s.maxBytes()
	client := &http.Client{Timeout: 60 * time.Second}
	var files []File
	for _, att := range atts {
		if att.Size > 0 && att.Size > limit {
			continue
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, att.URL, nil)
		if err != nil {
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
		_ = resp.Body.Close()
		if err != nil || resp.StatusCode >= 300 || len(data) == 0 {
			continue
		}
		if int64(len(data)) > limit {
			continue
		}
		fid := uuid.NewString()
		path := filepath.Join(dir, fid+filepath.Ext(att.Name))
		if err := os.WriteFile(path, data, 0o644); err != nil {
			continue
		}
		files = append(files, File{
			ID:   fid,
			Name: att.Name,
			Size: int64(len(data)),
			Path: path,
		})
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("нет файлов")
	}
	return files, nil
}
