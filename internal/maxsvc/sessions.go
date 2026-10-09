package maxsvc

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

func (s *Service) StartPrintSession(timeout time.Duration) (*PrintSession, error) {
	if !s.Enabled() {
		return nil, fmt.Errorf("MAX не активирован в настройках")
	}
	if timeout < 30*time.Second {
		timeout = 2 * time.Minute
	}
	id := uuid.NewString()
	dir := filepath.Join(s.rootDir, "print-"+id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	now := time.Now()
	sess := &PrintSession{
		ID:        id,
		Status:    StatusWaiting,
		Dir:       dir,
		CreatedAt: now,
		Deadline:  now.Add(timeout),
	}
	s.mu.Lock()
	s.printSess[id] = sess
	s.mu.Unlock()

	go func() {
		t := time.NewTimer(timeout)
		defer t.Stop()
		<-t.C
		s.mu.Lock()
		defer s.mu.Unlock()
		if sess, ok := s.printSess[id]; ok {
			switch sess.Status {
			case StatusWaiting, StatusConfirm:
				for _, f := range sess.Files {
					if f.Path != "" {
						_ = os.Remove(f.Path)
					}
				}
				sess.Files = nil
				sess.Status = StatusTimeout
				sess.Error = "Время ожидания истекло (2 минуты). Попробуйте снова"
			}
		}
	}()
	return sess, nil
}

func (s *Service) GetPrint(id string) (*PrintSession, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.printSess[id]
	return sess, ok
}

func (s *Service) ConfirmPrint(id string) (*PrintSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.printSess[id]
	if !ok {
		return nil, fmt.Errorf("сессия не найдена")
	}
	if sess.Status != StatusConfirm {
		return nil, fmt.Errorf("нет файла для подтверждения")
	}
	if len(sess.Files) == 0 {
		return nil, fmt.Errorf("в сообщении нет файлов")
	}
	sess.Status = StatusFound
	sess.Error = ""
	return sess, nil
}

func (s *Service) RejectPrint(id string) (*PrintSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.printSess[id]
	if !ok {
		return nil, fmt.Errorf("сессия не найдена")
	}
	if sess.Status != StatusConfirm {
		return nil, fmt.Errorf("нет файла для отклонения")
	}
	for _, f := range sess.Files {
		if f.Path != "" {
			_ = os.Remove(f.Path)
		}
	}
	sess.Files = nil
	sess.From = ""
	sess.UserID = 0
	sess.Status = StatusWaiting
	sess.Error = ""
	sess.Deadline = time.Now().Add(2 * time.Minute)
	return sess, nil
}

func (s *Service) GetPrintFile(sessionID, fileID string) (*PrintSession, *File, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.printSess[sessionID]
	if !ok {
		return nil, nil, fmt.Errorf("сессия не найдена")
	}
	if sess.Status != StatusFound {
		return nil, nil, fmt.Errorf("сначала подтвердите файл")
	}
	for i := range sess.Files {
		if sess.Files[i].ID == fileID {
			return sess, &sess.Files[i], nil
		}
	}
	return nil, nil, fmt.Errorf("файл не найден")
}

func (s *Service) MarkPrintPrinted(sessionID, fileID string) (*PrintSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.printSess[sessionID]
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

func (s *Service) StartScanSession(jobID string, timeout time.Duration) (*ScanSession, error) {
	if !s.Enabled() {
		return nil, fmt.Errorf("MAX не активирован в настройках")
	}
	if timeout < 30*time.Second {
		timeout = 2 * time.Minute
	}
	s.mu.Lock()
	code, err := s.newScanCodeLocked()
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	id := uuid.NewString()
	now := time.Now()
	sess := &ScanSession{
		ID:        id,
		JobID:     jobID,
		Code:      code,
		Token:     uuid.NewString(),
		Status:    StatusWaiting,
		CreatedAt: now,
		Deadline:  now.Add(timeout),
	}
	s.scanSess[id] = sess
	s.mu.Unlock()

	go func() {
		t := time.NewTimer(timeout)
		defer t.Stop()
		<-t.C
		s.mu.Lock()
		defer s.mu.Unlock()
		if sess, ok := s.scanSess[id]; ok && sess.Status == StatusWaiting {
			sess.Status = StatusTimeout
			sess.Error = "Время ожидания истекло"
		}
	}()
	return sess, nil
}

func (s *Service) GetScan(id string) (*ScanSession, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.scanSess[id]
	return sess, ok
}

// AbandonPrint deletes a print wait session and its local files.
func (s *Service) AbandonPrint(id string) {
	s.mu.Lock()
	sess, ok := s.printSess[id]
	if ok {
		delete(s.printSess, id)
	}
	s.mu.Unlock()
	if !ok || sess == nil {
		return
	}
	for _, f := range sess.Files {
		if f.Path != "" {
			_ = os.Remove(f.Path)
		}
	}
	if sess.Dir != "" {
		_ = os.RemoveAll(sess.Dir)
	}
}

// AbandonScan drops a scan delivery wait session.
func (s *Service) AbandonScan(id string) {
	s.mu.Lock()
	delete(s.scanSess, id)
	s.mu.Unlock()
}

// Caller holds s.mu through allocation and insertion.
func (s *Service) newScanCodeLocked() (string, error) {
	start, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	used := make(map[string]bool)
	for _, sess := range s.scanSess {
		if sess.Status == StatusWaiting && time.Now().Before(sess.Deadline) {
			used[sess.Code] = true
		}
	}
	for i := 0; i < 1000000; i++ {
		code := fmt.Sprintf("%06d", (int(start.Int64())+i)%1000000)
		if !used[code] {
			return code, nil
		}
	}
	return "", fmt.Errorf("слишком много активных сеансов MAX")
}
