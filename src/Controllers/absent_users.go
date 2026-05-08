package Controllers

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"database/sql"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// grantPostAbsenceImmunity inserts a 3-day auto_archiving_immunity record for every
// active character of the given user, starting from the absence end date.
func grantPostAbsenceImmunity(userID int, absenceEnd time.Time, db *sql.DB) {
	immunityStart := absenceEnd
	immunityEnd := absenceEnd.Add(3 * 24 * time.Hour)

	rows, err := db.Query(
		"SELECT id FROM character_base WHERE user_id = ? AND character_status = ?",
		userID, Entities.ActiveCharacter,
	)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var charID int
		if rows.Scan(&charID) != nil {
			continue
		}
		_, _ = db.Exec(
			"INSERT INTO auto_archiving_immunity (character_id, start_date, end_date, reason) VALUES (?, ?, ?, ?)",
			charID, immunityStart, immunityEnd, "Post-absence",
		)
	}
}

type AbsentUserItem struct {
	UserId           int                       `json:"user_id"`
	Username         string                    `json:"username"`
	AbsenceStartDate time.Time                 `json:"absence_start_date"`
	AbsenceEndDate   time.Time                 `json:"absence_end_date"`
	Characters       []Entities.ShortCharacter `json:"characters"`
}

func GetAbsentUsers(c *gin.Context, db *sql.DB) {
	rows, err := db.Query(`
		SELECT au.user_id, u.username, au.absence_start_date, au.absence_end_date
		FROM absent_users au
		JOIN users u ON u.id = au.user_id
		WHERE au.absence_start_date <= NOW() AND au.absence_end_date >= NOW()
		ORDER BY au.absence_end_date ASC
	`)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get absent users: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	users := []AbsentUserItem{}
	for rows.Next() {
		var u AbsentUserItem
		if err := rows.Scan(&u.UserId, &u.Username, &u.AbsenceStartDate, &u.AbsenceEndDate); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan absent user: " + err.Error()})
			c.Abort()
			return
		}
		u.AbsenceStartDate = u.AbsenceStartDate.In(time.Local)
		u.AbsenceEndDate = u.AbsenceEndDate.In(time.Local)

		u.Characters = []Entities.ShortCharacter{}
		charRows, err := db.Query(
			"SELECT id, name FROM character_base WHERE user_id = ? AND character_status = ? ORDER BY name ASC",
			u.UserId, Entities.ActiveCharacter,
		)
		if err == nil {
			for charRows.Next() {
				var ch Entities.ShortCharacter
				if err := charRows.Scan(&ch.Id, &ch.Name); err == nil {
					u.Characters = append(u.Characters, ch)
				}
			}
			charRows.Close()
		}

		users = append(users, u)
	}

	c.JSON(http.StatusOK, users)
}

type CreateAbsenceRequest struct {
	StartDate string `json:"start_date" binding:"required"`
	EndDate   string `json:"end_date" binding:"required"`
}

func CreateAbsence(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)

	var req CreateAbsenceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	const dateLayout = "2006-01-02"
	startDay, err := time.ParseInLocation(dateLayout, req.StartDate, time.Local)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid start_date format, expected: YYYY-MM-DD"})
		c.Abort()
		return
	}
	endDay, err := time.ParseInLocation(dateLayout, req.EndDate, time.Local)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid end_date format, expected: YYYY-MM-DD"})
		c.Abort()
		return
	}

	// Force time boundaries
	start := time.Date(startDay.Year(), startDay.Month(), startDay.Day(), 0, 0, 0, 0, time.Local)
	end := time.Date(endDay.Year(), endDay.Month(), endDay.Day(), 23, 59, 59, 0, time.Local)

	if !end.After(start) {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "end_date must be after start_date"})
		c.Abort()
		return
	}

	userLoc := time.Local
	if tzName := Services.GetUserTimezone(userID, db); tzName != nil {
		if loc, err := time.LoadLocation(*tzName); err == nil {
			userLoc = loc
		}
	}
	now := time.Now().In(userLoc)
	todayEnd := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, userLoc)
	if end.Before(todayEnd) {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "end_date cannot be in the past"})
		c.Abort()
		return
	}

	// Validate duration against max days setting
	maxDays := Services.GetAbsenceMaxDays(db)
	durationDays := int(end.Sub(start).Hours()/24) + 1
	if durationDays > maxDays {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: fmt.Sprintf("Absence duration cannot exceed %d days", maxDays)})
		c.Abort()
		return
	}

	// Check for overlapping absence periods
	var overlapCount int
	err = db.QueryRow(
		"SELECT COUNT(*) FROM absent_users WHERE user_id = ? AND absence_start_date <= ? AND absence_end_date >= ?",
		userID, end, start,
	).Scan(&overlapCount)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to check overlapping absences: " + err.Error()})
		c.Abort()
		return
	}
	if overlapCount > 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "This period overlaps with an existing absence"})
		c.Abort()
		return
	}

	// Validate cooldown against last absence
	cooldownDays := Services.GetAbsenceCooldownDays(db)
	var lastEndDate time.Time
	err = db.QueryRow(
		"SELECT absence_end_date FROM absent_users WHERE user_id = ? ORDER BY absence_end_date DESC LIMIT 1",
		userID,
	).Scan(&lastEndDate)
	if err != nil && err != sql.ErrNoRows {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to check cooldown: " + err.Error()})
		c.Abort()
		return
	}
	if err == nil {
		daysSinceLast := int(start.Sub(lastEndDate).Hours() / 24)
		if daysSinceLast < cooldownDays {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: fmt.Sprintf("You must wait %d more day(s) before claiming a new absence", cooldownDays-daysSinceLast)})
			c.Abort()
			return
		}
	}

	_, err = db.Exec(
		"INSERT INTO absent_users (user_id, absence_start_date, absence_end_date) VALUES (?, ?, ?)",
		userID, start, end,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create absence: " + err.Error()})
		c.Abort()
		return
	}

	grantPostAbsenceImmunity(userID, end, db)
	c.JSON(http.StatusOK, gin.H{"absence_start_date": start, "absence_end_date": end})
}

func AdminCreateAbsence(c *gin.Context, db *sql.DB) {
	targetUserID, err := strconv.Atoi(c.Param("user_id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid user_id"})
		c.Abort()
		return
	}

	var req CreateAbsenceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	const dateLayout = "2006-01-02"
	startDay, err := time.ParseInLocation(dateLayout, req.StartDate, time.Local)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid start_date format, expected: YYYY-MM-DD"})
		c.Abort()
		return
	}
	endDay, err := time.ParseInLocation(dateLayout, req.EndDate, time.Local)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid end_date format, expected: YYYY-MM-DD"})
		c.Abort()
		return
	}

	start := time.Date(startDay.Year(), startDay.Month(), startDay.Day(), 0, 0, 0, 0, time.Local)
	end := time.Date(endDay.Year(), endDay.Month(), endDay.Day(), 23, 59, 59, 0, time.Local)

	if !end.After(start) {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "end_date must be after start_date"})
		c.Abort()
		return
	}

	var overlapCount int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM absent_users WHERE user_id = ? AND absence_start_date <= ? AND absence_end_date >= ?",
		targetUserID, end, start,
	).Scan(&overlapCount); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to check overlapping absences: " + err.Error()})
		c.Abort()
		return
	}
	if overlapCount > 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "This period overlaps with an existing absence"})
		c.Abort()
		return
	}

	if _, err := db.Exec(
		"INSERT INTO absent_users (user_id, absence_start_date, absence_end_date) VALUES (?, ?, ?)",
		targetUserID, start, end,
	); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create absence: " + err.Error()})
		c.Abort()
		return
	}

	grantPostAbsenceImmunity(targetUserID, end, db)
	c.JSON(http.StatusOK, gin.H{"absence_start_date": start, "absence_end_date": end})
}

type AddImmunityRequest struct {
	CharacterID int    `json:"character_id" binding:"required"`
	StartDate   string `json:"start_date" binding:"required"`
	EndDate     string `json:"end_date" binding:"required"`
	Reason      string `json:"reason" binding:"required"`
}

func AdminAddImmunity(c *gin.Context, db *sql.DB) {
	var req AddImmunityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	const dateLayout = "2006-01-02"
	startDay, err := time.ParseInLocation(dateLayout, req.StartDate, time.Local)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid start_date format, expected: YYYY-MM-DD"})
		c.Abort()
		return
	}
	endDay, err := time.ParseInLocation(dateLayout, req.EndDate, time.Local)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid end_date format, expected: YYYY-MM-DD"})
		c.Abort()
		return
	}

	start := time.Date(startDay.Year(), startDay.Month(), startDay.Day(), 0, 0, 0, 0, time.Local)
	end := time.Date(endDay.Year(), endDay.Month(), endDay.Day(), 23, 59, 59, 0, time.Local)

	if !end.After(start) {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "end_date must be after start_date"})
		c.Abort()
		return
	}

	var exists int
	if err := db.QueryRow("SELECT COUNT(*) FROM character_base WHERE id = ?", req.CharacterID).Scan(&exists); err != nil || exists == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Character not found"})
		c.Abort()
		return
	}

	_, err = db.Exec(
		"INSERT INTO auto_archiving_immunity (character_id, start_date, end_date, reason) VALUES (?, ?, ?, ?)",
		req.CharacterID, start, end, req.Reason,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to add immunity: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"character_id": req.CharacterID, "start_date": start, "end_date": end, "reason": req.Reason})
}

type CharacterProtectionItem struct {
	Type      string    `json:"type"`
	StartDate time.Time `json:"start_date"`
	EndDate   time.Time `json:"end_date"`
	Reason    *string   `json:"reason"`
}

func GetCharacterProtectionHistory(c *gin.Context, db *sql.DB) {
	characterID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid character ID"})
		c.Abort()
		return
	}

	// Resolve the user_id for this character to fetch absences
	var userID int
	if err := db.QueryRow("SELECT user_id FROM character_base WHERE id = ?", characterID).Scan(&userID); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Character not found"})
		c.Abort()
		return
	}

	items := []CharacterProtectionItem{}

	// Absences (belong to the user, no reason field)
	absenceRows, err := db.Query(
		"SELECT absence_start_date, absence_end_date FROM absent_users WHERE user_id = ? ORDER BY absence_start_date DESC",
		userID,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch absences: " + err.Error()})
		c.Abort()
		return
	}
	defer absenceRows.Close()
	for absenceRows.Next() {
		var item CharacterProtectionItem
		item.Type = "absence"
		if err := absenceRows.Scan(&item.StartDate, &item.EndDate); err != nil {
			continue
		}
		item.StartDate = item.StartDate.In(time.Local)
		item.EndDate = item.EndDate.In(time.Local)
		items = append(items, item)
	}

	// Immunities (belong to the character, have a reason)
	immunityRows, err := db.Query(
		"SELECT start_date, end_date, reason FROM auto_archiving_immunity WHERE character_id = ? ORDER BY start_date DESC",
		characterID,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch immunities: " + err.Error()})
		c.Abort()
		return
	}
	defer immunityRows.Close()
	for immunityRows.Next() {
		var item CharacterProtectionItem
		item.Type = "immunity"
		var reason string
		if err := immunityRows.Scan(&item.StartDate, &item.EndDate, &reason); err != nil {
			continue
		}
		item.StartDate = item.StartDate.In(time.Local)
		item.EndDate = item.EndDate.In(time.Local)
		item.Reason = &reason
		items = append(items, item)
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].EndDate.After(items[j].EndDate)
	})

	c.JSON(http.StatusOK, items)
}
