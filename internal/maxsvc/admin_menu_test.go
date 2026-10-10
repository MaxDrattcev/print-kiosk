package maxsvc

import (
	"github.com/max-messenger/max-bot-api-client-go/v2/model"
	"testing"
)

func TestAdminActionsUseActualSender(t *testing.T) {
	u := model.Update{UpdateType: model.UpdateMessageCreated, UserID: 99, Message: &model.MessageUpdate{Sender: model.Sender{UserID: 7}, Body: model.MessageBody{Text: " ADMIN "}}}
	id, action := adminAction(u)
	if id != 7 || action != "admin" {
		t.Fatalf("%d %s", id, action)
	}
	u = model.Update{UpdateType: model.UpdateMessageCallback, UserID: 99, Callback: &model.Callback{User: model.User{UserID: 8}, Payload: adminStatusPayload}}
	id, action = adminAction(u)
	if id != 8 || action != "status" {
		t.Fatalf("%d %s", id, action)
	}
	u.Callback.Payload = "unknown"
	_, action = adminAction(u)
	if action != "" {
		t.Fatal("unknown callback accepted")
	}
}

func TestHistoryCallbackPeriods(t *testing.T) {
	for _, tc := range []struct{ payload, action string }{
		{"admin:history", "history"}, {"admin:history:1", "history:1"}, {"admin:history:2", "history:2"}, {"admin:history:3", "history:3"}, {"admin:history:4", "history:4"}, {"admin:history:5", "history:5"}, {"admin:history:0", ""}, {"admin:history:6", ""}, {"admin:history:01", ""}, {"admin:history:1:extra", ""},
	} {
		u := model.Update{UpdateType: model.UpdateMessageCallback, Callback: &model.Callback{User: model.User{UserID: 7}, Payload: tc.payload}}
		id, action := adminAction(u)
		if action != tc.action {
			t.Errorf("%s: got %s", tc.payload, action)
		}
		if action != "" && id != 7 {
			t.Fatal("incorrect sender")
		}
	}
}
