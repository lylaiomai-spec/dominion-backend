package EventHandlers

import (
	"cuento-backend/src/Events"
	"database/sql"
	"fmt"
)

func RegisterArcEventHandlers() {
	Events.Subscribe(Events.ArcCreated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.ArcCreatedEvent)
		if !ok {
			return
		}

		description := "A private subforum for Game Masters of the story arc " + event.Title
		res, err := db.Exec(
			"INSERT INTO subforums (name, description, is_private) VALUES (?, ?, 1)",
			event.Title, description,
		)
		if err != nil {
			fmt.Printf("ArcCreated: failed to create subforum: %v\n", err)
			return
		}

		subforumID, _ := res.LastInsertId()

		if _, err := db.Exec("UPDATE arcs SET subforum_id = ? WHERE id = ?", subforumID, event.ArcID); err != nil {
			fmt.Printf("ArcCreated: failed to link subforum to arc: %v\n", err)
			return
		}

		for _, userID := range event.GMUserIDs {
			if _, err := db.Exec(
				"INSERT IGNORE INTO private_subforum_users (subforum_id, user_id) VALUES (?, ?)",
				subforumID, userID,
			); err != nil {
				fmt.Printf("ArcCreated: failed to add GM %d to private subforum: %v\n", userID, err)
			}
		}
	})

	Events.Subscribe(Events.ArcGMsUpdated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.ArcGMsUpdatedEvent)
		if !ok {
			return
		}

		var subforumID sql.NullInt64
		if err := db.QueryRow("SELECT subforum_id FROM arcs WHERE id = ?", event.ArcID).Scan(&subforumID); err != nil || !subforumID.Valid {
			return
		}
		sfID := subforumID.Int64

		prevSet := make(map[int]bool, len(event.PrevGMUserIDs))
		for _, id := range event.PrevGMUserIDs {
			prevSet[id] = true
		}
		newSet := make(map[int]bool, len(event.NewGMUserIDs))
		for _, id := range event.NewGMUserIDs {
			newSet[id] = true
		}

		for id := range newSet {
			if !prevSet[id] {
				if _, err := db.Exec(
					"INSERT IGNORE INTO private_subforum_users (subforum_id, user_id) VALUES (?, ?)",
					sfID, id,
				); err != nil {
					fmt.Printf("ArcGMsUpdated: failed to add GM %d: %v\n", id, err)
				}
			}
		}

		for id := range prevSet {
			if !newSet[id] {
				if _, err := db.Exec(
					"DELETE FROM private_subforum_users WHERE subforum_id = ? AND user_id = ?",
					sfID, id,
				); err != nil {
					fmt.Printf("ArcGMsUpdated: failed to remove GM %d: %v\n", id, err)
				}
			}
		}
	})
}
