package EventHandlers

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Events"
	"database/sql"
	"fmt"
)

func RegisterTopicSubscriptionEventHandlers() {
	Events.Subscribe(Events.PostCreated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.PostCreatedEvent)
		if !ok || event.Type != "post_created" {
			return
		}

		rows, err := db.Query(
			"SELECT user_id FROM user_topic_subscription WHERE topic_id = ? AND user_id != ?",
			event.TopicID, event.Post.AuthorUserId,
		)
		if err != nil {
			fmt.Printf("Error fetching topic subscribers: %v\n", err)
			return
		}
		defer rows.Close()

		var topicName string
		db.QueryRow("SELECT name FROM topics WHERE id = ?", event.TopicID).Scan(&topicName)

		for rows.Next() {
			var subscriberUserID int
			if err := rows.Scan(&subscriberUserID); err != nil {
				continue
			}
			Events.Publish(db, Events.NotificationCreated, Events.NotificationEvent{
				UserID:  subscriberUserID,
				Type:    "topic_subscriptions",
				Message: fmt.Sprintf("New post in %s", topicName),
				Data: Entities.NotificationTopicSubscription{
					TopicId:   int(event.TopicID),
					TopicName: topicName,
					PostId:    event.Post.Id,
				},
			})
		}
	})
}
