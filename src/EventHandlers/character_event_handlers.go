package EventHandlers

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Events"
	"cuento-backend/src/Features"
	"cuento-backend/src/Services"
	"cuento-backend/src/Websockets"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
)

func RegisterCharacterEventHandlers() {
	// Subscriber 7: Update Global Stats on Character Created
	Events.Subscribe(Events.CharacterCreated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.CharacterCreatedEvent)
		if !ok {
			return
		}

		_, err := db.Exec("UPDATE global_stats SET stat_value = stat_value + 1 WHERE stat_name = 'total_topic_number'")
		if err != nil {
			fmt.Printf("Error updating global topic stats on character created: %v\n", err)
		}

		_, err = db.Exec("UPDATE subforums SET topic_number = COALESCE(topic_number, 0) + 1, show_last_topic = true, last_post_topic_id = ?, last_post_topic_name = ? WHERE id = ?", event.TopicID, event.TopicName, event.SubforumID)
		if err != nil {
			fmt.Printf("Error updating subforum topic count for character: %v\n", err)
		}
	})

	// Subscriber 12: Update Global Stats on Character Accepted
	Events.Subscribe(Events.CharacterAccepted, func(db *sql.DB, data Events.EventData) {
		_, ok := data.(Events.CharacterAcceptedEvent)
		if !ok {
			return
		}

		_, err := db.Exec("UPDATE global_stats SET stat_value = stat_value + 1 WHERE stat_name = 'total_character_number'")
		if err != nil {
			fmt.Printf("Error updating global character stats on accept: %v\n", err)
		}

	})

	// Subscriber: Update Global Stats on Character Activated
	Events.Subscribe(Events.CharacterActivated, func(db *sql.DB, data Events.EventData) {
		_, ok := data.(Events.CharacterActivatedEvent)
		if !ok {
			return
		}

		_, err := db.Exec("UPDATE global_stats SET stat_value = stat_value + 1 WHERE stat_name = 'total_character_number'")
		if err != nil {
			fmt.Printf("Error updating global character stats on activate: %v\n", err)
		}

	})

	// Subscriber: Update Global Stats on Character Deactivated
	Events.Subscribe(Events.CharacterDeactivated, func(db *sql.DB, data Events.EventData) {
		_, ok := data.(Events.CharacterDeactivatedEvent)
		if !ok {
			return
		}

		_, err := db.Exec("UPDATE global_stats SET stat_value = GREATEST(stat_value - 1, 0) WHERE stat_name = 'total_character_number'")
		if err != nil {
			fmt.Printf("Error updating global character stats on deactivate: %v\n", err)
		}

	})

	// Subscriber 13: Post Welcome Message on Character Accepted
	Events.Subscribe(Events.CharacterAccepted, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.CharacterAcceptedEvent)
		if !ok {
			return
		}

		// Get topic ID for the character
		var topicID int
		err := db.QueryRow("SELECT topic_id FROM character_base WHERE id = ?", event.CharacterID).Scan(&topicID)
		if err != nil {
			fmt.Printf("Error getting topic ID for welcome message: %v\n", err)
			return
		}

		// Insert welcome post from "System" (user_id = 1)
		res, err := db.Exec("INSERT INTO posts (topic_id, author_user_id, content, date_created) VALUES (?, 1, 'Welcome to the game!', NOW())", topicID)
		if err != nil {
			fmt.Printf("Error posting welcome message: %v\n", err)
			return
		}

		postID, _ := res.LastInsertId()

		// Notify anyone reading the topic
		topicIDStr := strconv.Itoa(topicID)
		users := Services.ActivityStorage.GetUsersOnPage("topic", topicIDStr)

		// Fetch full post data for WebSocket
		fullPost, err := Services.GetPostById(int(postID), db, Features.IsCurrencyActive(db))
		if err == nil {
			notification := map[string]interface{}{
				"type": "post_created",
				"data": fullPost,
			}
			for _, u := range users {
				Websockets.MainHub.SendNotification(u.UserID, notification)
			}
		}
	})

	// Subscriber: Award currency on Character Accepted
	Events.Subscribe(Events.CharacterAccepted, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.CharacterAcceptedEvent)
		if !ok {
			return
		}

		if !Features.IsCurrencyActive(db) {
			return
		}

		var amount int
		var isActive bool
		if err := db.QueryRow(
			"SELECT amount, is_active FROM currency_income_types WHERE `key` = 'currency_income_new_character'",
		).Scan(&amount, &isActive); err != nil || !isActive {
			return
		}

		tx, err := db.Begin()
		if err != nil {
			fmt.Printf("Error starting transaction for new character currency: %v\n", err)
			return
		}
		defer tx.Rollback()

		_, err = tx.Exec(
			"INSERT INTO currency_user_account (user_id, amount) VALUES (?, ?) ON DUPLICATE KEY UPDATE amount = amount + ?",
			event.UserID, amount, amount,
		)
		if err != nil {
			fmt.Printf("Error awarding currency for new character: %v\n", err)
			return
		}

		metadataJSON, _ := json.Marshal(map[string]int{"character_id": event.CharacterID})
		_, err = tx.Exec(
			"INSERT INTO currency_user_transactions (user_id, type, amount, datetime, status, income_type_key, metadata) VALUES (?, ?, ?, NOW(), ?, ?, ?)",
			event.UserID, Features.CurrencyTransactionIncome, amount, Features.CurrencyTransactionApproved, "currency_income_new_character", metadataJSON,
		)
		if err != nil {
			fmt.Printf("Error recording currency transaction for new character: %v\n", err)
			return
		}

		if err := tx.Commit(); err != nil {
			fmt.Printf("Error committing currency award for new character: %v\n", err)
			return
		}

		var newTotal int
		_ = db.QueryRow("SELECT amount FROM currency_user_account WHERE user_id = ?", event.UserID).Scan(&newTotal)

		Events.Publish(db, Events.NotificationCreated, Events.NotificationEvent{
			UserID:  event.UserID,
			Type:    "account_update",
			Message: fmt.Sprintf("You earned %d currency for your new character %s", amount, event.CharacterName),
			Data: Entities.NotificationAccountUpdate{
				IncomeTypeKey: "currency_income_new_character",
				Amount:        amount,
				TotalAmount:   newTotal,
			},
		})
	})

	// Subscriber 14: Send System Notification on Character Accepted
	Events.Subscribe(Events.CharacterAccepted, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.CharacterAcceptedEvent)
		if !ok {
			return
		}

		Events.Publish(db, Events.NotificationCreated, Events.NotificationEvent{
			UserID:  event.UserID,
			Type:    "system",
			Message: fmt.Sprintf("Your character %s has been accepted", event.CharacterName),
			Data:    map[string]interface{}{"topic_id": event.TopicID},
		})
	})
}
