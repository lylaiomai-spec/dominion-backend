package EventHandlers

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Events"
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

		var notification interface{}
		switch event.Type {
		case "mention":
			n := Entities.MentionNotification{NotificationBase: base}
			json.Unmarshal(dataJSON, &n.Data)
			notification = n
		case "game":
			n := Entities.GameNotification{NotificationBase: base}
			json.Unmarshal(dataJSON, &n.Data)
			notification = n
		case "system":
			n := Entities.SystemNotification{NotificationBase: base}
			json.Unmarshal(dataJSON, &n.Data)
			notification = n
		case "auto_archiving":
			n := Entities.AutoArchivingNotification{NotificationBase: base}
			json.Unmarshal(dataJSON, &n.Data)
			notification = n
		case "account_update":
			n := Entities.AccountUpdateNotification{NotificationBase: base}
			json.Unmarshal(dataJSON, &n.Data)
			notification = n
		case "direct_message":
			n := Entities.DirectMessageNotification{NotificationBase: base}
			json.Unmarshal(dataJSON, &n.Data)
			notification = n
		case "reaction":
			n := Entities.ReactionNotification{NotificationBase: base}
			json.Unmarshal(dataJSON, &n.Data)
			notification = n
		default:
			notification = base
		}

		Websockets.MainHub.SendNotification(event.UserID, map[string]interface{}{
			"type": "notification",
			"data": notification,
		})
	})
}
