package Services

import (
	"database/sql"
	"log"
	"time"
)

// recalculateAbsenceTimerStart recomputes when the archiving countdown begins for a character.
//
// Rules:
//   - Not active → remove from table (no archiving).
//   - No active episodes → timer starts from the later of date_last_post or the most recent
//     episode closure recorded in topic_activity_log.
//   - All active episodes have this character as last poster → no timer (delete from table).
//   - Any active episode has another character as last poster → timer starts from the earliest
//     such post date (the oldest post the character has yet to answer).
func recalculateAbsenceTimerStart(characterID int, db *sql.DB) {
	var charStatus int
	var userID int
	var topicCreatedAt time.Time
	var lastPost *time.Time
	err := db.QueryRow(`
		SELECT cb.character_status, cb.user_id, t.date_created, cb.date_last_post
		FROM character_base cb
		JOIN topics t ON t.id = cb.topic_id
		WHERE cb.id = ?
	`, characterID).Scan(&charStatus, &userID, &topicCreatedAt, &lastPost)
	if err != nil {
		log.Printf("AbsenceTimerStart: failed to load character %d: %v", characterID, err)
		return
	}
	if charStatus != 0 { // not ActiveCharacter
		_, _ = db.Exec("DELETE FROM absence_timer_start WHERE character_id = ?", characterID)
		return
	}

	rows, err := db.Query(`
		SELECT
			p.date_created,
			COALESCE(cpb.character_id = ?, 0) AS is_by_character
		FROM episode_character ec
		JOIN episode_base eb ON ec.episode_id = eb.id
		JOIN (
			SELECT topic_id, MAX(id) AS last_post_id
			FROM posts
			WHERE is_deleted IS NULL OR is_deleted != 1
			GROUP BY topic_id
		) lp ON lp.topic_id = eb.topic_id
		JOIN posts p ON p.id = lp.last_post_id
		LEFT JOIN character_profile_base cpb ON p.character_profile_id = cpb.id
		WHERE ec.character_id = ? AND eb.episode_status = 0
	`, characterID, characterID)
	if err != nil {
		log.Printf("AbsenceTimerStart: failed to query episodes for character %d: %v", characterID, err)
		return
	}

	type episodeInfo struct {
		lastPostDate  time.Time
		isByCharacter bool
	}
	var episodes []episodeInfo
	for rows.Next() {
		var ep episodeInfo
		var isByChar int
		if rows.Scan(&ep.lastPostDate, &isByChar) == nil {
			ep.isByCharacter = isByChar == 1
			episodes = append(episodes, ep)
		}
	}
	rows.Close()

	var startDate time.Time

	if len(episodes) == 0 {
		// No active episodes — base the timer on date_last_post, then floor it at the most
		// recent episode closure date from the activity log so the character always gets a
		// full N-day window from when their last episode actually ended.
		if lastPost != nil {
			startDate = *lastPost
		} else {
			startDate = topicCreatedAt
		}
		var lastClosure *time.Time
		_ = db.QueryRow(`
			SELECT MAX(tal.date)
			FROM topic_activity_log tal
			JOIN episode_base eb ON eb.topic_id = tal.topic_id
			JOIN episode_character ec ON ec.episode_id = eb.id
			WHERE ec.character_id = ?
			  AND tal.event = 'episode_status_changed'
			  AND tal.new_state IN (1, 2)
		`, characterID).Scan(&lastClosure)
		if lastClosure != nil && lastClosure.After(startDate) {
			startDate = *lastClosure
		}
	} else {
		allByCharacter := true
		var earliest *time.Time
		for _, ep := range episodes {
			if !ep.isByCharacter {
				allByCharacter = false
				if earliest == nil || ep.lastPostDate.Before(*earliest) {
					t := ep.lastPostDate
					earliest = &t
				}
			}
		}
		if allByCharacter {
			_, _ = db.Exec("DELETE FROM absence_timer_start WHERE character_id = ?", characterID)
			return
		}
		// Timer starts from the later of: character's own last post, or earliest unanswered post.
		charLastPost := topicCreatedAt
		if lastPost != nil {
			charLastPost = *lastPost
		}
		if charLastPost.After(*earliest) {
			startDate = charLastPost
		} else {
			startDate = *earliest
		}
	}

	startDate = time.Date(startDate.Year(), startDate.Month(), startDate.Day(), 0, 0, 0, 0, startDate.Location())

	var extraDays int
	_ = db.QueryRow(`
		SELECT COALESCE(SUM(
			GREATEST(0, DATEDIFF(LEAST(absence_end_date, NOW()), GREATEST(absence_start_date, ?)) + 1)
		), 0)
		FROM absent_users
		WHERE user_id = ? AND is_deleted = 0
		AND absence_end_date >= ?
		AND absence_start_date <= NOW()
	`, startDate, userID, startDate).Scan(&extraDays)

	_, _ = db.Exec(
		"INSERT INTO absence_timer_start (character_id, start_date, extra_days) VALUES (?, ?, ?) ON DUPLICATE KEY UPDATE start_date = ?, extra_days = ?",
		characterID, startDate, extraDays, startDate, extraDays,
	)
}

// RecalculateAbsenceTimerStart recomputes when the archiving countdown begins for a character.
func RecalculateAbsenceTimerStart(characterID int, db *sql.DB) {
	recalculateAbsenceTimerStart(characterID, db)
}

// RecalculateAbsenceTimerStartForEpisode recalculates the absence timer for all characters
// participating in the given episode.
func RecalculateAbsenceTimerStartForEpisode(episodeID int, db *sql.DB) {
	rows, err := db.Query("SELECT character_id FROM episode_character WHERE episode_id = ?", episodeID)
	if err != nil {
		return
	}
	var charIDs []int
	for rows.Next() {
		var id int
		if rows.Scan(&id) == nil {
			charIDs = append(charIDs, id)
		}
	}
	rows.Close()
	for _, id := range charIDs {
		recalculateAbsenceTimerStart(id, db)
	}
}

// RecalculateAbsenceTimerStartForUser recalculates the absence timer for all active characters
// of the given user.
func RecalculateAbsenceTimerStartForUser(userID int, db *sql.DB) {
	rows, err := db.Query(
		"SELECT id FROM character_base WHERE user_id = ? AND character_status = 0",
		userID,
	)
	if err != nil {
		return
	}
	var charIDs []int
	for rows.Next() {
		var id int
		if rows.Scan(&id) == nil {
			charIDs = append(charIDs, id)
		}
	}
	rows.Close()
	for _, id := range charIDs {
		recalculateAbsenceTimerStart(id, db)
	}
}

// InitializeAbsenceTimerStart backfills absence_timer_start for any active character
// that does not yet have an entry. Safe to call on every startup.
func InitializeAbsenceTimerStart(db *sql.DB) {
	rows, err := db.Query(`
		SELECT cb.id FROM character_base cb
		LEFT JOIN absence_timer_start ats ON ats.character_id = cb.id
		WHERE cb.character_status = 0 AND ats.character_id IS NULL
	`)
	if err != nil {
		log.Printf("AbsenceTimerStart init: failed to query characters: %v", err)
		return
	}
	var charIDs []int
	for rows.Next() {
		var id int
		if rows.Scan(&id) == nil {
			charIDs = append(charIDs, id)
		}
	}
	rows.Close()
	if len(charIDs) == 0 {
		return
	}
	log.Printf("AbsenceTimerStart init: backfilling %d character(s)", len(charIDs))
	for _, id := range charIDs {
		recalculateAbsenceTimerStart(id, db)
	}
}
