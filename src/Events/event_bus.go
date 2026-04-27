package Events

import (
	"cuento-backend/src/Entities"
	"database/sql"
	"sync"
	"time"
)

type EventType string

const (
	TopicCreated           EventType = "TopicCreated"
	PostCreated            EventType = "PostCreated"
	NotificationCreated    EventType = "NotificationCreated"
	UserReadingTopic       EventType = "UserReadingTopic"
	CharacterCreated       EventType = "CharacterCreated"
	EpisodeCreated         EventType = "EpisodeCreated"
	CharacterAccepted      EventType = "CharacterAccepted"
	UserRegistered         EventType = "UserRegistered"
	DirectMessageCreated   EventType = "DirectMessageCreated"
	WantedCharacterCreated EventType = "WantedCharacterCreated"
	StaticFileUploaded     EventType = "StaticFileUploaded"
	ReactionCreated        EventType = "ReactionCreated"
	TopicsMoved            EventType = "TopicsMoved"
)

type EventData interface{}

type TopicCreatedEvent struct {
	Type       string `json:"type"`
	TopicID    int64
	SubforumID int
	Title      string
	PostID     int64
	UserID     int
	Username   string
}

type PostCreatedEvent struct {
	Type       string        `json:"type"`
	TopicID    int64         `json:"topic_id"`
	SubforumID int           `json:"subforum_id"`
	Post       Entities.Post `json:"post"`
}

type NotificationEvent struct {
	UserID  int         `json:"user_id"`
	Type    string      `json:"type"` // e.g., "info", "success", "error"
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type UserReadingTopicEvent struct {
	TopicID string `json:"topic_id"`
}

type CharacterCreatedEvent struct {
	CharacterID int64
	SubforumID  int
	TopicID     int64
	TopicName   string
}

type EpisodeCreatedEvent struct {
	EpisodeID  int64
	SubforumID int
	TopicID    int64
	TopicName  string
}

type CharacterAcceptedEvent struct {
	CharacterID   int
	CharacterName string
	UserID        int
	TopicID       int
}

type UserRegisteredEvent struct {
	UserID   int
	Username string
}

type WantedCharacterCreatedEvent struct {
	WantedCharacterID int64
	SubforumID        int
	TopicID           int64
	TopicName         string
	AuthorUserID      int
}

type DirectMessageCreatedEvent struct {
	MessageID   int64
	ChatID      int
	SenderID    int
	Ciphertext  string
	IV          string
	KeyAuthor   string
	KeyReceiver string
	DateSend    time.Time
}

type StaticFileUploadedEvent struct {
	FileType string
}

type TopicsMovedEvent struct {
	SubforumIDs []int // all affected subforum IDs (sources + target)
}

type ReactionCreatedEvent struct {
	TopicID      int64  `json:"topic_id"`
	TopicName    string `json:"topic_name"`
	PostID       int    `json:"post_id"`
	PostAuthorID int    `json:"post_author_id"`
	ReactionID   int    `json:"reaction_id"`
	Url          string `json:"url"`
	UserID       int    `json:"user_id"`
	UserName     string `json:"user_name"`
}

type EventHandler func(db *sql.DB, data EventData)

var (
	subscribers = make(map[EventType][]EventHandler)
	mu          sync.RWMutex
)

func Subscribe(eventType EventType, handler EventHandler) {
	mu.Lock()
	defer mu.Unlock()
	subscribers[eventType] = append(subscribers[eventType], handler)
}

func Publish(db *sql.DB, eventType EventType, data EventData) {
	mu.RLock()
	defer mu.RUnlock()
	if handlers, found := subscribers[eventType]; found {
		for _, handler := range handlers {
			// Run handlers in a goroutine to avoid blocking the main request
			go handler(db, data)
		}
	}
}
