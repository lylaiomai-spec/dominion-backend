package Controllers

import (
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

var (
	iframeTagRe    = regexp.MustCompile(`(?i)^\s*<iframe(\s[^>]*)?\s*(?:/>|>\s*</iframe>)\s*$`)
	jsProtocolRe   = regexp.MustCompile(`(?i)javascript\s*:`)
	eventHandlerRe = regexp.MustCompile(`(?i)\bon\w+\s*=`)
	scriptTagRe    = regexp.MustCompile(`(?i)<\s*script`)
)

func validateIframeCode(code string) error {
	if !iframeTagRe.MatchString(code) {
		return errors.New("iframe code must contain only a single <iframe> tag")
	}
	if jsProtocolRe.MatchString(code) {
		return errors.New("iframe code must not contain javascript: protocol")
	}
	if eventHandlerRe.MatchString(code) {
		return errors.New("iframe code must not contain event handlers")
	}
	if scriptTagRe.MatchString(code) {
		return errors.New("iframe code must not contain script tags")
	}
	return nil
}

type PuzzleResponse struct {
	ID          int       `json:"id"`
	Title       string    `json:"title"`
	IframeCode  string    `json:"iframe_code"`
	DateCreated time.Time `json:"date_created"`
	IsPublic    bool      `json:"is_public"`
	IsActive    bool      `json:"is_active"`
}

type PuzzleAchievementResponse struct {
	ID            int       `json:"id"`
	PuzzleID      int       `json:"puzzle_id"`
	UserID        int       `json:"user_id"`
	Date          time.Time `json:"date"`
	ScreenshotURL string    `json:"screenshot_url"`
}

type CreatePuzzleRequest struct {
	Title      string `json:"title" binding:"required"`
	IframeCode string `json:"iframe_code" binding:"required"`
	IsPublic   bool   `json:"is_public"`
	IsActive   bool   `json:"is_active"`
}

type UpdatePuzzleRequest struct {
	Title      *string `json:"title"`
	IframeCode *string `json:"iframe_code"`
	IsPublic   *bool   `json:"is_public"`
	IsActive   *bool   `json:"is_active"`
}

type SaveAchievementRequest struct {
	ScreenshotURL string `json:"screenshot_url" binding:"required"`
}

func GetPuzzles(c *gin.Context, db *sql.DB) {
	var query string
	if Services.GetUserIdFromContext(c) != 0 {
		query = "SELECT id, title, iframe_code, date_created, is_public, is_active FROM puzzles WHERE is_active = 1 ORDER BY date_created DESC"
	} else {
		query = "SELECT id, title, iframe_code, date_created, is_public, is_active FROM puzzles WHERE is_public = 1 AND is_active = 1 ORDER BY date_created DESC"
	}

	rows, err := db.Query(query)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get puzzles: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	var puzzles []PuzzleResponse
	for rows.Next() {
		var p PuzzleResponse
		if err := rows.Scan(&p.ID, &p.Title, &p.IframeCode, &p.DateCreated, &p.IsPublic, &p.IsActive); err != nil {
			continue
		}
		puzzles = append(puzzles, p)
	}
	if puzzles == nil {
		puzzles = []PuzzleResponse{}
	}
	c.JSON(http.StatusOK, puzzles)
}

func GetPuzzle(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid puzzle ID"})
		c.Abort()
		return
	}

	var p PuzzleResponse
	err = db.QueryRow("SELECT id, title, iframe_code, date_created, is_public, is_active FROM puzzles WHERE id = ? AND is_active = 1", id).
		Scan(&p.ID, &p.Title, &p.IframeCode, &p.DateCreated, &p.IsPublic, &p.IsActive)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Puzzle not found"})
		c.Abort()
		return
	}

	// Guests can only access public puzzles
	if !p.IsPublic && Services.GetUserIdFromContext(c) == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Puzzle not found"})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, p)
}

func AdminCreatePuzzle(c *gin.Context, db *sql.DB) {
	var req CreatePuzzleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	if err := validateIframeCode(req.IframeCode); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: err.Error()})
		c.Abort()
		return
	}

	res, err := db.Exec(
		"INSERT INTO puzzles (title, iframe_code, is_public, is_active) VALUES (?, ?, ?, ?)",
		req.Title, req.IframeCode, req.IsPublic, req.IsActive,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create puzzle: " + err.Error()})
		c.Abort()
		return
	}

	newID, _ := res.LastInsertId()

	var p PuzzleResponse
	_ = db.QueryRow("SELECT id, title, iframe_code, date_created, is_public, is_active FROM puzzles WHERE id = ?", newID).
		Scan(&p.ID, &p.Title, &p.IframeCode, &p.DateCreated, &p.IsPublic, &p.IsActive)

	c.JSON(http.StatusOK, p)
}

func AdminUpdatePuzzle(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid puzzle ID"})
		c.Abort()
		return
	}

	var req UpdatePuzzleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	if req.Title != nil {
		db.Exec("UPDATE puzzles SET title = ? WHERE id = ?", *req.Title, id)
	}
	if req.IframeCode != nil {
		if err := validateIframeCode(*req.IframeCode); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: err.Error()})
			c.Abort()
			return
		}
		db.Exec("UPDATE puzzles SET iframe_code = ? WHERE id = ?", *req.IframeCode, id)
	}
	if req.IsPublic != nil {
		db.Exec("UPDATE puzzles SET is_public = ? WHERE id = ?", *req.IsPublic, id)
	}
	if req.IsActive != nil {
		db.Exec("UPDATE puzzles SET is_active = ? WHERE id = ?", *req.IsActive, id)
	}

	var p PuzzleResponse
	err = db.QueryRow("SELECT id, title, iframe_code, date_created, is_public, is_active FROM puzzles WHERE id = ?", id).
		Scan(&p.ID, &p.Title, &p.IframeCode, &p.DateCreated, &p.IsPublic, &p.IsActive)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Puzzle not found"})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, p)
}

func AdminGetPuzzles(c *gin.Context, db *sql.DB) {
	rows, err := db.Query("SELECT id, title, iframe_code, date_created, is_public, is_active FROM puzzles ORDER BY date_created DESC")
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get puzzles: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	var puzzles []PuzzleResponse
	for rows.Next() {
		var p PuzzleResponse
		if err := rows.Scan(&p.ID, &p.Title, &p.IframeCode, &p.DateCreated, &p.IsPublic, &p.IsActive); err != nil {
			continue
		}
		puzzles = append(puzzles, p)
	}
	if puzzles == nil {
		puzzles = []PuzzleResponse{}
	}
	c.JSON(http.StatusOK, puzzles)
}

func AdminGetPuzzle(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid puzzle ID"})
		c.Abort()
		return
	}

	var p PuzzleResponse
	err = db.QueryRow("SELECT id, title, iframe_code, date_created, is_public, is_active FROM puzzles WHERE id = ?", id).
		Scan(&p.ID, &p.Title, &p.IframeCode, &p.DateCreated, &p.IsPublic, &p.IsActive)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Puzzle not found"})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, p)
}

func SavePuzzleAchievement(c *gin.Context, db *sql.DB) {
	puzzleID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid puzzle ID"})
		c.Abort()
		return
	}

	// Image uploading must be enabled
	if val, _ := Services.GetGlobalSetting("use_image_uploading", db); val != "y" {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "Image uploading is disabled"})
		c.Abort()
		return
	}

	userID := Services.GetUserIdFromContext(c)

	var req SaveAchievementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	// Verify puzzle exists and is accessible
	var isPublic, isActive bool
	err = db.QueryRow("SELECT is_public, is_active FROM puzzles WHERE id = ?", puzzleID).Scan(&isPublic, &isActive)
	if err != nil || !isPublic || !isActive {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Puzzle not found"})
		c.Abort()
		return
	}

	res, err := db.Exec(
		"INSERT INTO puzzle_achievements (puzzle_id, user_id, screenshot_url) VALUES (?, ?, ?)",
		puzzleID, userID, req.ScreenshotURL,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to save achievement: " + err.Error()})
		c.Abort()
		return
	}

	newID, _ := res.LastInsertId()

	var a PuzzleAchievementResponse
	_ = db.QueryRow("SELECT id, puzzle_id, user_id, date, screenshot_url FROM puzzle_achievements WHERE id = ?", newID).
		Scan(&a.ID, &a.PuzzleID, &a.UserID, &a.Date, &a.ScreenshotURL)

	c.JSON(http.StatusOK, a)
}

func DeletePuzzleAchievement(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid achievement ID"})
		c.Abort()
		return
	}

	userID := Services.GetUserIdFromContext(c)

	var ownerID int
	err = db.QueryRow("SELECT user_id FROM puzzle_achievements WHERE id = ?", id).Scan(&ownerID)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Achievement not found"})
		c.Abort()
		return
	}

	if ownerID != userID {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "You can only delete your own achievements"})
		c.Abort()
		return
	}

	_, err = db.Exec("DELETE FROM puzzle_achievements WHERE id = ?", id)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to delete achievement: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Achievement deleted"})
}

func GetUserPuzzleAchievements(c *gin.Context, db *sql.DB) {
	userID, err := strconv.Atoi(c.Param("user_id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid user ID"})
		c.Abort()
		return
	}

	rows, err := db.Query(
		"SELECT id, puzzle_id, user_id, date, screenshot_url FROM puzzle_achievements WHERE user_id = ? ORDER BY date DESC",
		userID,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get achievements: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	var achievements []PuzzleAchievementResponse
	for rows.Next() {
		var a PuzzleAchievementResponse
		if err := rows.Scan(&a.ID, &a.PuzzleID, &a.UserID, &a.Date, &a.ScreenshotURL); err != nil {
			continue
		}
		achievements = append(achievements, a)
	}
	if achievements == nil {
		achievements = []PuzzleAchievementResponse{}
	}
	c.JSON(http.StatusOK, achievements)
}
