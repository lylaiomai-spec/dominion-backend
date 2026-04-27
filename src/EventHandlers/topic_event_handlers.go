package EventHandlers

import (
	"cuento-backend/src/Events"
	"cuento-backend/src/Services"
	"cuento-backend/src/Websockets"
	"database/sql"
	"fmt"
	"time"
)

func refreshSubforumStats(db *sql.DB, subforumID int) {
	var topicCount int
	_ = db.QueryRow("SELECT COUNT(*) FROM topics WHERE subforum_id = ?", subforumID).Scan(&topicCount)

	var postCount int
	_ = db.QueryRow("SELECT COUNT(*) FROM posts p JOIN topics t ON p.topic_id = t.id WHERE t.subforum_id = ? AND COALESCE(p.is_deleted, 0) != 1", subforumID).Scan(&postCount)

	var lastPostID *int64
	var lastPostTopicID *int64
	var lastPostTopicName *string
	var lastPostDate *time.Time
	var lastPostAuthor *string

	err := db.QueryRow(`
		SELECT p.id, t.id, t.name, p.date_created, u.username
		FROM posts p
		JOIN topics t ON p.topic_id = t.id
		LEFT JOIN users u ON u.id = p.author_user_id
		WHERE t.subforum_id = ? AND COALESCE(p.is_deleted, 0) != 1
		ORDER BY p.date_created DESC
		LIMIT 1
	`, subforumID).Scan(&lastPostID, &lastPostTopicID, &lastPostTopicName, &lastPostDate, &lastPostAuthor)

	if err == sql.ErrNoRows {
		var lastTopicID *int64
		var lastTopicName *string
		err2 := db.QueryRow("SELECT id, name FROM topics WHERE subforum_id = ? ORDER BY date_created DESC LIMIT 1", subforumID).Scan(&lastTopicID, &lastTopicName)
		if err2 == sql.ErrNoRows {
			_, _ = db.Exec(`UPDATE subforums SET topic_number = 0, post_number = 0,
				last_post_topic_id = NULL, last_post_topic_name = NULL,
				last_post_id = NULL, date_last_post = NULL,
				last_post_author_user_name = NULL, show_last_topic = NULL
				WHERE id = ?`, subforumID)
		} else if err2 == nil {
			_, _ = db.Exec(`UPDATE subforums SET topic_number = ?, post_number = ?,
				last_post_topic_id = ?, last_post_topic_name = ?,
				last_post_id = NULL, date_last_post = NULL,
				last_post_author_user_name = NULL, show_last_topic = true
				WHERE id = ?`, topicCount, postCount, lastTopicID, lastTopicName, subforumID)
		}
	} else if err == nil {
		_, _ = db.Exec(`UPDATE subforums SET topic_number = ?, post_number = ?,
			last_post_topic_id = ?, last_post_topic_name = ?,
			last_post_id = ?, date_last_post = ?,
			last_post_author_user_name = ?, show_last_topic = false
			WHERE id = ?`,
			topicCount, postCount, lastPostTopicID, lastPostTopicName,
			lastPostID, lastPostDate, lastPostAuthor, subforumID)
	}
}

func RegisterTopicEventHandlers() {
	// Subscriber 1: Update Global Stats
	Events.Subscribe(Events.TopicCreated, func(db *sql.DB, data Events.EventData) {
		_, ok := data.(Events.TopicCreatedEvent)
		if !ok {
			return
		}

		_, err := db.Exec("UPDATE global_stats SET stat_value = stat_value + 1 WHERE stat_name = 'total_post_number'")
		if err != nil {
			fmt.Printf("Error updating global post stats: %v\n", err)
		}
		_, err = db.Exec("UPDATE global_stats SET stat_value = stat_value + 1 WHERE stat_name = 'total_topic_number'")
		if err != nil {
			fmt.Printf("Error updating global topic stats: %v\n", err)
		}
	})

	// Subscriber 2: Update Subforum Stats
	Events.Subscribe(Events.TopicCreated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.TopicCreatedEvent)
		if !ok {
			return
		}

		_, err := db.Exec("UPDATE subforums SET topic_number = COALESCE(topic_number, 0) + 1, post_number = COALESCE(post_number, 0) + 1, last_post_topic_id = ?, last_post_topic_name = ?, last_post_id = ?, date_last_post = NOW(), last_post_author_user_name = ? WHERE id = ?",
			event.TopicID, event.Title, event.PostID, event.Username, event.SubforumID)
		if err != nil {
			fmt.Printf("Error updating subforum stats: %v\n", err)
		}
	})

	// Subscriber 3: Refresh subforum stats when topics are moved
	Events.Subscribe(Events.TopicsMoved, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.TopicsMovedEvent)
		if !ok {
			return
		}
		for _, id := range event.SubforumIDs {
			refreshSubforumStats(db, id)
		}
	})

	// Subscriber 4: Notify Topic Viewers
	Events.Subscribe(Events.UserReadingTopic, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.UserReadingTopicEvent)
		if !ok {
			return
		}

		// Get all users currently reading this topic
		users := Services.ActivityStorage.GetUsersOnPage("topic", event.TopicID)

		// Construct the notification message — only visible users appear in the list
		type Viewer struct {
			UserID   int    `json:"user_id"`
			Username string `json:"username"`
		}
		var viewerList []Viewer
		for _, u := range users {
			if u.IsVisible {
				viewerList = append(viewerList, Viewer{
					UserID:   u.UserID,
					Username: u.Username,
				})
			}
		}

		notification := map[string]interface{}{
			"type": "topic_viewers_update",
			"data": viewerList,
		}

		// Send to each user on the page
		for _, u := range users {
			Websockets.MainHub.SendNotification(u.UserID, notification)
		}
	})
}
