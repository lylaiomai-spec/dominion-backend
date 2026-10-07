package EventHandlers

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Events"
	"cuento-backend/src/Services"
	"database/sql"
	"fmt"
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

	// Notify co-participants in active episodes when a user's absence period begins.
	Events.Subscribe(Events.UserAbsenceStarted, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.UserAbsenceStartedEvent)
		if !ok {
			return
		}

		var username string
		if err := db.QueryRow("SELECT username FROM users WHERE id = ?", event.UserID).Scan(&username); err != nil {
			return
		}

		// Query distinct (recipient, absent_user_character_name) pairs across active shared episodes.
		rows, err := db.Query(`
			SELECT DISTINCT cb2.user_id, cb1.name
			FROM episode_character ec1
			JOIN character_base cb1 ON cb1.id = ec1.character_id
			JOIN episode_base eb ON eb.id = ec1.episode_id
			JOIN episode_character ec2 ON ec2.episode_id = ec1.episode_id
			JOIN character_base cb2 ON cb2.id = ec2.character_id
			WHERE cb1.user_id = ?
			  AND cb2.user_id != ?
			  AND eb.episode_status = ?
			ORDER BY cb2.user_id, cb1.name`,
			event.UserID, event.UserID, Entities.ActiveEpisode,
		)
		if err != nil {
			fmt.Printf("UserAbsenceStarted: failed to query co-participants for user %d: %v\n", event.UserID, err)
			return
		}
		defer rows.Close()

		// Group character names by recipient user ID.
		recipientChars := map[int][]string{}
		recipientOrder := []int{}
		for rows.Next() {
			var uid int
			var charName string
			if rows.Scan(&uid, &charName) != nil {
				continue
			}
			if _, seen := recipientChars[uid]; !seen {
				recipientOrder = append(recipientOrder, uid)
			}
			recipientChars[uid] = append(recipientChars[uid], charName)
		}

		endDateStr := event.AbsenceEndDate.Format("2006-01-02")
		for _, recipientID := range recipientOrder {
			charNames := recipientChars[recipientID]
			lang := Services.GetUserLanguage(recipientID, db)
			localizer := Services.NewLocalizer(lang)
			msg := Services.TData(localizer, "absence.started_notification", map[string]interface{}{
				"Username": username,
				"EndDate":  endDateStr,
			})
			Events.Publish(db, Events.NotificationCreated, Events.NotificationEvent{
				UserID:  recipientID,
				Type:    "absence_started",
				Message: msg,
				Data: map[string]interface{}{
					"user_id":          event.UserID,
					"username":         username,
					"absence_end_date": endDateStr,
					"characters":       charNames,
				},
			})
		}
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
