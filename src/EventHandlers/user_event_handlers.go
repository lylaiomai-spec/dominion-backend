package EventHandlers

import (
	"cuento-backend/src/Events"
	"database/sql"
	"fmt"
)

func RegisterUserEventHandlers() {
	// Subscriber: Update date_last_visit when user becomes active or inactive
	Events.Subscribe(Events.UserActivityChanged, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.UserActivityChangedEvent)
		if !ok {
			return
		}
		_, err := db.Exec("UPDATE users SET date_last_visit = NOW() WHERE id = ?", event.UserID)
		if err != nil {
			fmt.Printf("Error updating date_last_visit for user %d: %v\n", event.UserID, err)
		}
	})

	// Subscriber 15: Update Global Stats on User Registered
	Events.Subscribe(Events.UserRegistered, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.UserRegisteredEvent)
		if !ok {
			return
		}

		// 1. Update total user number
		_, err := db.Exec("UPDATE global_stats SET stat_value = stat_value + 1 WHERE stat_name = 'total_user_number'")
		if err != nil {
			fmt.Printf("Error updating global user stats: %v\n", err)
		}

		// 2. Update last user
		_, err = db.Exec("UPDATE global_stats SET stat_value = ?, stat_secondary = ? WHERE stat_name = 'last_user'", event.UserID, event.Username)
		if err != nil {
			fmt.Printf("Error updating last user global stat: %v\n", err)
		}
	})

	// Subscriber: Decrement global user count when an account is wiped
	Events.Subscribe(Events.UserWiped, func(db *sql.DB, data Events.EventData) {
		if _, ok := data.(Events.UserWipedEvent); !ok {
			return
		}
		_, _ = db.Exec("UPDATE global_stats SET stat_value = GREATEST(stat_value - 1, 0) WHERE stat_name = 'total_user_number'")
	})

	// Subscriber: Update post counters and subforum stats per topic batch
	Events.Subscribe(Events.GeneralPostsDeleted, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.GeneralPostsDeletedEvent)
		if !ok {
			return
		}

		_, _ = db.Exec(
			"UPDATE global_stats SET stat_value = GREATEST(stat_value - ?, 0) WHERE stat_name = 'total_post_number'",
			event.Count,
		)

		_, _ = db.Exec(`
			UPDATE topics SET
				post_number              = (SELECT COUNT(*) FROM posts WHERE topic_id = ? AND COALESCE(is_deleted, 0) != 1),
				date_last_post           = (SELECT MAX(date_created) FROM posts WHERE topic_id = ? AND COALESCE(is_deleted, 0) != 1),
				last_post_author_user_id = (SELECT author_user_id FROM posts WHERE topic_id = ? AND COALESCE(is_deleted, 0) != 1 ORDER BY date_created DESC LIMIT 1)
			WHERE id = ?`,
			event.TopicID, event.TopicID, event.TopicID, event.TopicID)

		refreshSubforumStats(db, event.SubforumID)
		Events.Publish(db, Events.SubforumUpdated, Events.SubforumUpdatedEvent{SubforumID: event.SubforumID})
	})
}
