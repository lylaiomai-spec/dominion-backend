package EventHandlers

import (
	"cuento-backend/src/Events"
	"cuento-backend/src/Services"
	"database/sql"
)

func RegisterAbsenceTimerEventHandlers() {
	// Recalculate when a character is accepted (freshly approved).
	Events.Subscribe(Events.CharacterAccepted, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.CharacterAcceptedEvent)
		if !ok {
			return
		}
		Services.RecalculateAbsenceTimerStart(event.CharacterID, db)
	})

	// Recalculate when an existing character is re-activated by an admin.
	Events.Subscribe(Events.CharacterActivated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.CharacterActivatedEvent)
		if !ok {
			return
		}
		Services.RecalculateAbsenceTimerStart(event.CharacterID, db)
	})

	// Recalculate for all characters in the episode when a new post is created.
	Events.Subscribe(Events.PostCreated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.PostCreatedEvent)
		if !ok {
			return
		}
		var episodeID int
		var episodeStatus int
		if err := db.QueryRow(
			"SELECT id, episode_status FROM episode_base WHERE topic_id = ?", event.TopicID,
		).Scan(&episodeID, &episodeStatus); err != nil {
			return // not an episode topic
		}
		if episodeStatus != 0 {
			// Episode is no longer active — the closure handler already set the timer
			// with today as the floor. Don't overwrite it with stale date_last_post.
			return
		}
		Services.RecalculateAbsenceTimerStartForEpisode(episodeID, db)
	})
}
