package maxsvc

import (
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"print-kiosk/internal/storage"
)

type AdminBinding struct {
	ID       string    `json:"id"`
	Token    string    `json:"-"`
	Deadline time.Time `json:"deadline"`
	Status   string    `json:"status"`
	UserID   int64     `json:"user_id"`
	Name     string    `json:"name"`
}

func (s *Service) StartAdminBinding() (AdminBinding, error) {
	if !s.Enabled() {
		return AdminBinding{}, fmt.Errorf("сначала сохраните токен и включите MAX")
	}
	binding := AdminBinding{ID: uuid.NewString(), Token: uuid.NewString(), Deadline: time.Now().Add(2 * time.Minute), Status: "waiting"}
	s.mu.Lock()
	s.adminBinding = &binding
	s.mu.Unlock()
	return binding, nil
}

func (s *Service) GetAdminBinding(id string) (AdminBinding, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.adminBinding
	if b == nil || b.ID != id {
		return AdminBinding{}, false
	}
	if b.Status == "waiting" && !time.Now().Before(b.Deadline) {
		b.Status = "timeout"
	}
	return *b, true
}

func (s *Service) CancelAdminBinding(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.adminBinding != nil && s.adminBinding.ID == id && s.adminBinding.Status == "waiting" {
		s.adminBinding = nil
	}
}

func (s *Service) claimAdminBinding(payload string, userID int64, name string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.adminBinding
	if userID <= 0 || b == nil || b.Status != "waiting" || !now.Before(b.Deadline) || payload != "notify_"+b.Token {
		return false
	}
	if err := s.settings.SetMany(map[string]string{storage.SettingMaxAdminID: strconv.FormatInt(userID, 10)}); err != nil {
		return false
	}
	b.UserID = userID
	b.Name = name
	b.Status = "bound"
	return true
}
