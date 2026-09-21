package Controllers

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Middlewares"
	"database/sql"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// parseDateRange parses date_from and date_to query params (YYYY-MM-DD).
// dateTo is extended to 23:59:59 so the full day is included.
// Returns false and aborts the request on any parse error.
func parseDateRange(c *gin.Context) (dateFrom, dateTo time.Time, ok bool) {
	var err error
	dateFrom, err = time.Parse("2006-01-02", c.Query("date_from"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid date_from: expected YYYY-MM-DD"})
		c.Abort()
		return
	}
	dateTo, err = time.Parse("2006-01-02", c.Query("date_to"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid date_to: expected YYYY-MM-DD"})
		c.Abort()
		return
	}
	dateTo = dateTo.Add(24*time.Hour - time.Second)
	ok = true
	return
}

type OverallStatsResponse struct {
	CreatedEpisodes         int `json:"created_episodes"`
	FinishedEpisodes        int `json:"finished_episodes"`
	InactivatedEpisodes     int `json:"inactivated_episodes"`
	TotalPosts              int `json:"total_posts"`
	CreatedWantedCharacters int `json:"created_wanted_characters"`
	AcceptedCharacters      int `json:"accepted_characters"`
}

func GetOverallStats(c *gin.Context, db *sql.DB) {
	dateFrom, dateTo, ok := parseDateRange(c)
	if !ok {
		return
	}

	var stats OverallStatsResponse

	_ = db.QueryRow(`
		SELECT COUNT(*) FROM episode_base eb
		JOIN topics t ON t.id = eb.topic_id
		WHERE t.date_created BETWEEN ? AND ?
	`, dateFrom, dateTo).Scan(&stats.CreatedEpisodes)

	_ = db.QueryRow(`
		SELECT COUNT(DISTINCT topic_id) FROM topic_activity_log
		WHERE event = 'episode_status_changed'
		  AND new_state = ?
		  AND date BETWEEN ? AND ?
	`, int(Entities.FinishedEpisode), dateFrom, dateTo).Scan(&stats.FinishedEpisodes)

	_ = db.QueryRow(`
		SELECT COUNT(DISTINCT topic_id) FROM topic_activity_log
		WHERE event = 'episode_status_changed'
		  AND new_state = ?
		  AND date BETWEEN ? AND ?
	`, int(Entities.InactiveEpisode), dateFrom, dateTo).Scan(&stats.InactivatedEpisodes)

	_ = db.QueryRow(`
		SELECT COUNT(*) FROM posts p
		JOIN episode_base eb ON eb.topic_id = p.topic_id
		WHERE p.date_created BETWEEN ? AND ?
		  AND (p.is_deleted IS NULL OR p.is_deleted != 1)
	`, dateFrom, dateTo).Scan(&stats.TotalPosts)

	_ = db.QueryRow(`
		SELECT COUNT(*) FROM wanted_character_base wb
		JOIN topics t ON t.id = wb.topic_id
		WHERE t.date_created BETWEEN ? AND ?
	`, dateFrom, dateTo).Scan(&stats.CreatedWantedCharacters)

	_ = db.QueryRow(`
		SELECT COUNT(DISTINCT topic_id) FROM topic_activity_log
		WHERE event = 'character_status_changed'
		  AND new_state = ?
		  AND date BETWEEN ? AND ?
	`, int(Entities.ActiveCharacter), dateFrom, dateTo).Scan(&stats.AcceptedCharacters)

	c.JSON(http.StatusOK, stats)
}

type TopWriterEntry struct {
	UserID    int    `json:"user_id"`
	Username  string `json:"username"`
	PostCount int    `json:"post_count"`
}

type TopCharacterEntry struct {
	CharacterID *int   `json:"character_id"`
	Name        string `json:"name"`
	IsMask      bool   `json:"is_mask"`
	PostCount   int    `json:"post_count"`
}

type DailyPostCount struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

type FactionEpisodeStats struct {
	FactionID int    `json:"faction_id"`
	Name      string `json:"name"`
	Created   int    `json:"created"`
	Finished  int    `json:"finished"`
	Archived  int    `json:"archived"`
}

type FactionPostStats struct {
	FactionID int    `json:"faction_id"`
	Name      string `json:"name"`
	PostCount int    `json:"post_count"`
}

func GetWritingActivity(c *gin.Context, db *sql.DB) {
	dateFrom, dateToInclusive, ok := parseDateRange(c)
	if !ok {
		return
	}
	// Keep the raw date (without time component) for the day-loop upper bound.
	dateTo, _ := time.Parse("2006-01-02", c.Query("date_to"))

	rows, err := db.Query(`
		SELECT DATE(p.date_created) AS day, COUNT(*) AS post_count
		FROM posts p
		JOIN episode_base eb ON eb.topic_id = p.topic_id
		WHERE p.date_created BETWEEN ? AND ?
		  AND (p.is_deleted IS NULL OR p.is_deleted != 1)
		GROUP BY day
		ORDER BY day
	`, dateFrom, dateToInclusive)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to query writing activity"})
		c.Abort()
		return
	}
	defer rows.Close()

	postsByDay := make(map[string]int)
	for rows.Next() {
		var day time.Time
		var count int
		if rows.Scan(&day, &count) == nil {
			postsByDay[day.Format("2006-01-02")] = count
		}
	}

	// Build a contiguous slice covering every day in the range, filling zeros.
	var result []DailyPostCount
	for d := dateFrom; !d.After(dateTo); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		result = append(result, DailyPostCount{Date: key, Count: postsByDay[key]})
	}

	c.JSON(http.StatusOK, result)
}

func GetTopWriters(c *gin.Context, db *sql.DB) {
	dateFrom, dateToInclusive, ok := parseDateRange(c)
	if !ok {
		return
	}

	rows, err := db.Query(`
		SELECT p.author_user_id, u.username, COUNT(p.id) AS post_count
		FROM posts p
		JOIN episode_base eb ON eb.topic_id = p.topic_id
		JOIN users u ON u.id = p.author_user_id
		WHERE p.date_created BETWEEN ? AND ?
		  AND (p.is_deleted IS NULL OR p.is_deleted != 1)
		GROUP BY p.author_user_id, u.username
		ORDER BY post_count DESC
		LIMIT 10
	`, dateFrom, dateToInclusive)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to query top writers"})
		c.Abort()
		return
	}
	defer rows.Close()

	result := []TopWriterEntry{}
	for rows.Next() {
		var entry TopWriterEntry
		if rows.Scan(&entry.UserID, &entry.Username, &entry.PostCount) == nil {
			result = append(result, entry)
		}
	}

	c.JSON(http.StatusOK, result)
}

func GetTopCharacters(c *gin.Context, db *sql.DB) {
	dateFrom, dateToInclusive, ok := parseDateRange(c)
	if !ok {
		return
	}

	// Group by character_id for regular characters (aggregates all profiles of the same
	// character) and by profile_id for masks (each mask is its own distinct identity).
	rows, err := db.Query(`
		SELECT
			cb.id AS character_id,
			COALESCE(cb.name, cpb.mask_name) AS name,
			COALESCE(cpb.is_mask, false) AS is_mask,
			COUNT(p.id) AS post_count
		FROM posts p
		JOIN episode_base eb ON eb.topic_id = p.topic_id
		JOIN character_profile_base cpb ON cpb.id = p.character_profile_id
		LEFT JOIN character_base cb ON cb.id = cpb.character_id
		WHERE p.date_created BETWEEN ? AND ?
		  AND p.use_character_profile = true
		  AND (p.is_deleted IS NULL OR p.is_deleted != 1)
		GROUP BY
			CASE WHEN COALESCE(cpb.is_mask, false) THEN cpb.id ELSE cb.id END,
			name,
			is_mask
		ORDER BY post_count DESC
		LIMIT 10
	`, dateFrom, dateToInclusive)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to query top characters"})
		c.Abort()
		return
	}
	defer rows.Close()

	result := []TopCharacterEntry{}
	for rows.Next() {
		var entry TopCharacterEntry
		var characterID sql.NullInt64
		var isMask bool
		if rows.Scan(&characterID, &entry.Name, &isMask, &entry.PostCount) == nil {
			entry.IsMask = isMask
			if characterID.Valid && !isMask {
				id := int(characterID.Int64)
				entry.CharacterID = &id
			}
			result = append(result, entry)
		}
	}

	c.JSON(http.StatusOK, result)
}

func GetEpisodesByFaction(c *gin.Context, db *sql.DB) {
	dateFrom, dateTo, ok := parseDateRange(c)
	if !ok {
		return
	}

	// An episode belongs to a faction when any of its participating characters is a
	// member of that faction. COUNT(DISTINCT) prevents double-counting when multiple
	// characters in the same episode share a faction.
	rows, err := db.Query(`
		SELECT
			f.id,
			f.name,
			COALESCE(cr.cnt, 0) AS created,
			COALESCE(fi.cnt, 0) AS finished,
			COALESCE(ar.cnt, 0) AS archived
		FROM factions f
		LEFT JOIN (
			SELECT cf.faction_id, COUNT(DISTINCT eb.id) AS cnt
			FROM episode_base eb
			JOIN topics t             ON t.id = eb.topic_id
			JOIN episode_character ec ON ec.episode_id = eb.id
			JOIN character_faction cf ON cf.character_id = ec.character_id
			WHERE t.date_created BETWEEN ? AND ?
			GROUP BY cf.faction_id
		) cr ON cr.faction_id = f.id
		LEFT JOIN (
			SELECT cf.faction_id, COUNT(DISTINCT tal.topic_id) AS cnt
			FROM topic_activity_log tal
			JOIN episode_base eb       ON eb.topic_id = tal.topic_id
			JOIN episode_character ec  ON ec.episode_id = eb.id
			JOIN character_faction cf  ON cf.character_id = ec.character_id
			WHERE tal.event = 'episode_status_changed'
			  AND tal.new_state = ?
			  AND tal.date BETWEEN ? AND ?
			GROUP BY cf.faction_id
		) fi ON fi.faction_id = f.id
		LEFT JOIN (
			SELECT cf.faction_id, COUNT(DISTINCT tal.topic_id) AS cnt
			FROM topic_activity_log tal
			JOIN episode_base eb       ON eb.topic_id = tal.topic_id
			JOIN episode_character ec  ON ec.episode_id = eb.id
			JOIN character_faction cf  ON cf.character_id = ec.character_id
			WHERE tal.event = 'episode_status_changed'
			  AND tal.new_state = ?
			  AND tal.date BETWEEN ? AND ?
			GROUP BY cf.faction_id
		) ar ON ar.faction_id = f.id
		WHERE COALESCE(cr.cnt, 0) > 0
		   OR COALESCE(fi.cnt, 0) > 0
		   OR COALESCE(ar.cnt, 0) > 0
		ORDER BY f.name
	`, dateFrom, dateTo,
		int(Entities.FinishedEpisode), dateFrom, dateTo,
		int(Entities.InactiveEpisode), dateFrom, dateTo,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to query episodes by faction"})
		c.Abort()
		return
	}
	defer rows.Close()

	result := []FactionEpisodeStats{}
	for rows.Next() {
		var entry FactionEpisodeStats
		if rows.Scan(&entry.FactionID, &entry.Name, &entry.Created, &entry.Finished, &entry.Archived) == nil {
			result = append(result, entry)
		}
	}

	c.JSON(http.StatusOK, result)
}

func GetPostsByFaction(c *gin.Context, db *sql.DB) {
	dateFrom, dateTo, ok := parseDateRange(c)
	if !ok {
		return
	}

	rows, err := db.Query(`
		SELECT f.id, f.name, COUNT(DISTINCT p.id) AS post_count
		FROM factions f
		JOIN character_faction cf       ON cf.faction_id = f.id
		JOIN character_base cb          ON cb.id = cf.character_id
		JOIN character_profile_base cpb ON cpb.character_id = cb.id
		JOIN posts p                    ON p.character_profile_id = cpb.id
		JOIN episode_base eb            ON eb.topic_id = p.topic_id
		WHERE p.date_created BETWEEN ? AND ?
		  AND p.use_character_profile = true
		  AND (p.is_deleted IS NULL OR p.is_deleted != 1)
		GROUP BY f.id, f.name
		ORDER BY post_count DESC
	`, dateFrom, dateTo)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to query posts by faction"})
		c.Abort()
		return
	}
	defer rows.Close()

	result := []FactionPostStats{}
	for rows.Next() {
		var entry FactionPostStats
		if rows.Scan(&entry.FactionID, &entry.Name, &entry.PostCount) == nil {
			result = append(result, entry)
		}
	}

	c.JSON(http.StatusOK, result)
}
