package Entities

import "time"

type AISource struct {
	PostID    *int64 `json:"post_id,omitempty"`
	TopicID   *int64 `json:"topic_id,omitempty"`
	TopicName string `json:"topic_name,omitempty"`
	TopicType *int   `json:"topic_type,omitempty"`
}

type AIChatMessage struct {
	Id          int        `json:"id"`
	UserId      int        `json:"user_id"`
	Role        string     `json:"role"` // "user" or "assistant"
	Content     string     `json:"content"`
	Sources     []AISource `json:"sources,omitempty"`
	DateCreated time.Time  `json:"date_created"`
}
