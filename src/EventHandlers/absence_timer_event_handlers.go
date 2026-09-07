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
		if err := db.QueryRow(
			"SELECT id FROM episode_base WHERE topic_id = ?", event.TopicID,
		).Scan(&episodeID); err != nil {
			return // not an episode topic
		}
		Services.RecalculateAbsenceTimerStartForEpisode(episodeID, db)
	})
}
