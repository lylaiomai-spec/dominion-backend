package Controllers

import (
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
)

type UserSubforumSetting struct {
	SubforumID             int64  `json:"subforum_id"`
	SubforumName           string `json:"subforum_name"`
	HideNewPostsIndex      bool   `json:"hide_new_posts_index"`
	HideNewPostsActivePage bool   `json:"hide_new_posts_active_page"`
}

type UpsertUserSubforumSettingRequest struct {
	SubforumID             int64 `json:"subforum_id" binding:"required"`
	HideNewPostsIndex      bool  `json:"hide_new_posts_index"`
	HideNewPostsActivePage bool  `json:"hide_new_posts_active_page"`
}

func GetUserSubforumSettings(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)

	rows, err := db.Query(`
		SELECT s.id, s.name,
		       COALESCE(uss.hide_new_posts_index, 0),
		       COALESCE(uss.hide_new_posts_active_page, 0)
		FROM subforums s
		LEFT JOIN categories c ON c.id = s.category_id
		LEFT JOIN user_subforum_settings uss ON uss.subforum_id = s.id AND uss.user_id = ?
		ORDER BY c.position, s.position`,
		userID,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch settings: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	settings := []UserSubforumSetting{}
	for rows.Next() {
		var s UserSubforumSetting
		if err := rows.Scan(&s.SubforumID, &s.SubforumName, &s.HideNewPostsIndex, &s.HideNewPostsActivePage); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan setting: " + err.Error()})
			c.Abort()
			return
		}
		settings = append(settings, s)
	}

	c.JSON(http.StatusOK, settings)
}

func UpdateAllUserSubforumSettings(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)

	var req []UpsertUserSubforumSettingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	tx, err := db.Begin()
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to start transaction"})
		c.Abort()
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM user_subforum_settings WHERE user_id = ?", userID); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to clear settings: " + err.Error()})
		c.Abort()
		return
	}

	for _, s := range req {
		if !s.HideNewPostsIndex && !s.HideNewPostsActivePage {
			continue
		}
		if _, err := tx.Exec(
			"INSERT INTO user_subforum_settings (user_id, subforum_id, hide_new_posts_index, hide_new_posts_active_page) VALUES (?, ?, ?, ?)",
			userID, s.SubforumID, s.HideNewPostsIndex, s.HideNewPostsActivePage,
		); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to save settings: " + err.Error()})
			c.Abort()
			return
		}
	}

	if err := tx.Commit(); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to commit"})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Settings updated"})
}

