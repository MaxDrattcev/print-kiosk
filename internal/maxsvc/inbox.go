package maxsvc

import (
	"archive/zip"
	"bytes"
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
		if u.UpdateType == model.UpdateBotStarted {
			s.handleIncoming(ctx, u)
			continue
		}
		if u.UpdateType != model.UpdateMessageCallback && (u.UpdateType != model.UpdateMessageCreated || u.Message == nil) {
			continue
		}
		s.handleIncoming(ctx, u)
	}
}

func (s *Service) handleIncoming(ctx context.Context, u model.Update) {
	userID := u.UserID
	if u.User != nil && u.User.UserID != 0 {
		userID = u.User.UserID
	}
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

	if u.UpdateType == model.UpdateBotStarted && s.claimAdminBinding(u.Payload, userID, fromName, time.Now()) {
		return
	}
	if s.claimScan(u, userID, time.Now()) {
		_ = s.sendUserText(ctx, userID, "Готовим документ к отправке…")
		return
	}

	if s.handleAdminCommand(ctx, u) {
		return
	}

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
			// MAX image URLs can contain opaque paths and query tokens, not filenames.
			if name != "" {
				name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
			}
			if name != "" && !mailinbox.IsSupportedExt(strings.ToLower(filepath.Ext(name))) && a.Type != model.AttachImage {
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
	client, err := newHTTPClient(60 * time.Second)
	if err != nil {
		return nil, err
	}
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
		name, ext := attachmentName(att.Name, data, len(files)+1)
		if !mailinbox.IsSupportedExt(ext) {
			continue
		}
		fid := uuid.NewString()
		path := filepath.Join(dir, fid+ext)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			continue
		}
		files = append(files, File{
			ID:   fid,
			Name: name,
			Size: int64(len(data)),
			Path: path,
		})
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("нет файлов")
	}
	return files, nil
}

// attachmentName uses the downloaded bytes rather than CDN URL parameters.
func attachmentName(original string, data []byte, index int) (string, string) {
	ext := ""
	switch http.DetectContentType(data) {
	case "application/pdf":
		ext = ".pdf"
	case "image/jpeg":
		ext = ".jpg"
	case "image/png":
		ext = ".png"
	case "image/bmp":
		ext = ".bmp"
	case "image/webp":
		ext = ".webp"
	}
	if bytes.HasPrefix(data, []byte("II\x2a\x00")) || bytes.HasPrefix(data, []byte("MM\x00\x2a")) {
		ext = ".tif"
	}
	if ext == "" {
		if archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data))); err == nil {
			for _, f := range archive.File {
				switch f.Name {
				case "word/document.xml":
					ext = ".docx"
				case "xl/workbook.xml":
					ext = ".xlsx"
				case "ppt/presentation.xml":
					ext = ".pptx"
				case "mimetype":
					if f.UncompressedSize64 > 128 {
						continue
					}
					r, err := f.Open()
					if err != nil {
						continue
					}
					mime, _ := io.ReadAll(io.LimitReader(r, 128))
					_ = r.Close()
					switch string(mime) {
					case "application/vnd.oasis.opendocument.text":
						ext = ".odt"
					case "application/vnd.oasis.opendocument.spreadsheet":
						ext = ".ods"
					case "application/vnd.oasis.opendocument.presentation":
						ext = ".odp"
					}
				}
			}
		}
	}
	oldExt := strings.ToLower(filepath.Ext(original))
	if ext == "" {
		ext = oldExt
	}
	if original != "" && mailinbox.IsSupportedExt(oldExt) {
		if ext == oldExt || (ext == ".jpg" && oldExt == ".jpeg") || (ext == ".tif" && oldExt == ".tiff") {
			return original, oldExt
		}
		return strings.TrimSuffix(original, filepath.Ext(original)) + ext, ext
	}
	label := "Документ"
	switch ext {
	case ".jpg", ".png", ".bmp", ".webp", ".tif", ".heic", ".heif":
		label = "Фото"
	}
	return fmt.Sprintf("%s %d%s", label, index, ext), ext
}

func (s *Service) claimScan(u model.Update, userID int64, now time.Time) bool {
	if userID == 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sess := range s.scanSess {
		if sess.Status != StatusWaiting || !now.Before(sess.Deadline) {
			continue
		}
		matched := u.UpdateType == model.UpdateBotStarted && sess.Token != "" && u.Payload == "scan_"+sess.Token
		if u.UpdateType == model.UpdateMessageCreated && u.Message != nil {
			matched = sess.Code != "" && strings.TrimSpace(u.Message.Body.Text) == sess.Code
		}
		if matched {
			sess.UserID = userID
			sess.Status = StatusFound
			sess.Error = ""
			return true
		}
	}
	return false
}
