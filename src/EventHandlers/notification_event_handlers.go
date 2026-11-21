package EventHandlers

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Events"
	"cuento-backend/src/Services"
	"cuento-backend/src/Websockets"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

func RegisterNotificationEventHandlers() {
	// Subscriber 3: Send Live Notifications and Save to DB
	Events.Subscribe(Events.NotificationCreated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.NotificationEvent)
		if !ok {
			return
		}

		// Save notification to database
		dataJSON, err := json.Marshal(event.Data)
		if err != nil {
			fmt.Printf("Error marshaling notification data: %v\n", err)
			return
		}

		title := "New Notification"
		switch event.Type {
		case "mention":
			title = "You were mentioned"
		case "account_update":
			title = "Account Update"
		case "reaction":
			title = "New Reaction"
		case "auto_archiving":
			title = "Auto Archiving"
		case "episode_status_change":
			title = "Scene Status Changed"
		case "topic_subscriptions":
			title = "New Post in Subscribed Topic"
		case "ai_message":
			title = "You have a message from AI"
		}

		res, err := db.Exec("INSERT INTO notifications (user_id, type, title, message, data, date_created, is_read) VALUES (?, ?, ?, ?, ?, NOW(), FALSE)",
			event.UserID, event.Type, title, event.Message, dataJSON)
		if err != nil {
			fmt.Printf("Error saving notification to DB: %v\n", err)
			return
		}

		notificationID, _ := res.LastInsertId()

		base := Entities.NotificationBase{
			Id:          int(notificationID),
			UserId:      event.UserID,
			Type:        event.Type,
			Title:       title,
			Message:     event.Message,
			DateCreated: time.Now(),
			IsRead:      false,
		}

		notification := Entities.TypedNotification(base, dataJSON)

		Websockets.MainHub.SendNotification(event.UserID, map[string]interface{}{
			"type": "notification",
			"data": notification,
		})

		// Send push notification only if user is offline and not disabled for this type.
		isConnected := Websockets.MainHub.IsUserConnected(event.UserID)
		if !isConnected {
			var disablePush bool
			_ = db.QueryRow(
				"SELECT disable_push FROM user_notification_setting WHERE user_id = ? AND notification_type = ?",
				event.UserID, event.Type,
			).Scan(&disablePush)
			if !disablePush {
				go Services.SendPushToUser(db, event.UserID, event.Type, title, event.Message, event.Data)
			}
		}
	})
}
