package MCP

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Events"
	"cuento-backend/src/Services"
	"cuento-backend/src/Websockets"
	"database/sql"
	"fmt"
	"time"
)

// SendAIMessageToUser lets an AI agent start a message to a user: it saves the
// markdown as an assistant chat message, pushes it to an open AI chat, and
// raises an "ai_message" notification that the chat dismisses once loaded.
func SendAIMessageToUser(db *sql.DB, userID int, markdown string) (int64, error) {
	content := Services.MarkdownToHTML(markdown)
	res, err := db.Exec(
		"INSERT INTO ai_chat_messages (user_id, role, content, date_created) VALUES (?, 'assistant', ?, NOW())",
		userID, content,
	)
	if err != nil {
		return 0, fmt.Errorf("saving AI message for user %d: %w", userID, err)
	}
	messageID, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("reading AI message id for user %d: %w", userID, err)
	}

	locale := Services.GetUserLocale(userID, db)
	Websockets.MainHub.SendNotification(userID, map[string]interface{}{
		"type": "ai_message",
		"data": Entities.AIChatMessage{
			Id:      int(messageID),
			UserId:  userID,
			Role:    "assistant",
			Content: content,
			Date:    Services.LocalizeTimeForUser(time.Now(), locale),
		},
	})

	localizer := Services.NewLocalizer(locale.Lang)
	Events.Publish(db, Events.NotificationCreated, Events.NotificationEvent{
		UserID:  userID,
		Type:    "ai_message",
		Message: Services.T(localizer, "ai.message_notification"),
		Data:    Entities.NotificationAIMessage{MessageId: int(messageID)},
	})
	return messageID, nil
}
