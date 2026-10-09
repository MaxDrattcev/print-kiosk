package maxsvc

import (
	"github.com/max-messenger/max-bot-api-client-go/v2/model"
	"regexp"
	"testing"
	"time"
)

func TestClaimScan(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name          string
		update        model.Update
		expired, want bool
	}{
		{name: "QR", update: model.Update{UpdateType: model.UpdateBotStarted, Payload: "scan_secret"}, want: true},
		{name: "manual", update: model.Update{UpdateType: model.UpdateMessageCreated, Message: &model.MessageUpdate{Body: model.MessageBody{Text: " 012345 "}}}, want: true},
		{name: "substring rejected", update: model.Update{UpdateType: model.UpdateMessageCreated, Message: &model.MessageUpdate{Body: model.MessageBody{Text: "90123456"}}}},
		{name: "wrong token", update: model.Update{UpdateType: model.UpdateBotStarted, Payload: "scan_other"}},
		{name: "expired", update: model.Update{UpdateType: model.UpdateBotStarted, Payload: "scan_secret"}, expired: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deadline := now.Add(time.Minute)
			if tc.expired {
				deadline = now.Add(-time.Second)
			}
			sess := &ScanSession{Code: "012345", Token: "secret", Status: StatusWaiting, Deadline: deadline}
			s := &Service{scanSess: map[string]*ScanSession{"one": sess}}
			if got := s.claimScan(tc.update, 42, now); got != tc.want {
				t.Fatalf("claim = %v", got)
			}
			if tc.want {
				if sess.UserID != 42 || sess.Status != StatusFound {
					t.Fatalf("session = %+v", sess)
				}
				if s.claimScan(tc.update, 99, now) || sess.UserID != 42 {
					t.Fatal("replay changed recipient")
				}
			}
		})
	}
}

func TestScanCodesSixDigitsAndUnique(t *testing.T) {
	s := &Service{scanSess: make(map[string]*ScanSession)}
	digits := regexp.MustCompile(`^[0-9]{6}$`)
	for i := 0; i < 100; i++ {
		code, err := s.newScanCodeLocked()
		if err != nil {
			t.Fatal(err)
		}
		if !digits.MatchString(code) {
			t.Fatalf("invalid code: %q", code)
		}
		if _, ok := s.scanSess[code]; ok {
			t.Fatal("duplicate active code")
		}
		s.scanSess[code] = &ScanSession{Code: code, Status: StatusWaiting, Deadline: time.Now().Add(time.Minute)}
	}
}
