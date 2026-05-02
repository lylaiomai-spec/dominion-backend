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

	// Subscriber 10: Update Subforum Stats on Episode Created
	Events.Subscribe(Events.EpisodeCreated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.EpisodeCreatedEvent)
		if !ok {
			return
		}

		_, err := db.Exec("UPDATE subforums SET topic_number = COALESCE(topic_number, 0) + 1, show_last_topic = true, last_post_topic_id = ?, last_post_topic_name = ? WHERE id = ?", event.TopicID, event.TopicName, event.SubforumID)
		if err != nil {
			fmt.Printf("Error updating subforum topic count for episode: %v\n", err)
		}
	})
}
