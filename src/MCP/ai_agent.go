package MCP

import "context"

// ChatSource holds a post/topic ID pair collected from tool call results.
type ChatSource struct {
	PostID    *int64 `json:"post_id,omitempty"`
	TopicID   *int64 `json:"topic_id,omitempty"`
	TopicName string `json:"topic_name,omitempty"`
	TopicType *int   `json:"topic_type,omitempty"`
}

type AIClient interface {
	Chat(ctx context.Context, history []ChatMessage, systemInstruction string) (string, []ChatSource, error)
}

type ChatMessage struct {
	Role    string // "user" or "assistant"
	Content string
}

type AIAgent struct {
	client AIClient
}

func NewAIAgent(client AIClient) *AIAgent {
	return &AIAgent{client: client}
}

func (a *AIAgent) Chat(ctx context.Context, history []ChatMessage, systemInstruction string) (string, []ChatSource, error) {
	return a.client.Chat(ctx, history, systemInstruction)
}
