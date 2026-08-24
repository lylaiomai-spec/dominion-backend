package Controllers

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Events"
	"cuento-backend/src/Features"
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"database/sql"
	"encoding/json"
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
	Id               int                       `json:"id"`
	UserId           int                       `json:"user_id"`
	Username         string                    `json:"username"`
	AbsenceStartDate time.Time                 `json:"absence_start_date"`
	AbsenceEndDate   time.Time                 `json:"absence_end_date"`
	Characters       []Entities.ShortCharacter `json:"characters"`
}

func GetAbsentUsers(c *gin.Context, db *sql.DB) {
	rows, err := db.Query(`
		SELECT au.id, au.user_id, u.username, au.absence_start_date, au.absence_end_date
		FROM absent_users au
		JOIN users u ON u.id = au.user_id
		WHERE au.absence_start_date <= NOW() AND au.absence_end_date >= NOW() AND au.is_deleted = 0
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
		if err := rows.Scan(&u.Id, &u.UserId, &u.Username, &u.AbsenceStartDate, &u.AbsenceEndDate); err != nil {
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
		"SELECT COUNT(*) FROM absent_users WHERE user_id = ? AND absence_start_date <= ? AND absence_end_date >= ? AND is_deleted = 0",
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
		"SELECT absence_end_date FROM absent_users WHERE user_id = ? AND is_deleted = 0 ORDER BY absence_end_date DESC LIMIT 1",
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
		"SELECT COUNT(*) FROM absent_users WHERE user_id = ? AND absence_start_date <= ? AND absence_end_date >= ? AND is_deleted = 0",
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

func getCharacterArchivalDate(characterID int, db *sql.DB) (time.Time, error) {
	autoArchivingDays := Services.GetAutoArchivingDays(db)
	var archivalDate time.Time
	err := db.QueryRow(`
		SELECT COALESCE(
			(SELECT MAX(end_date) FROM auto_archiving_immunity WHERE character_id = ? AND end_date > NOW()),
			DATE_ADD(COALESCE(cb.date_last_post, t.date_created), INTERVAL ? DAY)
		)
		FROM character_base cb
		JOIN topics t ON t.id = cb.topic_id
		WHERE cb.id = ?
	`, characterID, autoArchivingDays, characterID).Scan(&archivalDate)
	if err != nil {
		return archivalDate, err
	}
	archivalDate = time.Date(archivalDate.Year(), archivalDate.Month(), archivalDate.Day(), 23, 59, 59, 0, archivalDate.Location())
	return archivalDate, nil
}

type AddImmunityRequest struct {
	CharacterID int    `json:"character_id" binding:"required"`
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
	endDay, err := time.ParseInLocation(dateLayout, req.EndDate, time.Local)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid end_date format, expected: YYYY-MM-DD"})
		c.Abort()
		return
	}
	end := time.Date(endDay.Year(), endDay.Month(), endDay.Day(), 23, 59, 59, 0, time.Local)

	start, err := getCharacterArchivalDate(req.CharacterID, db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Character not found"})
		c.Abort()
		return
	}

	if !end.After(start) {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "end_date must be after the character's archival date"})
		c.Abort()
		return
	}

	startDate := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
	_, err = db.Exec(
		"INSERT INTO auto_archiving_immunity (character_id, start_date, end_date, reason) VALUES (?, ?, ?, ?)",
		req.CharacterID, startDate, end, req.Reason,
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
		"SELECT absence_start_date, absence_end_date FROM absent_users WHERE user_id = ? AND is_deleted = 0 ORDER BY absence_start_date DESC",
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

func DeleteAbsence(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)

	absenceID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid absence ID"})
		c.Abort()
		return
	}

	res, err := db.Exec(
		"UPDATE absent_users SET is_deleted = 1 WHERE id = ? AND user_id = ? AND is_deleted = 0",
		absenceID, userID,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to delete absence: " + err.Error()})
		c.Abort()
		return
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Absence not found"})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func BuyAutoArchivingImmunity(c *gin.Context, db *sql.DB) {
	var req struct {
		CharacterID  int `json:"character_id" binding:"required"`
		DurationDays int `json:"duration_days" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	userID := Services.GetUserIdFromContext(c)

	// Verify character belongs to user and is active
	var ownerID int
	var charStatus Entities.CharacterStatus
	err := db.QueryRow("SELECT user_id, character_status FROM character_base WHERE id = ?", req.CharacterID).Scan(&ownerID, &charStatus)
	if err == sql.ErrNoRows {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Character not found"})
		c.Abort()
		return
	}
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch character: " + err.Error()})
		c.Abort()
		return
	}
	if ownerID != userID {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "Character does not belong to you"})
		c.Abort()
		return
	}
	if charStatus != Entities.ActiveCharacter {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Character is not active"})
		c.Abort()
		return
	}

	// Check spend type is active and get price per day
	var pricePerDay int
	var isActive bool
	err = db.QueryRow(
		"SELECT amount, is_active FROM currency_spend_types WHERE `key` = 'currency_spend_auto_archiving_immunity'",
	).Scan(&pricePerDay, &isActive)
	if err == sql.ErrNoRows {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Immunity purchase is not configured"})
		c.Abort()
		return
	}
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch spend type: " + err.Error()})
		c.Abort()
		return
	}
	if !isActive {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Immunity purchase is not available"})
		c.Abort()
		return
	}

	totalCost := pricePerDay * req.DurationDays

	tx, err := db.Begin()
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to start transaction"})
		c.Abort()
		return
	}
	defer tx.Rollback()

	// Atomically deduct — fails if balance is insufficient
	res, err := tx.Exec(
		"UPDATE currency_user_account SET amount = amount - ? WHERE user_id = ? AND amount >= ?",
		totalCost, userID, totalCost,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update balance: " + err.Error()})
		c.Abort()
		return
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusPaymentRequired, Message: "Insufficient currency balance"})
		c.Abort()
		return
	}

	// Record the spend transaction
	metadataJSON, _ := json.Marshal(map[string]int{"character_id": req.CharacterID, "duration_days": req.DurationDays})
	_, err = tx.Exec(
		"INSERT INTO currency_user_transactions (user_id, type, amount, datetime, status, income_type_key, metadata) VALUES (?, ?, ?, NOW(), ?, ?, ?)",
		userID, Features.CurrencyTransactionSpend, totalCost, Features.CurrencyTransactionApproved, "currency_spend_auto_archiving_immunity", metadataJSON,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to record transaction: " + err.Error()})
		c.Abort()
		return
	}

	// Grant immunity starting from the character's archival date
	start, err := getCharacterArchivalDate(req.CharacterID, db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to determine archival date: " + err.Error()})
		c.Abort()
		return
	}
	startDate := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
	end := start.Add(time.Duration(req.DurationDays) * 24 * time.Hour)
	_, err = tx.Exec(
		"INSERT INTO auto_archiving_immunity (character_id, start_date, end_date, reason) VALUES (?, ?, ?, ?)",
		req.CharacterID, startDate, end, "Currency purchase",
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to grant immunity: " + err.Error()})
		c.Abort()
		return
	}

	if err := tx.Commit(); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to commit transaction"})
		c.Abort()
		return
	}

	var newTotal int
	_ = db.QueryRow("SELECT amount FROM currency_user_account WHERE user_id = ?", userID).Scan(&newTotal)

	Events.Publish(db, Events.NotificationCreated, Events.NotificationEvent{
		UserID:  userID,
		Type:    "account_update",
		Message: fmt.Sprintf("%d currency spent on %d days of archiving immunity", totalCost, req.DurationDays),
		Data: Entities.NotificationAccountUpdate{
			IncomeTypeKey: "currency_spend_auto_archiving_immunity",
			Amount:        totalCost,
			TotalAmount:   newTotal,
		},
	})

	c.JSON(http.StatusOK, gin.H{"start_date": start, "end_date": end, "cost": totalCost, "new_balance": newTotal})
}
