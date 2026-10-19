package EventHandlers

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Events"
	"cuento-backend/src/Websockets"
	"database/sql"
	"fmt"
	"strconv"
)

func RegisterReactionEventHandlers() {
	Events.Subscribe(Events.ReactionCreated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.ReactionCreatedEvent)
		if !ok {
			return
		}

		// 1. Notify the post author
		Events.Publish(db, Events.NotificationCreated, Events.NotificationEvent{
			UserID:  event.PostAuthorID,
			Type:    "reaction",
			Message: fmt.Sprintf("%s reacted to a post in %s", event.UserName, event.TopicName),
			Data: Entities.NotificationReaction{
				PostId:     event.PostID,
				TopicId:    event.TopicID,
				TopicName:  event.TopicName,
				ReactionId: event.ReactionID,
				Url:        event.Url,
				UserId:     event.UserID,
				UserName:   event.UserName,
			},
		})

		// 2. Broadcast reaction_created to everyone on this topic page.
		// Use per-connection hub tracking so multi-tab users aren't missed.
		topicIDStr := strconv.FormatInt(event.TopicID, 10)
		userIDs := Websockets.MainHub.GetUserIDsOnPage("topic", topicIDStr)
		payload := map[string]interface{}{
			"type": "reaction_created",
			"data": map[string]interface{}{
				"post_id":     event.PostID,
				"reaction_id": event.ReactionID,
				"url":         event.Url,
				"user_id":     event.UserID,
				"user_name":   event.UserName,
			},
		}
		for _, uid := range userIDs {
			Websockets.MainHub.SendNotification(uid, payload)
		}
	})
}
