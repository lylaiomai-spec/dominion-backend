package Controllers

import (
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"database/sql"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type PostDraftResponse struct {
	ID          int     `json:"id"`
	DraftID     string  `json:"draft_id"`
	UserID      int     `json:"user_id"`
	CharacterID *int64  `json:"character_id"`
	TopicID     *int64  `json:"topic_id"`
	DateCreated string  `json:"date_created"`
	IsManual    bool    `json:"is_manual"`
	IsPublished bool    `json:"is_published"`
	PostID      *int64  `json:"post_id"`
	Content     *string `json:"content"`
}

type PostDraftListResponse struct {
	ID          int    `json:"id"`
	DraftID     string `json:"draft_id"`
	UserID      int    `json:"user_id"`
	CharacterID *int64 `json:"character_id"`
	TopicID     *int64 `json:"topic_id"`
	DateCreated string `json:"date_created"`
	IsManual    bool   `json:"is_manual"`
	IsPublished bool   `json:"is_published"`
	PostID      *int64 `json:"post_id"`
}

type CreatePostDraftRequest struct {
	DraftID     *string `json:"draft_id"`
	CharacterID *int64  `json:"character_id"`
	TopicID     *int64  `json:"topic_id"`
	IsManual    bool    `json:"is_manual"`
	Content     *string `json:"content"`
}

type UpdatePostDraftRequest struct {
	CharacterID *int64  `json:"character_id"`
	TopicID     *int64  `json:"topic_id"`
	IsManual    *bool   `json:"is_manual"`
	Content     *string `json:"content"`
}

func scanDraft(row *sql.Row) (PostDraftResponse, error) {
	var d PostDraftResponse
	var dateCreated []byte
	var characterID sql.NullInt64
	var topicID sql.NullInt64
	var postID sql.NullInt64
	var content sql.NullString
	err := row.Scan(&d.ID, &d.DraftID, &d.UserID, &characterID, &topicID, &dateCreated, &d.IsManual, &d.IsPublished, &postID, &content)
	if err != nil {
		return d, err
	}
	d.DateCreated = string(dateCreated)
	if characterID.Valid {
		d.CharacterID = &characterID.Int64
	}
	if topicID.Valid {
		d.TopicID = &topicID.Int64
	}
	if postID.Valid {
		d.PostID = &postID.Int64
	}
	if content.Valid {
		d.Content = &content.String
	}
	return d, nil
}

func scanDraftFromRows(rows *sql.Rows) (PostDraftListResponse, error) {
	var d PostDraftListResponse
	var dateCreated []byte
	var characterID sql.NullInt64
	var topicID sql.NullInt64
	var postID sql.NullInt64
	err := rows.Scan(&d.ID, &d.DraftID, &d.UserID, &characterID, &topicID, &dateCreated, &d.IsManual, &d.IsPublished, &postID)
	if err != nil {
		return d, err
	}
	d.DateCreated = string(dateCreated)
	if characterID.Valid {
		d.CharacterID = &characterID.Int64
	}
	if topicID.Valid {
		d.TopicID = &topicID.Int64
	}
	if postID.Valid {
		d.PostID = &postID.Int64
	}
	return d, nil
}

const draftSelectFields = `id, draft_id, user_id, character_id, topic_id, date_created, is_manual, is_published, post_id, content`
const draftListSelectFields = `id, draft_id, user_id, character_id, topic_id, date_created, is_manual, is_published, post_id`

// GetLatestPostDraft returns all versions of the latest unpublished draft group for a given topic, owned by the current user.
func GetLatestPostDraft(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		c.Abort()
		return
	}

	topicID, err := strconv.ParseInt(c.Param("topic_id"), 10, 64)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid topic ID"})
		c.Abort()
		return
	}

	var latestDraftID string
	err = db.QueryRow(
		`SELECT draft_id FROM post_drafts WHERE topic_id = ? AND user_id = ? AND is_published = 0 ORDER BY id DESC LIMIT 1`,
		topicID, userID,
	).Scan(&latestDraftID)
	if err != nil {
		c.JSON(http.StatusOK, []PostDraftListResponse{})
		return
	}

	rows, err := db.Query(
		`SELECT `+draftListSelectFields+` FROM post_drafts WHERE draft_id = ? AND user_id = ? ORDER BY id ASC`,
		latestDraftID, userID,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to load draft group: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	var drafts []PostDraftListResponse
	for rows.Next() {
		d, err := scanDraftFromRows(rows)
		if err != nil {
			continue
		}
		drafts = append(drafts, d)
	}
	if drafts == nil {
		drafts = []PostDraftListResponse{}
	}
	c.JSON(http.StatusOK, drafts)
}

// ListPostDrafts returns all draft versions for a given draft_id, owned by the current user.
func ListPostDrafts(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		c.Abort()
		return
	}

	draftID := c.Param("draft_id")
	rows, err := db.Query(
		`SELECT `+draftListSelectFields+` FROM post_drafts WHERE draft_id = ? AND user_id = ? ORDER BY id ASC`,
		draftID, userID,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to list drafts: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	var drafts []PostDraftListResponse
	for rows.Next() {
		d, err := scanDraftFromRows(rows)
		if err != nil {
			continue
		}
		drafts = append(drafts, d)
	}
	if drafts == nil {
		drafts = []PostDraftListResponse{}
	}
	c.JSON(http.StatusOK, drafts)
}

// GetPostDraft returns a single draft version by its id.
func GetPostDraft(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		c.Abort()
		return
	}

	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid draft ID"})
		c.Abort()
		return
	}

	d, err := scanDraft(db.QueryRow(`SELECT `+draftSelectFields+` FROM post_drafts WHERE id = ? AND user_id = ?`, id, userID))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Draft not found"})
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, d)
}

// CreatePostDraft saves a new draft version (auto-save or manual).
func CreatePostDraft(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		c.Abort()
		return
	}

	var req CreatePostDraftRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	draftID := uuid.New().String()
	if req.DraftID != nil && *req.DraftID != "" {
		draftID = *req.DraftID
	}

	res, err := db.Exec(
		`INSERT INTO post_drafts (draft_id, user_id, character_id, topic_id, is_manual, content) VALUES (?, ?, ?, ?, ?, ?)`,
		draftID, userID, req.CharacterID, req.TopicID, req.IsManual, req.Content,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create draft: " + err.Error()})
		c.Abort()
		return
	}

	newID, _ := res.LastInsertId()
	d, err := scanDraft(db.QueryRow(`SELECT `+draftSelectFields+` FROM post_drafts WHERE id = ?`, newID))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch created draft"})
		c.Abort()
		return
	}
	c.JSON(http.StatusCreated, d)
}

// UpdatePostDraft updates the content or metadata of a draft version.
func UpdatePostDraft(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		c.Abort()
		return
	}

	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid draft ID"})
		c.Abort()
		return
	}

	var req UpdatePostDraftRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	if req.CharacterID != nil {
		db.Exec(`UPDATE post_drafts SET character_id = ? WHERE id = ? AND user_id = ?`, *req.CharacterID, id, userID)
	}
	if req.TopicID != nil {
		db.Exec(`UPDATE post_drafts SET topic_id = ? WHERE id = ? AND user_id = ?`, *req.TopicID, id, userID)
	}
	if req.IsManual != nil {
		db.Exec(`UPDATE post_drafts SET is_manual = ? WHERE id = ? AND user_id = ?`, *req.IsManual, id, userID)
	}
	if req.Content != nil {
		db.Exec(`UPDATE post_drafts SET content = ? WHERE id = ? AND user_id = ?`, *req.Content, id, userID)
	}

	d, err := scanDraft(db.QueryRow(`SELECT `+draftSelectFields+` FROM post_drafts WHERE id = ? AND user_id = ?`, id, userID))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Draft not found"})
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, d)
}

// DeletePostDraft deletes a single draft version.
func DeletePostDraft(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		c.Abort()
		return
	}

	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid draft ID"})
		c.Abort()
		return
	}

	res, err := db.Exec(`DELETE FROM post_drafts WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to delete draft: " + err.Error()})
		c.Abort()
		return
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Draft not found"})
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Draft deleted"})
}

// DeletePostDraftGroup deletes all versions of a draft (by draft_id).
func DeletePostDraftGroup(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		c.Abort()
		return
	}

	draftID := c.Param("draft_id")
	_, err := db.Exec(`DELETE FROM post_drafts WHERE draft_id = ? AND user_id = ?`, draftID, userID)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to delete draft: " + err.Error()})
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Draft deleted"})
}
