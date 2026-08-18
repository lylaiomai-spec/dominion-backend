package EventHandlers

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Events"
	"cuento-backend/src/Services"
	"cuento-backend/src/Websockets"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

func RegisterDirectChatEventHandlers() {
	Events.Subscribe(Events.DirectMessageCreated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.DirectMessageCreatedEvent)
		if !ok {
			return
		}

		// Get sender info
		var username string
		var avatar *string
		err := db.QueryRow("SELECT username, avatar FROM users WHERE id = ?", event.SenderID).Scan(&username, &avatar)
		if err != nil {
			fmt.Printf("Error fetching sender info for direct message event: %v\n", err)
			return
		}

		// Get all participants of this chat along with their block date
		rows, err := db.Query(
			"SELECT user_id, chat_blocked_since_date FROM direct_chat_users WHERE direct_chat_id = ?",
			event.ChatID,
		)
		if err != nil {
			fmt.Printf("Error fetching direct chat participants: %v\n", err)
			return
		}
		defer rows.Close()

		chatIDStr := strconv.Itoa(event.ChatID)

		for rows.Next() {
			var participantID int
			var blockedSince *time.Time
			if err := rows.Scan(&participantID, &blockedSince); err != nil {
				continue
			}

			// Skip notification if the message is newer than the block date
			if blockedSince != nil && event.DateSend.After(*blockedSince) {
				continue
			}

			key := event.KeyReceiver
			if participantID == event.SenderID {
				key = event.KeyAuthor
			}

			tz := Services.GetUserTimezone(participantID, db)
			wsNotification := map[string]interface{}{
				"type": "direct_message_created",
				"data": map[string]interface{}{
					"id":                  event.MessageID,
					"chat_id":             event.ChatID,
					"user_id":             event.SenderID,
					"username":            username,
					"avatar":              avatar,
					"date_send":           event.DateSend,
					"date_send_localized": Services.LocalizeTime(event.DateSend, tz),
					"date_received":       nil,
					"ciphertext":          event.Ciphertext,
					"iv":                  event.IV,
					"key":                 key,
				},
			}

			Websockets.MainHub.SendNotification(participantID, wsNotification)

			// For receivers (not the sender), create a DB notification unless they have this specific chat open
			if participantID != event.SenderID {
				activity := Services.ActivityStorage.GetUserActivity(participantID)
				isViewingThisChat := activity != nil &&
					activity.CurrentPageType == "direct_chat" &&
					activity.CurrentPageId == chatIDStr

				if !isViewingThisChat {
					notification := saveDirectMessageNotification(db, participantID, event.ChatID, username, avatar)
					// If the user is online, also push the notification via WS
					if activity != nil && notification != nil {
						Websockets.MainHub.SendNotification(participantID, map[string]interface{}{
							"type": "notification",
							"data": notification,
						})
					}
				}
			}
		}
	})
}

func saveDirectMessageNotification(db *sql.DB, receiverID int, chatID int, senderUsername string, senderAvatar *string) *Entities.DirectMessageNotification {
	message := fmt.Sprintf("New message from %s", senderUsername)
	dataJSON, err := json.Marshal(Entities.NotificationDirectMessage{
		ChatId:   chatID,
		Username: senderUsername,
		Avatar:   senderAvatar,
	})
	if err != nil {
		fmt.Printf("Error marshaling direct message notification data: %v\n", err)
		return nil
	}

	res, err := db.Exec(
		"INSERT INTO notifications (user_id, type, title, message, data, date_created, is_read) VALUES (?, 'direct_message', 'New direct message', ?, ?, NOW(), FALSE)",
		receiverID, message, dataJSON,
	)
	if err != nil {
		fmt.Printf("Error saving direct message notification to DB: %v\n", err)
		return nil
	}

	notificationID, _ := res.LastInsertId()
	return &Entities.DirectMessageNotification{
		NotificationBase: Entities.NotificationBase{
			Id:          int(notificationID),
			UserId:      receiverID,
			Type:        "direct_message",
			Title:       "New direct message",
			Message:     message,
			DateCreated: time.Now(),
			IsRead:      false,
		},
		Data: Entities.NotificationDirectMessage{
			ChatId:   chatID,
			Username: senderUsername,
			Avatar:   senderAvatar,
		},
	}
}
