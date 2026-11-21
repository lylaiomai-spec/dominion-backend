package test

import (
	"cuento-backend/src/Entities"
	"encoding/json"
	"net/http"
	"testing"
)

func TestAIChat_Access(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   interface{}
		caller func(f *fixture) int
		want   int
	}{
		{"history unauthenticated", "GET", "/ai-chat/history", nil, func(f *fixture) int { return 0 }, http.StatusUnauthorized},
		{"history user role", "GET", "/ai-chat/history", nil, func(f *fixture) int { return f.member }, http.StatusForbidden},
		{"history admin", "GET", "/ai-chat/history", nil, func(f *fixture) int { return f.admin }, http.StatusOK},
		{"clear unauthenticated", "POST", "/ai-chat/clear", map[string]string{"session_id": "s"}, func(f *fixture) int { return 0 }, http.StatusUnauthorized},
		{"clear user role", "POST", "/ai-chat/clear", map[string]string{"session_id": "s"}, func(f *fixture) int { return f.member }, http.StatusForbidden},
		{"clear admin", "POST", "/ai-chat/clear", map[string]string{"session_id": "s"}, func(f *fixture) int { return f.admin }, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			expectStatus(t, f.do(t, tt.method, tt.path, tt.caller(f), tt.body), tt.want)
		})
	}
}

func TestClearAIContext_DoesNotInsertRow(t *testing.T) {
	f := newFixture(t)

	expectStatus(t, f.do(t, "POST", "/ai-chat/clear", f.admin, map[string]string{"session_id": "s"}), http.StatusOK)

	if n := f.count(t, "SELECT COUNT(*) FROM ai_chat_messages"); n != 0 {
		t.Errorf("expected no ai_chat_messages rows after clear, got %d", n)
	}
}

func TestGetAIChatHistory_ExcludesClearRows(t *testing.T) {
	f := newFixture(t)
	insertMessage := func(userID int, role, content, created string) {
		f.exec(t, "INSERT INTO ai_chat_messages (user_id, role, content, date_created) VALUES (?, ?, ?, ?)", userID, role, content, created)
	}
	insertMessage(f.admin, "user", "question", "2026-11-01 10:00:00")
	insertMessage(f.admin, "clear", "", "2026-11-01 10:01:00")
	insertMessage(f.admin, "assistant", "answer", "2026-11-01 10:02:00")
	insertMessage(f.admin, "clear", "", "2026-11-01 10:03:00")
	insertMessage(f.member, "user", "someone else", "2026-11-01 10:04:00")

	w := f.do(t, "GET", "/ai-chat/history", f.admin, nil)
	expectStatus(t, w, http.StatusOK)

	var messages []Entities.AIChatMessage
	if err := json.Unmarshal(w.Body.Bytes(), &messages); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d: %s", len(messages), w.Body.String())
	}
	if messages[0].Role != "user" || messages[0].Content != "question" {
		t.Errorf("expected first message user/question, got %s/%q", messages[0].Role, messages[0].Content)
	}
	if messages[1].Role != "assistant" || messages[1].Content != "answer" {
		t.Errorf("expected second message assistant/answer, got %s/%q", messages[1].Role, messages[1].Content)
	}
}

func TestGetAIChatHistory_LocalizesDate(t *testing.T) {
	tests := []struct {
		name     string
		lang     string
		timezone string
		want     string
	}{
		{"english utc", "en", "", "1 Nov 2026 10:00 AM"},
		{"russian utc", "ru", "", "01.11.2026 10:00"},
		{"english user timezone", "en", "Europe/Berlin", "1 Nov 2026 11:00 AM"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.exec(t, "UPDATE users SET interface_language = ?, interface_timezone = NULLIF(?, '') WHERE id = ?", tt.lang, tt.timezone, f.admin)
			f.exec(t, "INSERT INTO ai_chat_messages (user_id, role, content, date_created) VALUES (?, 'user', 'question', '2026-11-01 10:00:00')", f.admin)

			w := f.do(t, "GET", "/ai-chat/history", f.admin, nil)
			expectStatus(t, w, http.StatusOK)

			var messages []map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &messages); err != nil {
				t.Fatalf("decoding response: %v", err)
			}
			if len(messages) != 1 {
				t.Fatalf("expected 1 message, got %d: %s", len(messages), w.Body.String())
			}
			if got := messages[0]["date"]; got != tt.want {
				t.Errorf("expected date %q, got %q", tt.want, got)
			}
			if _, ok := messages[0]["date_created"]; ok {
				t.Errorf("raw date_created must not be sent: %s", w.Body.String())
			}
		})
	}
}
