package MCP

import (
	"database/sql"
	"sync"
)

type chatSession struct {
	messages []ChatMessage
	mu       sync.Mutex
}

var (
	sessionsMu sync.RWMutex
	sessions   = map[string]*chatSession{}
)

func getOrCreateChatSession(sessionID string) *chatSession {
	sessionsMu.RLock()
	sess := sessions[sessionID]
	sessionsMu.RUnlock()
	if sess != nil {
		return sess
	}

	sess = &chatSession{}
	sessionsMu.Lock()
	sessions[sessionID] = sess
	sessionsMu.Unlock()
	return sess
}

// InvalidateChatSession drops the in-memory session (called on chat clear or page close).
func InvalidateChatSession(sessionID string) {
	sessionsMu.Lock()
	delete(sessions, sessionID)
	sessionsMu.Unlock()
}

func loadMessageByID(messageID int64, db *sql.DB) (ChatMessage, error) {
	var msg ChatMessage
	err := db.QueryRow(
		`SELECT role, content FROM ai_chat_messages WHERE id = ?`, messageID,
	).Scan(&msg.Role, &msg.Content)
	return msg, err
}
