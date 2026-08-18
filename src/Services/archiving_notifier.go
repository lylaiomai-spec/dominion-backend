package Services

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Events"
	"database/sql"
	"log"
	"time"
)

var archivingWarningThresholds = []int{10, 5, 3, 2, 1}

func StartArchivingNotifier(db *sql.DB) {
	go func() {
		now := time.Now().UTC()
		next := time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, time.UTC)
		if !next.After(now) {
			next = next.Add(24 * time.Hour)
		}

		time.Sleep(time.Until(next))
		runAutoArchiving(db)
		runArchivingNotifications(db)

		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			runAutoArchiving(db)
			runArchivingNotifications(db)
		}
	}()
}

func runAutoArchiving(db *sql.DB) {
	if !GetAutoArchivingEnabled(db) {
		return
	}

	autoArchivingDays := GetAutoArchivingDays(db)

	rows, err := db.Query(`
		SELECT cb.id, cb.name, cb.user_id
		FROM character_base cb
		JOIN topics t ON t.id = cb.topic_id
		JOIN users u ON u.id = cb.user_id
		LEFT JOIN absent_users au ON au.user_id = cb.user_id
			AND au.absence_start_date <= NOW() AND au.absence_end_date >= NOW() AND au.is_deleted = 0
		LEFT JOIN auto_archiving_immunity aai ON aai.character_id = cb.id
			AND aai.start_date <= NOW() AND aai.end_date >= NOW()
		WHERE cb.character_status = ?
		AND u.user_status = ?
		AND DATEDIFF(NOW(), COALESCE(cb.date_last_post, t.date_created)) >= ?
		AND au.id IS NULL
		AND aai.id IS NULL
	`, Entities.ActiveCharacter, Entities.ActiveUser, autoArchivingDays)
	if err != nil {
		log.Printf("Auto archiving: failed to query characters: %v", err)
		return
	}

	type charRow struct {
		id     int
		name   string
		userID int
	}

	var chars []charRow
	for rows.Next() {
		var ch charRow
		if err := rows.Scan(&ch.id, &ch.name, &ch.userID); err != nil {
			continue
		}
		chars = append(chars, ch)
	}
	rows.Close()

	for _, ch := range chars {
		if _, err := db.Exec(
			"UPDATE character_base SET character_status = ? WHERE id = ?",
			Entities.InactiveCharacter, ch.id,
		); err != nil {
			log.Printf("Auto archiving: failed to deactivate character %d: %v", ch.id, err)
			continue
		}

		_, _ = db.Exec("UPDATE global_stats SET stat_value = GREATEST(stat_value - 1, 0) WHERE stat_name = 'total_character_number'")

		lang := GetUserLanguage(ch.userID, db)
		localizer := NewLocalizer(lang)
		Events.Publish(db, Events.NotificationCreated, Events.NotificationEvent{
			UserID: ch.userID,
			Type:   "auto_archiving",
			Message: TData(localizer, "auto_archiving.deactivated", map[string]interface{}{
				"Name": ch.name,
			}),
			Data: map[string]interface{}{
				"character_id":   ch.id,
				"character_name": ch.name,
			},
		})
	}
}

func runArchivingNotifications(db *sql.DB) {
	if !GetAutoArchivingEnabled(db) {
		return
	}

	autoArchivingDays := GetAutoArchivingDays(db)

	// Warn immune characters whose protection expires at a threshold day and will be immediately archived.
	for _, threshold := range archivingWarningThresholds {
		rows, err := db.Query(`
			SELECT
				cb.id,
				cb.name,
				cb.user_id,
				DATE(aai_exp.end_date) AS base_date
			FROM character_base cb
			JOIN topics t ON t.id = cb.topic_id
			JOIN users u ON u.id = cb.user_id
			JOIN (
				SELECT character_id, MAX(end_date) AS end_date
				FROM auto_archiving_immunity
				WHERE end_date >= NOW()
				GROUP BY character_id
			) aai_exp ON aai_exp.character_id = cb.id
			LEFT JOIN absent_users au ON au.user_id = cb.user_id
				AND au.absence_start_date <= NOW() AND au.absence_end_date >= NOW() AND au.is_deleted = 0
			WHERE cb.character_status = ?
			AND u.user_status = ?
			AND DATEDIFF(aai_exp.end_date, NOW()) = ?
			AND ? - DATEDIFF(aai_exp.end_date, COALESCE(cb.date_last_post, t.date_created)) <= 0
			AND au.id IS NULL
		`, Entities.ActiveCharacter, Entities.ActiveUser, threshold, autoArchivingDays)
		if err != nil {
			log.Printf("Archiving notifier (immunity expiry): failed to query for threshold %d: %v", threshold, err)
			continue
		}

		type charRow struct {
			id       int
			name     string
			userID   int
			baseDate string
		}

		var chars []charRow
		for rows.Next() {
			var ch charRow
			if err := rows.Scan(&ch.id, &ch.name, &ch.userID, &ch.baseDate); err != nil {
				continue
			}
			chars = append(chars, ch)
		}
		rows.Close()

		for _, ch := range chars {
			var count int
			if err := db.QueryRow(
				"SELECT COUNT(*) FROM archiving_warning_log WHERE character_id = ? AND days_warning = ? AND base_date = ?",
				ch.id, threshold, ch.baseDate,
			).Scan(&count); err != nil || count > 0 {
				continue
			}

			lang := GetUserLanguage(ch.userID, db)
			localizer := NewLocalizer(lang)
			Events.Publish(db, Events.NotificationCreated, Events.NotificationEvent{
				UserID: ch.userID,
				Type:   "auto_archiving",
				Message: TPlural(localizer, "auto_archiving.immunity_expiry_warning", threshold, map[string]interface{}{
					"Name":  ch.name,
					"Count": threshold,
				}),
				Data: map[string]interface{}{
					"character_id":   ch.id,
					"character_name": ch.name,
					"days_left":      threshold,
				},
			})

			if _, err := db.Exec(
				"INSERT INTO archiving_warning_log (character_id, days_warning, base_date) VALUES (?, ?, ?)",
				ch.id, threshold, ch.baseDate,
			); err != nil {
				log.Printf("Archiving notifier (immunity expiry): failed to log notification for character %d: %v", ch.id, err)
			}
		}
	}

	for _, threshold := range archivingWarningThresholds {
		rows, err := db.Query(`
			SELECT
				cb.id,
				cb.name,
				cb.user_id,
				DATE(COALESCE(cb.date_last_post, t.date_created)) AS base_date
			FROM character_base cb
			JOIN topics t ON t.id = cb.topic_id
			JOIN users u ON u.id = cb.user_id
			LEFT JOIN absent_users au ON au.user_id = cb.user_id
				AND au.absence_start_date <= NOW() AND au.absence_end_date >= NOW() AND au.is_deleted = 0
			LEFT JOIN auto_archiving_immunity aai ON aai.character_id = cb.id
				AND aai.start_date <= NOW() AND aai.end_date >= NOW()
			WHERE cb.character_status = ?
			AND u.user_status = ?
			AND ? - DATEDIFF(NOW(), COALESCE(cb.date_last_post, t.date_created)) = ?
			AND au.id IS NULL
			AND aai.id IS NULL
		`, Entities.ActiveCharacter, Entities.ActiveUser, autoArchivingDays, threshold)
		if err != nil {
			log.Printf("Archiving notifier: failed to query characters for threshold %d: %v", threshold, err)
			continue
		}

		type charRow struct {
			id       int
			name     string
			userID   int
			baseDate string
		}

		var chars []charRow
		for rows.Next() {
			var ch charRow
			if err := rows.Scan(&ch.id, &ch.name, &ch.userID, &ch.baseDate); err != nil {
				continue
			}
			chars = append(chars, ch)
		}
		rows.Close()

		for _, ch := range chars {
			var count int
			if err := db.QueryRow(
				"SELECT COUNT(*) FROM archiving_warning_log WHERE character_id = ? AND days_warning = ? AND base_date = ?",
				ch.id, threshold, ch.baseDate,
			).Scan(&count); err != nil || count > 0 {
				continue
			}

			lang := GetUserLanguage(ch.userID, db)
			localizer := NewLocalizer(lang)
			Events.Publish(db, Events.NotificationCreated, Events.NotificationEvent{
				UserID: ch.userID,
				Type:   "auto_archiving",
				Message: TPlural(localizer, "auto_archiving.archiving_warning", threshold, map[string]interface{}{
					"Name":  ch.name,
					"Count": threshold,
				}),
				Data: map[string]interface{}{
					"character_id":   ch.id,
					"character_name": ch.name,
					"days_left":      threshold,
				},
			})

			if _, err := db.Exec(
				"INSERT INTO archiving_warning_log (character_id, days_warning, base_date) VALUES (?, ?, ?)",
				ch.id, threshold, ch.baseDate,
			); err != nil {
				log.Printf("Archiving notifier: failed to log notification for character %d: %v", ch.id, err)
			}
		}
	}
}
