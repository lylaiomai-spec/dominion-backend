package Controllers

import (
	"crypto/rand"
	"cuento-backend/src/Entities"
	"cuento-backend/src/Events"
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"cuento-backend/src/Websockets"
	"database/sql"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type UpdateDesignDraftRequest struct {
	Name           *string `json:"name"`
	MainCss        *string `json:"main_css"`
	CustomStyleCss *string `json:"custom_style_css"`
}

type CreateDesignDraftRequest struct {
	Name string `json:"name" binding:"required"`
}

func generateSessionKey() (string, error) {
	bytes := make([]byte, 6)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func CreateDesignDraft(c *gin.Context, db *sql.DB) {
	var req CreateDesignDraftRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	sessionKey, err := generateSessionKey()
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to generate session key"})
		c.Abort()
		return
	}

	publicDir := "./../frontend"

	mainCssBytes, err := os.ReadFile(filepath.Join(publicDir, "main_style.css"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to read main_style.css: " + err.Error()})
		c.Abort()
		return
	}

	customStyleBytes, err := os.ReadFile(filepath.Join(publicDir, "custom_style.css"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to read custom_style.css: " + err.Error()})
		c.Abort()
		return
	}

	now := time.Now()
	draft := Entities.DesignDraft{
		Name:            req.Name,
		SessionKey:      sessionKey,
		DateCreated:     now,
		DateLastChanged: now,
		MainCss:         string(mainCssBytes),
		CustomStyleCss:  string(customStyleBytes),
	}

	res, err := db.Exec(
		"INSERT INTO design_drafts (name, session_key, date_created, date_last_changed, main_css, custom_style_css) VALUES (?, ?, ?, ?, ?, ?)",
		draft.Name, draft.SessionKey, draft.DateCreated, draft.DateLastChanged, draft.MainCss, draft.CustomStyleCss,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create design draft: " + err.Error()})
		c.Abort()
		return
	}

	id, _ := res.LastInsertId()
	draft.Id = int(id)

	c.JSON(http.StatusOK, draft)
}

func GetDesignDraft(c *gin.Context, db *sql.DB) {
	id := c.Param("id")

	var draft Entities.DesignDraft
	err := db.QueryRow(
		"SELECT id, name, session_key, date_created, date_last_changed, main_css, custom_style_css FROM design_drafts WHERE id = ?",
		id,
	).Scan(&draft.Id, &draft.Name, &draft.SessionKey, &draft.DateCreated, &draft.DateLastChanged, &draft.MainCss, &draft.CustomStyleCss)
	if err == sql.ErrNoRows {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Design draft not found"})
		c.Abort()
		return
	}
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch design draft: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, draft)
}

func GetDesignDraftList(c *gin.Context, db *sql.DB) {
	rows, err := db.Query(
		"SELECT id, name, session_key, date_created, date_last_changed FROM design_drafts ORDER BY date_created DESC",
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch design drafts: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	drafts := []Entities.DesignDraftListItem{}
	for rows.Next() {
		var d Entities.DesignDraftListItem
		if err := rows.Scan(&d.Id, &d.Name, &d.SessionKey, &d.DateCreated, &d.DateLastChanged); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan design draft: " + err.Error()})
			c.Abort()
			return
		}
		drafts = append(drafts, d)
	}

	c.JSON(http.StatusOK, drafts)
}

func GetDesignDraftMainCss(c *gin.Context, db *sql.DB) {
	sessionKey := c.Param("session_key")

	var mainCss string
	err := db.QueryRow("SELECT main_css FROM design_drafts WHERE session_key = ?", sessionKey).Scan(&mainCss)
	if err == sql.ErrNoRows {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Design draft not found"})
		c.Abort()
		return
	}
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch design draft: " + err.Error()})
		c.Abort()
		return
	}

	c.Data(http.StatusOK, "text/css; charset=utf-8", []byte(mainCss))
}

func GetDesignDraftCustomStyleCss(c *gin.Context, db *sql.DB) {
	sessionKey := c.Param("session_key")

	var customStyleCss string
	err := db.QueryRow("SELECT custom_style_css FROM design_drafts WHERE session_key = ?", sessionKey).Scan(&customStyleCss)
	if err == sql.ErrNoRows {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Design draft not found"})
		c.Abort()
		return
	}
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch design draft: " + err.Error()})
		c.Abort()
		return
	}

	c.Data(http.StatusOK, "text/css; charset=utf-8", []byte(customStyleCss))
}

func UpdateDesignDraft(c *gin.Context, db *sql.DB) {
	id := c.Param("id")

	var req UpdateDesignDraftRequest
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
	if req.MainCss != nil {
		setClauses = append(setClauses, "main_css = ?")
		args = append(args, *req.MainCss)
	}
	if req.CustomStyleCss != nil {
		setClauses = append(setClauses, "custom_style_css = ?")
		args = append(args, *req.CustomStyleCss)
	}

	if len(setClauses) == 0 {
		c.JSON(http.StatusOK, gin.H{"message": "Design draft updated"})
		return
	}

	setClauses = append(setClauses, "date_last_changed = ?")
	args = append(args, time.Now())
	args = append(args, id)

	query := "UPDATE design_drafts SET " + strings.Join(setClauses, ", ") + " WHERE id = ?"
	res, err := db.Exec(query, args...)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update design draft: " + err.Error()})
		c.Abort()
		return
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Design draft not found"})
		c.Abort()
		return
	}

	cssChanged := req.MainCss != nil || req.CustomStyleCss != nil
	if cssChanged {
		var sessionKey string
		if err := db.QueryRow("SELECT session_key FROM design_drafts WHERE id = ?", id).Scan(&sessionKey); err == nil {
			notifyDraftSubscribers(sessionKey)
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "Design draft updated"})
}

func PublishDesignDraft(c *gin.Context, db *sql.DB) {
	id := c.Param("id")

	var draft Entities.DesignDraft
	err := db.QueryRow(
		"SELECT id, name, session_key, main_css, custom_style_css FROM design_drafts WHERE id = ?",
		id,
	).Scan(&draft.Id, &draft.Name, &draft.SessionKey, &draft.MainCss, &draft.CustomStyleCss)
	if err == sql.ErrNoRows {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Design draft not found"})
		c.Abort()
		return
	}
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch design draft: " + err.Error()})
		c.Abort()
		return
	}

	publicDir := "./../frontend"

	if err := publishCssFile(db, publicDir, "main_style.css", draft.MainCss); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to publish main_style.css: " + err.Error()})
		c.Abort()
		return
	}
	Events.Publish(db, Events.StaticFileUploaded, Events.StaticFileUploadedEvent{FileType: "main_style.css"})

	if err := publishCssFile(db, publicDir, "custom_style.css", draft.CustomStyleCss); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to publish custom_style.css: " + err.Error()})
		c.Abort()
		return
	}
	Events.Publish(db, Events.StaticFileUploaded, Events.StaticFileUploadedEvent{FileType: "custom_style.css"})

	c.JSON(http.StatusOK, gin.H{"message": "Design draft published"})
}

func DeleteDesignDraft(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid draft ID"})
		c.Abort()
		return
	}

	res, err := db.Exec("DELETE FROM design_drafts WHERE id = ?", id)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to delete design draft: " + err.Error()})
		c.Abort()
		return
	}

	if n, _ := res.RowsAffected(); n == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Design draft not found"})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Design draft deleted"})
}

func notifyDraftSubscribers(sessionKey string) {
	subs := Services.GetDraftSubscribers(sessionKey)
	userIDs := make([]int, 0, len(subs))
	seen := make(map[int]bool)
	for _, sub := range subs {
		if client, ok := sub.(*Websockets.Client); ok && !seen[client.UserID] {
			seen[client.UserID] = true
			userIDs = append(userIDs, client.UserID)
		}
	}
	Websockets.MainHub.BroadcastToUsers(userIDs, map[string]interface{}{
		"type":     "draft_updated",
		"draft_id": sessionKey,
	})
}
