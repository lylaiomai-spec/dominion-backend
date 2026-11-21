package test

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/EventHandlers"
	"cuento-backend/src/MCP"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"
)

// Events.Subscribe is global, so the notification handler is registered once per run.
var registerNotificationHandlers sync.Once

func TestTypedNotification(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		typ      string
		dataJSON string
		check    func(t *testing.T, got interface{})
	}{
		{"ai message", "ai_message", `{"message_id": 42}`, func(t *testing.T, got interface{}) {
			n, ok := got.(*Entities.AIMessageNotification)
			if !ok {
				t.Fatalf("expected *AIMessageNotification, got %T", got)
			}
			if n.Data.MessageId != 42 {
				t.Errorf("expected message_id 42, got %d", n.Data.MessageId)
			}
		}},
		{"episode status change", "episode_status_change", `{"episode_id": 7, "new_status": 2}`, func(t *testing.T, got interface{}) {
			n, ok := got.(*Entities.EpisodeStatusChangeNotification)
			if !ok {
				t.Fatalf("expected *EpisodeStatusChangeNotification, got %T", got)
			}
			if n.Data.EpisodeId != 7 || n.Data.NewStatus != 2 {
				t.Errorf("expected episode 7 status 2, got %+v", n.Data)
			}
		}},
		{"mention", "mention", `{"post_id": 5, "topic_id": 3}`, func(t *testing.T, got interface{}) {
			n, ok := got.(*Entities.MentionNotification)
			if !ok {
				t.Fatalf("expected *MentionNotification, got %T", got)
			}
			if n.Data.PostId != 5 || n.Data.TopicId != 3 {
				t.Errorf("expected post 5 topic 3, got %+v", n.Data)
			}
		}},
		{"known type without data", "ai_message", "", func(t *testing.T, got interface{}) {
			if _, ok := got.(*Entities.AIMessageNotification); !ok {
				t.Errorf("expected *AIMessageNotification, got %T", got)
			}
		}},
		{"undecodable data", "ai_message", `{"message_id": "x"}`, func(t *testing.T, got interface{}) {
			if _, ok := got.(Entities.NotificationBase); !ok {
				t.Errorf("expected bare NotificationBase, got %T", got)
			}
		}},
		{"unknown type", "something_new", `{"a": 1}`, func(t *testing.T, got interface{}) {
			if _, ok := got.(Entities.NotificationBase); !ok {
				t.Errorf("expected bare NotificationBase, got %T", got)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			base := Entities.NotificationBase{Id: 1, Type: tt.typ}
			tt.check(t, Entities.TypedNotification(base, []byte(tt.dataJSON)))
		})
	}
}

func TestSendAIMessageToUser_CreatesMessageAndNotification(t *testing.T) {
	f := newFixture(t)
	registerNotificationHandlers.Do(EventHandlers.RegisterNotificationEventHandlers)
	// The user is offline in tests; disable push so no background push work outlives the test.
	f.exec(t, "INSERT INTO user_notification_setting (user_id, notification_type, disable_push) VALUES (?, 'ai_message', TRUE)", f.admin)

	messageID, err := MCP.SendAIMessageToUser(f.db, f.admin, "Please check **this**.")
	if err != nil {
		t.Fatalf("SendAIMessageToUser: %v", err)
	}

	var role, content string
	if err := f.db.QueryRow("SELECT role, content FROM ai_chat_messages WHERE id = ? AND user_id = ?", messageID, f.admin).Scan(&role, &content); err != nil {
		t.Fatalf("loading saved message: %v", err)
	}
	if role != "assistant" {
		t.Errorf("expected role assistant, got %q", role)
	}
	if content != "<p>Please check <strong>this</strong>.</p>\n" {
		t.Errorf("expected markdown rendered to HTML, got %q", content)
	}

	// The notification is saved by an async event handler.
	var title, dataJSON string
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := f.db.QueryRow("SELECT title, data FROM notifications WHERE user_id = ? AND type = 'ai_message'", f.admin).Scan(&title, &dataJSON)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("ai_message notification was not saved: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if title != "You have a message from AI" {
		t.Errorf("expected title 'You have a message from AI', got %q", title)
	}
	var data Entities.NotificationAIMessage
	if err := json.Unmarshal([]byte(dataJSON), &data); err != nil {
		t.Fatalf("decoding notification data: %v", err)
	}
	if int64(data.MessageId) != messageID {
		t.Errorf("expected notification message_id %d, got %d", messageID, data.MessageId)
	}
}

func TestSendAIMessageToUser_UnknownUser(t *testing.T) {
	f := newFixture(t)

	if _, err := MCP.SendAIMessageToUser(f.db, 999999, "hello"); err == nil {
		t.Error("expected an error for a user that does not exist")
	}
	if n := f.count(t, "SELECT COUNT(*) FROM notifications"); n != 0 {
		t.Errorf("expected no notification, got %d", n)
	}
}

func TestGetUnreadNotifications_TypedBuckets(t *testing.T) {
	f := newFixture(t)
	aiID := f.insert(t, `INSERT INTO notifications (user_id, type, title, message, data) VALUES (?, 'ai_message', 'You have a message from AI', 'msg', '{"message_id": 12}')`, f.member)
	statusID := f.insert(t, `INSERT INTO notifications (user_id, type, title, message, data) VALUES (?, 'episode_status_change', 'Scene Status Changed', 'msg', '{"episode_id": 4, "episode_name": "Dawn", "new_status": 2}')`, f.member)

	w := f.do(t, "GET", "/notifications/unread", f.member, nil)
	expectStatus(t, w, http.StatusOK)

	var response struct {
		System              []json.RawMessage                          `json:"system"`
		AIMessage           []Entities.AIMessageNotification           `json:"ai_message"`
		EpisodeStatusChange []Entities.EpisodeStatusChangeNotification `json:"episode_status_change"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(response.AIMessage) != 1 || response.AIMessage[0].Id != aiID || response.AIMessage[0].Data.MessageId != 12 {
		t.Errorf("expected ai_message notification %d with message_id 12, got %+v", aiID, response.AIMessage)
	}
	if len(response.EpisodeStatusChange) != 1 || response.EpisodeStatusChange[0].Id != statusID || response.EpisodeStatusChange[0].Data.EpisodeName != "Dawn" {
		t.Errorf("expected episode_status_change notification %d for 'Dawn', got %+v", statusID, response.EpisodeStatusChange)
	}
	if len(response.System) != 0 {
		t.Errorf("expected nothing in the system bucket, got %d", len(response.System))
	}
}

func TestDismissNotification_AIMessage(t *testing.T) {
	f := newFixture(t)
	id := f.insert(t, `INSERT INTO notifications (user_id, type, title, message, data) VALUES (?, 'ai_message', 'You have a message from AI', 'msg', '{"message_id": 1}')`, f.member)

	// Another user's dismissal succeeds but must not touch the row.
	expectStatus(t, f.do(t, "POST", fmt.Sprintf("/notifications/dismiss/%d", id), f.admin, nil), http.StatusOK)
	if n := f.count(t, "SELECT COUNT(*) FROM notifications WHERE id = ? AND is_read = TRUE", id); n != 0 {
		t.Fatal("another user's dismissal marked the notification read")
	}

	expectStatus(t, f.do(t, "POST", fmt.Sprintf("/notifications/dismiss/%d", id), f.member, nil), http.StatusOK)
	if n := f.count(t, "SELECT COUNT(*) FROM notifications WHERE id = ? AND is_read = TRUE", id); n != 1 {
		t.Error("expected the owner's dismissal to mark the notification read")
	}
}
