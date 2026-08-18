package EventHandlers

import (
	"cuento-backend/src/Events"
	"database/sql"
	"fmt"
	"strings"
)

func RegisterEpisodeEventHandlers() {
	// Subscriber 9: Update Global Stats on Episode Created
	Events.Subscribe(Events.EpisodeCreated, func(db *sql.DB, data Events.EventData) {
		_, ok := data.(Events.EpisodeCreatedEvent)
		if !ok {
			return
		}

		_, err := db.Exec("UPDATE global_stats SET stat_value = stat_value + 1 WHERE stat_name = 'total_episode_number'")
		if err != nil {
			fmt.Printf("Error updating global episode stats: %v\n", err)
		}

		_, err = db.Exec("UPDATE global_stats SET stat_value = stat_value + 1 WHERE stat_name = 'total_topic_number'")
		if err != nil {
			fmt.Printf("Error updating global topic stats on episode created: %v\n", err)
		}
	})

	// Subscriber: Update total_episodes for characters added to a new episode
	Events.Subscribe(Events.EpisodeCreated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.EpisodeCreatedEvent)
		if !ok {
			return
		}

		_, err := db.Exec(
			"UPDATE character_base SET total_episodes = total_episodes + 1 WHERE id IN (SELECT character_id FROM episode_character WHERE episode_id = ?)",
			event.EpisodeID,
		)
		if err != nil {
			fmt.Printf("Error updating character total_episodes: %v\n", err)
		}
	})

	// Subscriber: Decrement total_episodes for characters when episode topics are deleted
	Events.Subscribe(Events.EpisodeTopicsDeleted, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.EpisodeTopicsDeletedEvent)
		if !ok || len(event.EpisodeIDs) == 0 {
			return
		}
		placeholders := strings.Repeat("?,", len(event.EpisodeIDs)-1) + "?"
		args := make([]interface{}, len(event.EpisodeIDs))
		for i, id := range event.EpisodeIDs {
			args[i] = id
		}
		_, err := db.Exec(
			fmt.Sprintf("UPDATE character_base SET total_episodes = GREATEST(total_episodes - 1, 0) WHERE id IN (SELECT character_id FROM episode_character WHERE episode_id IN (%s))", placeholders),
			args...,
		)
		if err != nil {
			fmt.Printf("Error decrementing character total_episodes on episode topic deleted: %v\n", err)
		}
	})

	// Subscriber: Decrement global total_episode_number when episode topics are deleted
	Events.Subscribe(Events.EpisodeTopicsDeleted, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.EpisodeTopicsDeletedEvent)
		if !ok || len(event.EpisodeIDs) == 0 {
			return
		}
		count := len(event.EpisodeIDs)
		_, err := db.Exec("UPDATE global_stats SET stat_value = GREATEST(stat_value - ?, 0) WHERE stat_name = 'total_episode_number'", count)
		if err != nil {
			fmt.Printf("Error decrementing global total_episode_number on episode topic deleted: %v\n", err)
		}
	})

	// Subscriber: Update mask_stats total_episodes on episode created
	Events.Subscribe(Events.EpisodeCreated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.EpisodeCreatedEvent)
		if !ok {
			return
		}

		rows, err := db.Query(`SELECT DISTINCT cpb.user_id FROM character_profile_base cpb JOIN episode_mask em ON em.mask_id = cpb.id WHERE em.episode_id = ?`, event.EpisodeID)
		if err != nil {
			fmt.Printf("Error fetching mask user IDs on episode created: %v\n", err)
			return
		}
		defer rows.Close()

		for rows.Next() {
			var uid int
			if rows.Scan(&uid) == nil {
				_, err = db.Exec(`INSERT INTO mask_stats (user_id, total_episodes) VALUES (?, 1) ON DUPLICATE KEY UPDATE total_episodes = total_episodes + 1`, uid)
				if err != nil {
					fmt.Printf("Error updating mask_stats total_episodes on episode created: %v\n", err)
				}
			}
		}
	})

	// Subscriber: Update mask_stats total_episodes on episode updated (diff old vs new mask owners)
	Events.Subscribe(Events.EpisodeUpdated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.EpisodeUpdatedEvent)
		if !ok {
			return
		}

		prevSet := make(map[int]bool)
		for _, uid := range event.PrevMaskUserIDs {
			prevSet[uid] = true
		}
		newSet := make(map[int]bool)
		for _, uid := range event.NewMaskUserIDs {
			newSet[uid] = true
		}

		for uid := range newSet {
			if !prevSet[uid] {
				_, err := db.Exec(`INSERT INTO mask_stats (user_id, total_episodes) VALUES (?, 1) ON DUPLICATE KEY UPDATE total_episodes = total_episodes + 1`, uid)
				if err != nil {
					fmt.Printf("Error incrementing mask_stats total_episodes on episode updated: %v\n", err)
				}
			}
		}

		for uid := range prevSet {
			if !newSet[uid] {
				_, err := db.Exec(`INSERT INTO mask_stats (user_id, total_episodes) VALUES (?, 0) ON DUPLICATE KEY UPDATE total_episodes = GREATEST(total_episodes - 1, 0)`, uid)
				if err != nil {
					fmt.Printf("Error decrementing mask_stats total_episodes on episode updated: %v\n", err)
				}
			}
		}
	})

	// Subscriber: Update character total_episodes on episode updated (diff old vs new)
	Events.Subscribe(Events.EpisodeUpdated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.EpisodeUpdatedEvent)
		if !ok {
			return
		}

		prevSet := make(map[int]bool)
		for _, id := range event.PrevCharacterIDs {
			prevSet[id] = true
		}
		newSet := make(map[int]bool)
		for _, id := range event.NewCharacterIDs {
			newSet[id] = true
		}

		for id := range newSet {
			if !prevSet[id] {
				_, err := db.Exec("UPDATE character_base SET total_episodes = total_episodes + 1 WHERE id = ?", id)
				if err != nil {
					fmt.Printf("Error incrementing character total_episodes on episode updated: %v\n", err)
				}
			}
		}

		for id := range prevSet {
			if !newSet[id] {
				_, err := db.Exec("UPDATE character_base SET total_episodes = GREATEST(total_episodes - 1, 0) WHERE id = ?", id)
				if err != nil {
					fmt.Printf("Error decrementing character total_episodes on episode updated: %v\n", err)
				}
			}
		}
	})

	// Subscriber 10: Update Subforum Stats on Episode Created
	Events.Subscribe(Events.EpisodeCreated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.EpisodeCreatedEvent)
		if !ok {
			return
		}

		var username string
		_ = db.QueryRow("SELECT username FROM users WHERE id = ?", event.UserID).Scan(&username)
		_, err := db.Exec("UPDATE subforums SET topic_number = COALESCE(topic_number, 0) + 1, show_last_topic = true, last_post_topic_id = ?, last_post_topic_name = ?, date_last_post = NOW(), last_post_author_user_name = ? WHERE id = ?", event.TopicID, event.TopicName, username, event.SubforumID)
		if err != nil {
			fmt.Printf("Error updating subforum topic count for episode: %v\n", err)
		}
	})
}
