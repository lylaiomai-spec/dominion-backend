package EventHandlers

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Events"
	"cuento-backend/src/Features"
	"database/sql"
	"encoding/json"
	"fmt"
)

func RegisterWantedCharacterEventHandlers() {
	Events.Subscribe(Events.WantedCharacterCreated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.WantedCharacterCreatedEvent)
		if !ok {
			return
		}

		var username string
		_ = db.QueryRow("SELECT username FROM users WHERE id = ?", event.AuthorUserID).Scan(&username)
		_, err := db.Exec("UPDATE subforums SET topic_number = COALESCE(topic_number, 0) + 1, show_last_topic = true, last_post_topic_id = ?, last_post_topic_name = ?, date_last_post = NOW(), last_post_author_user_name = ? WHERE id = ?", event.TopicID, event.TopicName, username, event.SubforumID)
		if err != nil {
			fmt.Printf("Error updating subforum stats for wanted character: %v\n", err)
		}
	})

	// Subscriber: Award currency for creating a wanted character topic
	Events.Subscribe(Events.WantedCharacterCreated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.WantedCharacterCreatedEvent)
		if !ok || event.AuthorUserID == 0 {
			return
		}

		if !Features.IsCurrencyActive(db) {
			return
		}

		var amount int
		var isActive bool
		err := db.QueryRow(
			"SELECT amount, is_active FROM currency_income_types WHERE `key` = 'currency_income_wanted_character'",
		).Scan(&amount, &isActive)
		if err != nil || !isActive {
			return
		}

		tx, err := db.Begin()
		if err != nil {
			fmt.Printf("Error starting transaction for wanted character award: %v\n", err)
			return
		}
		defer tx.Rollback()

		_, err = tx.Exec(
			"INSERT INTO currency_user_account (user_id, amount) VALUES (?, ?) ON DUPLICATE KEY UPDATE amount = amount + ?",
			event.AuthorUserID, amount, amount,
		)
		if err != nil {
			fmt.Printf("Error awarding currency for wanted character: %v\n", err)
			return
		}

		metadataJSON, _ := json.Marshal(map[string]interface{}{
			"topic_id":            event.TopicID,
			"wanted_character_id": event.WantedCharacterID,
		})
		_, err = tx.Exec(
			"INSERT INTO currency_user_transactions (user_id, type, amount, datetime, status, income_type_key, metadata) VALUES (?, ?, ?, NOW(), ?, ?, ?)",
			event.AuthorUserID, Features.CurrencyTransactionIncome, amount, Features.CurrencyTransactionApproved, "currency_income_wanted_character", metadataJSON,
		)
		if err != nil {
			fmt.Printf("Error writing wanted character transaction: %v\n", err)
			return
		}

		if err := tx.Commit(); err != nil {
			fmt.Printf("Error committing wanted character transaction: %v\n", err)
			return
		}

		var newTotal int
		_ = db.QueryRow("SELECT amount FROM currency_user_account WHERE user_id = ?", event.AuthorUserID).Scan(&newTotal)

		Events.Publish(db, Events.NotificationCreated, Events.NotificationEvent{
			UserID:  event.AuthorUserID,
			Type:    "account_update",
			Message: fmt.Sprintf("You earned %d currency for creating a wanted character", amount),
			Data: Entities.NotificationAccountUpdate{
				IncomeTypeKey: "currency_income_wanted_character",
				Amount:        amount,
				TotalAmount:   newTotal,
				TopicId:       int(event.TopicID),
			},
		})
	})
}
