package Controllers

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

type CreateStandardWarningRequest struct {
	Id             *int   `json:"id"`
	Name           string `json:"name" binding:"required"`
	Description    string `json:"description"`
	Locale         string `json:"locale" binding:"required"`
	RatingLanguage int    `json:"rating_language"`
	RatingViolence int    `json:"rating_violence"`
	RatingSex      int    `json:"rating_sex"`
}

type UpdateStandardWarningRequest struct {
	Name           *string `json:"name"`
	Description    *string `json:"description"`
	RatingLanguage *int    `json:"rating_language"`
	RatingViolence *int    `json:"rating_violence"`
	RatingSex      *int    `json:"rating_sex"`
}

func GetStandardWarnings(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	locale := Services.GetUserLanguage(userID, db)

	rows, err := db.Query("SELECT id, locale, name, description, rating_language, rating_violence, rating_sex FROM standard_warnings WHERE locale = ? ORDER BY id", locale)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get standard warnings: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	var warnings []Entities.StandardWarning
	for rows.Next() {
		var w Entities.StandardWarning
		if err := rows.Scan(&w.Id, &w.Locale, &w.Name, &w.Description, &w.RatingLanguage, &w.RatingViolence, &w.RatingSex); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan standard warning: " + err.Error()})
			c.Abort()
			return
		}
		warnings = append(warnings, w)
	}

	if warnings == nil {
		warnings = []Entities.StandardWarning{}
	}

	c.JSON(http.StatusOK, warnings)
}

func GetEpisodeWarnings(c *gin.Context, db *sql.DB) {
	episodeId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid episode id"})
		c.Abort()
		return
	}
	locale := c.Param("locale")

	rows, err := db.Query(`
		SELECT sw.id, sw.locale, sw.name, sw.description, sw.rating_language, sw.rating_violence, sw.rating_sex
		FROM standard_warnings sw
		JOIN episode_warnings ew ON sw.id = ew.warning_id
		WHERE ew.episode_id = ? AND sw.locale = ?
		ORDER BY sw.id
	`, episodeId, locale)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get episode warnings: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	var warnings []Entities.StandardWarning
	for rows.Next() {
		var w Entities.StandardWarning
		if err := rows.Scan(&w.Id, &w.Locale, &w.Name, &w.Description, &w.RatingLanguage, &w.RatingViolence, &w.RatingSex); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan warning: " + err.Error()})
			c.Abort()
			return
		}
		warnings = append(warnings, w)
	}

	if warnings == nil {
		warnings = []Entities.StandardWarning{}
	}

	c.JSON(http.StatusOK, warnings)
}

func CreateStandardWarning(c *gin.Context, db *sql.DB) {
	var req CreateStandardWarningRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	var id int
	if req.Id != nil {
		id = *req.Id
	} else {
		if err := db.QueryRow("SELECT COALESCE(MAX(id), 0) + 1 FROM standard_warnings").Scan(&id); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to generate warning id: " + err.Error()})
			c.Abort()
			return
		}
	}

	if _, err := db.Exec(
		"INSERT INTO standard_warnings (id, locale, name, description, rating_language, rating_violence, rating_sex) VALUES (?, ?, ?, ?, ?, ?, ?)",
		id, req.Locale, req.Name, req.Description, req.RatingLanguage, req.RatingViolence, req.RatingSex,
	); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create standard warning: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, Entities.StandardWarning{
		Id:             id,
		Locale:         req.Locale,
		Name:           req.Name,
		Description:    req.Description,
		RatingLanguage: req.RatingLanguage,
		RatingViolence: req.RatingViolence,
		RatingSex:      req.RatingSex,
	})
}

func UpdateStandardWarning(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid id"})
		c.Abort()
		return
	}
	locale := c.Param("locale")

	var req UpdateStandardWarningRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	setClauses := []string{}
	args := []interface{}{}

	if req.Name != nil {
		setClauses = append(setClauses, "name = ?")
		args = append(args, *req.Name)
	}
	if req.Description != nil {
		setClauses = append(setClauses, "description = ?")
		args = append(args, *req.Description)
	}
	if req.RatingLanguage != nil {
		setClauses = append(setClauses, "rating_language = ?")
		args = append(args, *req.RatingLanguage)
	}
	if req.RatingViolence != nil {
		setClauses = append(setClauses, "rating_violence = ?")
		args = append(args, *req.RatingViolence)
	}
	if req.RatingSex != nil {
		setClauses = append(setClauses, "rating_sex = ?")
		args = append(args, *req.RatingSex)
	}

	if len(setClauses) == 0 {
		c.JSON(http.StatusOK, gin.H{"message": "Standard warning updated"})
		return
	}

	args = append(args, id, locale)
	res, err := db.Exec("UPDATE standard_warnings SET "+strings.Join(setClauses, ", ")+" WHERE id = ? AND locale = ?", args...)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update standard warning: " + err.Error()})
		c.Abort()
		return
	}

	affected, _ := res.RowsAffected()
	if affected == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Standard warning not found"})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Standard warning updated"})
}

func DeleteStandardWarning(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid id"})
		c.Abort()
		return
	}
	locale := c.Param("locale")

	var exists bool
	if err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM standard_warnings WHERE id = ? AND locale = ?)", id, locale).Scan(&exists); err != nil || !exists {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Standard warning not found"})
		c.Abort()
		return
	}

	if _, err := db.Exec("DELETE FROM standard_warnings WHERE id = ? AND locale = ?", id, locale); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to delete standard warning: " + err.Error()})
		c.Abort()
		return
	}

	// Clean up episode references only if no locales remain for this id
	var remaining int
	if err := db.QueryRow("SELECT COUNT(*) FROM standard_warnings WHERE id = ?", id).Scan(&remaining); err == nil && remaining == 0 {
		_, _ = db.Exec("DELETE FROM episode_warnings WHERE warning_id = ?", id)
	}

	c.JSON(http.StatusOK, gin.H{"message": "Standard warning deleted"})
}
