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

func RegisterPostEventHandlers() {
	// Subscriber 5: Notify New Post in Topic
	Events.Subscribe(Events.PostCreated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.PostCreatedEvent)
		if !ok {
			return
		}

		// Handle both post_created and post_updated
		msgType := event.Type
		if msgType == "" {
			msgType = "post_created"
		}

		// Get all users currently reading this topic
		topicIDStr := strconv.FormatInt(event.TopicID, 10)
		users := Services.ActivityStorage.GetUsersOnPage("topic", topicIDStr)

		// Send to each user on the page with their localized date and per-user CanEdit
		for _, u := range users {
			userPost := event.Post
			userPost.DateCreatedLocalized = Services.LocalizeTime(userPost.DateCreated, Services.GetUserTimezone(u.UserID, db))

			canEdit := false
			if u.UserID != 0 {
				if u.UserID == userPost.AuthorUserId {
					perm := fmt.Sprintf("subforum_edit_own_post:%d", event.SubforumID)
					if hasPerm, err := Services.HasPermission(u.UserID, perm, db); err == nil && hasPerm {
						canEdit = true
					}
				} else {
					perm := fmt.Sprintf("subforum_edit_others_post:%d", event.SubforumID)
					if hasPerm, err := Services.HasPermission(u.UserID, perm, db); err == nil && hasPerm {
						canEdit = true
					}
				}
			}
			userPost.CanEdit = &canEdit

			Websockets.MainHub.SendNotification(u.UserID, map[string]interface{}{
				"type": msgType,
				"data": userPost,
			})
		}
	})

	// Subscriber 6: Update Stats on Post Created
	Events.Subscribe(Events.PostCreated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.PostCreatedEvent)
		if !ok || event.Type == "post_updated" {
			return
		}

		// 1. Update Global Stats
		_, err := db.Exec("UPDATE global_stats SET stat_value = stat_value + 1 WHERE stat_name = 'total_post_number'")
		if err != nil {
			fmt.Printf("Error updating global post stats: %v\n", err)
		}

		// Check if topic is an episode
		var topicType Entities.TopicType
		err = db.QueryRow("SELECT type FROM topics WHERE id = ?", event.TopicID).Scan(&topicType)
		if err == nil && topicType == Entities.EpisodeTopic {
			_, err = db.Exec("UPDATE global_stats SET stat_value = stat_value + 1 WHERE stat_name = 'total_episode_post_number'")
			if err != nil {
				fmt.Printf("Error updating global episode post stats: %v\n", err)
			}
		}

		// 2. Update Topic Stats — also set status to Full when the post cap is reached
		_, err = db.Exec(`
			UPDATE topics
			SET post_number = post_number + 1,
			    date_last_post = NOW(),
			    last_post_author_user_id = ?,
			    status = CASE WHEN post_number + 1 >= ? THEN ? ELSE status END
			WHERE id = ?`,
			event.Post.AuthorUserId, Entities.TopicPostCap, Entities.FullTopic, event.TopicID)
		if err != nil {
			fmt.Printf("Error updating topic stats: %v\n", err)
		}

		// 3. Update Subforum Stats
		var username string
		err = db.QueryRow("SELECT username FROM users WHERE id = ?", event.Post.AuthorUserId).Scan(&username)
		if err != nil {
			fmt.Printf("Error fetching username for stats: %v\n", err)
			return
		}

		var topicTitle string
		err = db.QueryRow("SELECT name FROM topics WHERE id = ?", event.TopicID).Scan(&topicTitle)
		if err != nil {
			fmt.Printf("Error fetching topic title for stats: %v\n", err)
			return
		}

		_, err = db.Exec("UPDATE subforums SET post_number = COALESCE(post_number, 0) + 1, last_post_topic_id = ?, last_post_topic_name = ?, last_post_id = ?, date_last_post = NOW(), last_post_author_user_name = ?, show_last_topic = false WHERE id = ?",
			event.TopicID, topicTitle, event.Post.Id, username, event.SubforumID)
		if err != nil {
			fmt.Printf("Error updating subforum stats: %v\n", err)
		}
	})

	// Subscriber: Update character stats on episode post created
	Events.Subscribe(Events.PostCreated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.PostCreatedEvent)
		if !ok || event.Type == "post_updated" {
			return
		}

		if event.Post.CharacterProfile == nil || event.Post.CharacterProfile.CharacterId == nil {
			return
		}

		var topicType Entities.TopicType
		err := db.QueryRow("SELECT type FROM topics WHERE id = ?", event.TopicID).Scan(&topicType)
		if err != nil || topicType != Entities.EpisodeTopic {
			return
		}

		_, err = db.Exec(
			"UPDATE character_base SET total_posts = total_posts + 1, date_last_post = ? WHERE id = ?",
			event.Post.DateCreated, *event.Post.CharacterProfile.CharacterId,
		)
		if err != nil {
			fmt.Printf("Error updating character stats: %v\n", err)
		}
	})

	// Subscriber: Update user post counts on post created
	Events.Subscribe(Events.PostCreated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.PostCreatedEvent)
		if !ok || event.Type == "post_updated" {
			return
		}

		var topicType Entities.TopicType
		err := db.QueryRow("SELECT type FROM topics WHERE id = ?", event.TopicID).Scan(&topicType)
		if err != nil {
			fmt.Printf("Error fetching topic type for user post count: %v\n", err)
			return
		}

		if topicType == Entities.EpisodeTopic {
			_, err = db.Exec("UPDATE users SET total_posts = total_posts + 1 WHERE id = ?", event.Post.AuthorUserId)
		} else {
			_, err = db.Exec("UPDATE users SET total_general_posts = total_general_posts + 1 WHERE id = ?", event.Post.AuthorUserId)
		}
		if err != nil {
			fmt.Printf("Error updating user post count: %v\n", err)
		}
	})

	// Subscriber: Award currency for post milestones (100/500/1000)
	Events.Subscribe(Events.PostCreated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.PostCreatedEvent)
		if !ok || event.Type == "post_updated" || event.Post.AuthorUserId == 0 {
			return
		}

		if !Features.IsCurrencyActive(db) {
			return
		}

		var topicType Entities.TopicType
		if err := db.QueryRow("SELECT type FROM topics WHERE id = ?", event.TopicID).Scan(&topicType); err != nil {
			return
		}

		type milestone struct {
			threshold int
			key       string
		}

		var postCount int
		var milestones []milestone

		if topicType == Entities.EpisodeTopic {
			if err := db.QueryRow("SELECT total_posts FROM users WHERE id = ?", event.Post.AuthorUserId).Scan(&postCount); err != nil {
				return
			}
			milestones = []milestone{
				{1000, "currency_income_1000_game_posts"},
				{500, "currency_income_500_game_posts"},
				{100, "currency_income_100_game_posts"},
			}
		} else {
			if err := db.QueryRow("SELECT total_general_posts FROM users WHERE id = ?", event.Post.AuthorUserId).Scan(&postCount); err != nil {
				return
			}
			milestones = []milestone{
				{1000, "currency_income_1000_general_posts"},
				{500, "currency_income_500_general_posts"},
				{100, "currency_income_100_general_posts"},
			}
		}

		for _, m := range milestones {
			if postCount == 0 || postCount%m.threshold != 0 {
				continue
			}
			var amount int
			var isActive bool
			err := db.QueryRow("SELECT amount, is_active FROM currency_income_types WHERE `key` = ?", m.key).Scan(&amount, &isActive)
			if err != nil || !isActive {
				continue
			}

			tx, err := db.Begin()
			if err != nil {
				fmt.Printf("Error starting milestone transaction: %v\n", err)
				return
			}
			defer tx.Rollback()

			_, err = tx.Exec(
				"INSERT INTO currency_user_account (user_id, amount) VALUES (?, ?) ON DUPLICATE KEY UPDATE amount = amount + ?",
				event.Post.AuthorUserId, amount, amount,
			)
			if err != nil {
				fmt.Printf("Error awarding milestone currency: %v\n", err)
				return
			}

			metadataJSON, _ := json.Marshal(map[string]int{"post_count": postCount})
			_, err = tx.Exec(
				"INSERT INTO currency_user_transactions (user_id, type, amount, datetime, status, income_type_key, metadata) VALUES (?, ?, ?, NOW(), ?, ?, ?)",
				event.Post.AuthorUserId, Features.CurrencyTransactionIncome, amount, Features.CurrencyTransactionApproved, m.key, metadataJSON,
			)
			if err != nil {
				fmt.Printf("Error writing milestone transaction: %v\n", err)
				return
			}

			if err := tx.Commit(); err != nil {
				fmt.Printf("Error committing milestone transaction: %v\n", err)
				return
			}

			var newTotal int
			_ = db.QueryRow("SELECT amount FROM currency_user_account WHERE user_id = ?", event.Post.AuthorUserId).Scan(&newTotal)

			Events.Publish(db, Events.NotificationCreated, Events.NotificationEvent{
				UserID:  event.Post.AuthorUserId,
				Type:    "account_update",
				Message: fmt.Sprintf("You earned %d currency for reaching %d posts", amount, m.threshold),
				Data: Entities.NotificationAccountUpdate{
					IncomeTypeKey: m.key,
					Amount:        amount,
					TotalAmount:   newTotal,
					PostId:        event.Post.Id,
					TopicId:       int(event.TopicID),
				},
			})
			break // Only award the highest applicable milestone
		}
	})

	// Subscriber 11: Send Game Notifications for Episode Posts
	Events.Subscribe(Events.PostCreated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.PostCreatedEvent)
		if !ok || event.Type == "post_updated" {
			return
		}

		// 1. Check if topic type is episode
		var topicType Entities.TopicType
		var topicTitle string
		err := db.QueryRow("SELECT type, name FROM topics WHERE id = ?", event.TopicID).Scan(&topicType, &topicTitle)
		if err != nil || topicType != Entities.EpisodeTopic {
			return
		}

		// 2. Get author character ID
		var authorCharacterID int
		if event.Post.CharacterProfile != nil && event.Post.CharacterProfile.CharacterId != nil {
			authorCharacterID = *event.Post.CharacterProfile.CharacterId
		} else {
			return
		}

		// 3. Get all participants of the episode
		query := `
			SELECT cb.user_id, cb.id as character_id, cb.name as character_name
			FROM character_base cb
			JOIN episode_character ec ON cb.id = ec.character_id
			JOIN episode_base e ON ec.episode_id = e.id
			WHERE e.topic_id = ? AND cb.user_id != ?
		`
		rows, err := db.Query(query, event.TopicID, event.Post.AuthorUserId)
		if err != nil {
			fmt.Printf("Error fetching episode participants: %v\n", err)
			return
		}
		defer rows.Close()

		authorCharacterName := ""
		if event.Post.CharacterProfile != nil {
			authorCharacterName = event.Post.CharacterProfile.CharacterName
		}

		for rows.Next() {
			var participantUserID int
			var participantCharacterID int
			var participantCharacterName string
			if err := rows.Scan(&participantUserID, &participantCharacterID, &participantCharacterName); err == nil {
				gameData := Entities.NotificationGame{
					TopicId:           int(event.TopicID),
					TopicName:         topicTitle,
					PostId:            event.Post.Id,
					Type:              "post_created",
					UserCharacterId:   participantCharacterID,
					UserCharacterName: participantCharacterName,
					CharacterId:       authorCharacterID,
					CharacterName:     authorCharacterName,
				}

				Events.Publish(db, Events.NotificationCreated, Events.NotificationEvent{
					UserID:  participantUserID,
					Type:    "game",
					Message: fmt.Sprintf("New [post in episode %s] by %s", topicTitle, event.Post.CharacterProfile.CharacterName),
					Data:    gameData,
				})
			}
		}
	})

	// Subscriber: Decrement stats on post deleted
	Events.Subscribe(Events.PostCreated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.PostCreatedEvent)
		if !ok || event.Type != "post_deleted" {
			return
		}

		// 1. Determine topic type.
		var topicType Entities.TopicType
		if err := db.QueryRow("SELECT type FROM topics WHERE id = ?", event.TopicID).Scan(&topicType); err != nil {
			fmt.Printf("Error fetching topic type on post delete: %v\n", err)
			return
		}

		// 2. Decrement global stats.
		_, _ = db.Exec("UPDATE global_stats SET stat_value = GREATEST(stat_value - 1, 0) WHERE stat_name = 'total_post_number'")
		if topicType == Entities.EpisodeTopic {
			_, _ = db.Exec("UPDATE global_stats SET stat_value = GREATEST(stat_value - 1, 0) WHERE stat_name = 'total_episode_post_number'")
		}

		// 3. Recalculate topic stats (post count + last post).
		_, _ = db.Exec(`
			UPDATE topics SET
				post_number            = (SELECT COUNT(*) FROM posts WHERE topic_id = ? AND COALESCE(is_deleted, 0) != 1),
				date_last_post         = (SELECT MAX(date_created) FROM posts WHERE topic_id = ? AND COALESCE(is_deleted, 0) != 1),
				last_post_author_user_id = (SELECT author_user_id FROM posts WHERE topic_id = ? AND COALESCE(is_deleted, 0) != 1 ORDER BY date_created DESC LIMIT 1)
			WHERE id = ?`,
			event.TopicID, event.TopicID, event.TopicID, event.TopicID)

		// 4. Recalculate subforum stats (post count + last post).
		refreshSubforumStats(db, event.SubforumID)

		// 5. Decrement author's post count.
		if topicType == Entities.EpisodeTopic {
			_, _ = db.Exec("UPDATE users SET total_posts = GREATEST(total_posts - 1, 0) WHERE id = ?", event.Post.AuthorUserId)
		} else {
			_, _ = db.Exec("UPDATE users SET total_general_posts = GREATEST(total_general_posts - 1, 0) WHERE id = ?", event.Post.AuthorUserId)
		}

		// 6. Decrement character post count (episode posts only).
		if topicType == Entities.EpisodeTopic {
			var characterID int64
			err := db.QueryRow(`
				SELECT cpb.character_id FROM posts p
				JOIN character_profile_base cpb ON cpb.id = p.character_profile_id
				WHERE p.id = ?`, event.Post.Id).Scan(&characterID)
			if err == nil && characterID > 0 {
				_, _ = db.Exec("UPDATE character_base SET total_posts = GREATEST(total_posts - 1, 0) WHERE id = ?", characterID)
			}
		}
	})

	// Subscriber: Update mask stats on episode post created
	Events.Subscribe(Events.PostCreated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.PostCreatedEvent)
		if !ok || event.Type == "post_updated" {
			return
		}

		if event.Post.CharacterProfile == nil || event.Post.CharacterProfile.IsMask == nil || !*event.Post.CharacterProfile.IsMask {
			return
		}

		var topicType Entities.TopicType
		if err := db.QueryRow("SELECT type FROM topics WHERE id = ?", event.TopicID).Scan(&topicType); err != nil || topicType != Entities.EpisodeTopic {
			return
		}

		_, err := db.Exec(`
			INSERT INTO mask_stats (user_id, total_posts, date_last_post)
			VALUES (?, 1, ?)
			ON DUPLICATE KEY UPDATE
				total_posts    = total_posts + 1,
				date_last_post = ?
		`, event.Post.AuthorUserId, event.Post.DateCreated, event.Post.DateCreated)
		if err != nil {
			fmt.Printf("Error updating mask stats: %v\n", err)
		}

		_, err = db.Exec("UPDATE users SET total_posts = total_posts + 1 WHERE id = ?", event.Post.AuthorUserId)
		if err != nil {
			fmt.Printf("Error updating user total_posts: %v\n", err)
		}
	})

	// Subscriber: Award currency for episode post
	Events.Subscribe(Events.PostCreated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.PostCreatedEvent)
		if !ok || event.Type == "post_updated" {
			return
		}

		if event.Post.AuthorUserId == 0 {
			return
		}

		var topicType Entities.TopicType
		if err := db.QueryRow("SELECT type FROM topics WHERE id = ?", event.TopicID).Scan(&topicType); err != nil || topicType != Entities.EpisodeTopic {
			return
		}

		if !Features.IsCurrencyActive(db) {
			return
		}

		var amount int
		var isActive bool
		err := db.QueryRow(
			"SELECT amount, is_active FROM currency_income_types WHERE `key` = 'currency_income_game_post'",
		).Scan(&amount, &isActive)
		if err != nil || !isActive {
			return
		}

		tx, err := db.Begin()
		if err != nil {
			fmt.Printf("Error starting transaction for currency award: %v\n", err)
			return
		}
		defer tx.Rollback()

		_, err = tx.Exec(`
			INSERT INTO currency_user_account (user_id, amount) VALUES (?, ?)
			ON DUPLICATE KEY UPDATE amount = amount + ?
		`, event.Post.AuthorUserId, amount, amount)
		if err != nil {
			fmt.Printf("Error awarding currency for episode post: %v\n", err)
			return
		}

		metadataJSON, _ := json.Marshal(map[string]int{
			"topic_id": int(event.TopicID),
			"post_id":  event.Post.Id,
		})
		_, err = tx.Exec(`
			INSERT INTO currency_user_transactions (user_id, type, amount, datetime, status, income_type_key, metadata)
			VALUES (?, ?, ?, NOW(), ?, ?, ?)
		`, event.Post.AuthorUserId, Features.CurrencyTransactionIncome, amount, Features.CurrencyTransactionApproved, "currency_income_game_post", metadataJSON)
		if err != nil {
			fmt.Printf("Error writing currency transaction record: %v\n", err)
			return
		}

		if err := tx.Commit(); err != nil {
			fmt.Printf("Error committing currency award transaction: %v\n", err)
			return
		}

		var newTotal int
		_ = db.QueryRow("SELECT amount FROM currency_user_account WHERE user_id = ?", event.Post.AuthorUserId).Scan(&newTotal)

		Events.Publish(db, Events.NotificationCreated, Events.NotificationEvent{
			UserID:  event.Post.AuthorUserId,
			Type:    "account_update",
			Message: fmt.Sprintf("You earned %d currency for your episode post", amount),
			Data: Entities.NotificationAccountUpdate{
				IncomeTypeKey: "currency_income_game_post",
				Amount:        amount,
				TotalAmount:   newTotal,
				PostId:        event.Post.Id,
				TopicId:       int(event.TopicID),
			},
		})
	})

	// Subscriber: Award currency for mask post
	Events.Subscribe(Events.PostCreated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.PostCreatedEvent)
		if !ok || event.Type == "post_updated" {
			return
		}

		if event.Post.AuthorUserId == 0 {
			return
		}

		if event.Post.CharacterProfile == nil || event.Post.CharacterProfile.IsMask == nil || !*event.Post.CharacterProfile.IsMask {
			return
		}

		var topicType Entities.TopicType
		if err := db.QueryRow("SELECT type FROM topics WHERE id = ?", event.TopicID).Scan(&topicType); err != nil || topicType != Entities.EpisodeTopic {
			return
		}

		if !Features.IsCurrencyActive(db) {
			return
		}

		var amount int
		var isActive bool
		err := db.QueryRow(
			"SELECT amount, is_active FROM currency_income_types WHERE `key` = 'currency_income_game_post'",
		).Scan(&amount, &isActive)
		if err != nil || !isActive {
			return
		}

		tx, err := db.Begin()
		if err != nil {
			fmt.Printf("Error starting transaction for mask post currency award: %v\n", err)
			return
		}
		defer tx.Rollback()

		_, err = tx.Exec(`
			INSERT INTO currency_user_account (user_id, amount) VALUES (?, ?)
			ON DUPLICATE KEY UPDATE amount = amount + ?
		`, event.Post.AuthorUserId, amount, amount)
		if err != nil {
			fmt.Printf("Error awarding currency for mask post: %v\n", err)
			return
		}

		metadataJSON, _ := json.Marshal(map[string]int{
			"topic_id": int(event.TopicID),
			"post_id":  event.Post.Id,
		})
		_, err = tx.Exec(`
			INSERT INTO currency_user_transactions (user_id, type, amount, datetime, status, income_type_key, metadata)
			VALUES (?, ?, ?, NOW(), ?, ?, ?)
		`, event.Post.AuthorUserId, Features.CurrencyTransactionIncome, amount, Features.CurrencyTransactionApproved, "currency_income_game_post", metadataJSON)
		if err != nil {
			fmt.Printf("Error writing mask post currency transaction: %v\n", err)
			return
		}

		if err := tx.Commit(); err != nil {
			fmt.Printf("Error committing mask post currency award: %v\n", err)
			return
		}

		var newTotal int
		_ = db.QueryRow("SELECT amount FROM currency_user_account WHERE user_id = ?", event.Post.AuthorUserId).Scan(&newTotal)

		Events.Publish(db, Events.NotificationCreated, Events.NotificationEvent{
			UserID:  event.Post.AuthorUserId,
			Type:    "account_update",
			Message: fmt.Sprintf("You earned %d currency for your mask post", amount),
			Data: Entities.NotificationAccountUpdate{
				IncomeTypeKey: "currency_income_game_post",
				Amount:        amount,
				TotalAmount:   newTotal,
				PostId:        event.Post.Id,
				TopicId:       int(event.TopicID),
			},
		})
	})
}
