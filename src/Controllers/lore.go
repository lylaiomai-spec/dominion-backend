package Controllers

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Events"
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type CreateLoreTopicRequest struct {
	SubforumId        int    `json:"subforum_id" binding:"required"`
	Title             string `json:"title" binding:"required"`
	Content           string `json:"content" binding:"required"`
	IsStickyFirstPost bool   `json:"is_sticky_first_post"`
}

type UpdateLoreTopicRequest struct {
	Name              *string               `json:"name"`
	Status            *Entities.TopicStatus `json:"status"`
	IsStickyFirstPost *bool                 `json:"is_sticky_first_post"`
}

type LorePageInfo struct {
	Name            string  `json:"name"`
	IsHidden        bool    `json:"is_hidden"`
	Order           int     `json:"order"`
	IsExternalLink  *bool   `json:"is_external_link"`
	ExternalLink    *string `json:"external_link"`
}

type LorePage struct {
	Id              int64   `json:"id"`
	PostId          *int64  `json:"post_id"`
	Name            string  `json:"name"`
	IsHidden        bool    `json:"is_hidden"`
	Order           int     `json:"order"`
	IsExternalLink  *bool   `json:"is_external_link"`
	ExternalLink    *string `json:"external_link"`
}

func GetLorePagesByTopic(c *gin.Context, db *sql.DB) {
	topicID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid topic ID"})
		c.Abort()
		return
	}

	rows, err := db.Query(
		"SELECT id, post_id, name, is_hidden, position, is_external_link, external_link FROM lore_pages WHERE topic_id = ? AND is_hidden = false ORDER BY position ASC",
		topicID,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get lore pages: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	var list []LorePage
	for rows.Next() {
		var p LorePage
		var isExternalLink sql.NullBool
		var externalLink sql.NullString
		if err := rows.Scan(&p.Id, &p.PostId, &p.Name, &p.IsHidden, &p.Order, &isExternalLink, &externalLink); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan lore page: " + err.Error()})
			c.Abort()
			return
		}
		if isExternalLink.Valid {
			v := isExternalLink.Bool
			p.IsExternalLink = &v
		}
		if externalLink.Valid {
			p.ExternalLink = &externalLink.String
		}
		list = append(list, p)
	}

	if list == nil {
		list = []LorePage{}
	}

	c.JSON(http.StatusOK, list)
}

type LoreTopicPostRow struct {
	Id          int64         `json:"id"`
	DateCreated time.Time     `json:"date_created"`
	LorePage    *LorePageInfo `json:"lore_page"`
}

func GetLoreTopicPosts(c *gin.Context, db *sql.DB) {
	topicID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid topic ID"})
		c.Abort()
		return
	}

	var topicType Entities.TopicType
	if err := db.QueryRow("SELECT type FROM topics WHERE id = ?", topicID).Scan(&topicType); err != nil {
		if err == sql.ErrNoRows {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Topic not found"})
		} else {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch topic: " + err.Error()})
		}
		c.Abort()
		return
	}
	if topicType != Entities.LoreTopic {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Topic is not a lore topic"})
		c.Abort()
		return
	}

	rows, err := db.Query(`
		SELECT p.id, p.date_created, lp.name, lp.is_hidden, lp.position, lp.is_external_link, lp.external_link
		FROM posts p
		LEFT JOIN lore_pages lp ON lp.topic_id = p.topic_id AND lp.post_id = p.id
		WHERE p.topic_id = ? AND (p.is_deleted IS NULL OR p.is_deleted = 0)
		ORDER BY p.date_created ASC`, topicID)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get posts: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	var list []LoreTopicPostRow
	for rows.Next() {
		var row LoreTopicPostRow
		var lpName sql.NullString
		var lpIsHidden sql.NullBool
		var lpOrder sql.NullInt64
		var lpIsExternalLink sql.NullBool
		var lpExternalLink sql.NullString
		if err := rows.Scan(&row.Id, &row.DateCreated, &lpName, &lpIsHidden, &lpOrder, &lpIsExternalLink, &lpExternalLink); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan row: " + err.Error()})
			c.Abort()
			return
		}
		if lpName.Valid {
			info := &LorePageInfo{
				Name:     lpName.String,
				IsHidden: lpIsHidden.Bool,
				Order:    int(lpOrder.Int64),
			}
			if lpIsExternalLink.Valid {
				v := lpIsExternalLink.Bool
				info.IsExternalLink = &v
			}
			if lpExternalLink.Valid {
				info.ExternalLink = &lpExternalLink.String
			}
			row.LorePage = info
		}
		list = append(list, row)
	}

	if list == nil {
		list = []LoreTopicPostRow{}
	}

	c.JSON(http.StatusOK, list)
}

func CreateLoreTopic(c *gin.Context, db *sql.DB) {
	var req CreateLoreTopicRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	if hasPerm, err := Services.HasPermission(userID, fmt.Sprintf("subforum_create_lore_topic:%d", req.SubforumId), db); err != nil || !hasPerm {
		c.JSON(http.StatusForbidden, gin.H{"error": "You don't have permission to create lore topics in this subforum"})
		return
	}

	var username string
	if err := db.QueryRow("SELECT username FROM users WHERE id = ?", userID).Scan(&username); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get user details: " + err.Error()})
		return
	}

	tx, err := db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
		return
	}
	defer tx.Rollback()

	res, err := tx.Exec(
		"INSERT INTO topics (subforum_id, name, author_user_id, date_created, date_last_post, status, type, post_number, last_post_author_user_id, is_sticky_first_post) VALUES (?, ?, ?, NOW(), NOW(), 0, 4, 1, ?, ?)",
		req.SubforumId, req.Title, userID, userID, req.IsStickyFirstPost,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert topic: " + err.Error()})
		return
	}
	topicID, err := res.LastInsertId()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get topic ID"})
		return
	}

	res, err = tx.Exec(
		"INSERT INTO posts (topic_id, author_user_id, content, date_created) VALUES (?, ?, ?, NOW())",
		topicID, userID, req.Content,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert post: " + err.Error()})
		return
	}
	postID, err := res.LastInsertId()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get post ID"})
		return
	}

	if _, err := tx.Exec(
		"INSERT INTO lore_pages (topic_id, post_id, name, is_hidden, position) VALUES (?, ?, 'Index', false, 0)",
		topicID, postID,
	); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create index lore page: " + err.Error()})
		return
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
		return
	}

	Events.Publish(db, Events.TopicCreated, Events.TopicCreatedEvent{
		Type:       "topic_created",
		TopicID:    topicID,
		SubforumID: req.SubforumId,
		Title:      req.Title,
		PostID:     postID,
		UserID:     userID,
		Username:   username,
	})

	c.JSON(http.StatusCreated, gin.H{"message": "Lore topic created successfully", "topic_id": topicID})
}

func UpdateLoreTopic(c *gin.Context, db *sql.DB) {
	topicID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid topic ID"})
		c.Abort()
		return
	}

	var req UpdateLoreTopicRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		c.Abort()
		return
	}

	var authorUserID int
	var subforumID int
	var topicType Entities.TopicType
	if err := db.QueryRow("SELECT author_user_id, subforum_id, type FROM topics WHERE id = ?", topicID).Scan(&authorUserID, &subforumID, &topicType); err != nil {
		if err == sql.ErrNoRows {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Topic not found"})
		} else {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch topic details: " + err.Error()})
		}
		c.Abort()
		return
	}

	var currentStatus Entities.TopicStatus
	if err := db.QueryRow("SELECT status FROM topics WHERE id = ?", topicID).Scan(&currentStatus); err == nil {
		if currentStatus == Entities.FullTopic {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "Cannot update a full topic"})
			c.Abort()
			return
		}
	}

	if topicType != Entities.LoreTopic {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Only lore topics can be updated via this endpoint"})
		c.Abort()
		return
	}

	canEdit := false
	if userID == authorUserID {
		permission := fmt.Sprintf("subforum_edit_own_topic:%d", subforumID)
		if hasPerm, err := Services.HasPermission(userID, permission, db); err == nil && hasPerm {
			canEdit = true
		}
	} else {
		permission := fmt.Sprintf("subforum_edit_others_topic:%d", subforumID)
		if hasPerm, err := Services.HasPermission(userID, permission, db); err == nil && hasPerm {
			canEdit = true
		}
	}

	if !canEdit {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "You do not have permission to edit this topic"})
		c.Abort()
		return
	}

	if req.Status != nil && *req.Status == Entities.FullTopic {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "FullTopic status cannot be set manually"})
		c.Abort()
		return
	}

	var setClauses []string
	var args []interface{}

	if req.Name != nil {
		setClauses = append(setClauses, "name = ?")
		args = append(args, *req.Name)
	}
	if req.Status != nil {
		setClauses = append(setClauses, "status = ?")
		args = append(args, *req.Status)
	}
	if req.IsStickyFirstPost != nil {
		setClauses = append(setClauses, "is_sticky_first_post = ?")
		args = append(args, *req.IsStickyFirstPost)
	}

	if len(setClauses) == 0 {
		c.JSON(http.StatusOK, gin.H{"message": "Topic updated successfully"})
		return
	}

	args = append(args, topicID)
	if _, err := db.Exec("UPDATE topics SET "+strings.Join(setClauses, ", ")+" WHERE id = ?", args...); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update topic: " + err.Error()})
		c.Abort()
		return
	}

	if req.Name != nil {
		_, _ = db.Exec(
			"UPDATE subforums SET last_post_topic_name = ? WHERE last_post_topic_id = ?",
			*req.Name, topicID,
		)
	}

	if req.Status != nil {
		Events.Publish(db, Events.TopicStatusChanged, Events.TopicStatusChangedEvent{
			TopicID:    int64(topicID),
			SubforumID: subforumID,
			OldStatus:  int(currentStatus),
			NewStatus:  int(*req.Status),
		})
	}

	c.JSON(http.StatusOK, gin.H{"message": "Topic updated successfully"})
}

type CreateLorePageRequest struct {
	TopicId         int64   `json:"topic_id" binding:"required"`
	PostId          *int64  `json:"post_id"`
	Name            string  `json:"name" binding:"required"`
	IsHidden        bool    `json:"is_hidden"`
	Order           int     `json:"order"`
	IsExternalLink  *bool   `json:"is_external_link"`
	ExternalLink    *string `json:"external_link"`
}

type UpdateLorePageRequest struct {
	Name            string  `json:"name" binding:"required"`
	IsHidden        bool    `json:"is_hidden"`
	Order           int     `json:"order"`
	IsExternalLink  *bool   `json:"is_external_link"`
	ExternalLink    *string `json:"external_link"`
}

func CreateLorePage(c *gin.Context, db *sql.DB) {
	var req CreateLorePageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	isExternal := req.IsExternalLink != nil && *req.IsExternalLink
	if isExternal {
		if req.ExternalLink == nil || *req.ExternalLink == "" {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "external_link is required for external link pages"})
			c.Abort()
			return
		}
	} else {
		if req.PostId == nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "post_id is required for non-external pages"})
			c.Abort()
			return
		}
	}

	res, err := db.Exec(
		"INSERT INTO lore_pages (topic_id, post_id, name, is_hidden, position, is_external_link, external_link) VALUES (?, ?, ?, ?, ?, ?, ?)",
		req.TopicId, req.PostId, req.Name, req.IsHidden, req.Order, req.IsExternalLink, req.ExternalLink,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create lore page: " + err.Error()})
		c.Abort()
		return
	}

	id, _ := res.LastInsertId()
	c.JSON(http.StatusOK, LorePage{
		Id:             id,
		PostId:         req.PostId,
		Name:           req.Name,
		IsHidden:       req.IsHidden,
		Order:          req.Order,
		IsExternalLink: req.IsExternalLink,
		ExternalLink:   req.ExternalLink,
	})
}

func isFirstLorePage(db *sql.DB, pageID int64) (bool, error) {
	var firstID int64
	err := db.QueryRow(
		"SELECT MIN(id) FROM lore_pages WHERE topic_id = (SELECT topic_id FROM lore_pages WHERE id = ?)",
		pageID,
	).Scan(&firstID)
	if err != nil {
		return false, err
	}
	return pageID == firstID, nil
}

func UpdateLorePage(c *gin.Context, db *sql.DB) {
	pageID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid lore page ID"})
		c.Abort()
		return
	}

	var req UpdateLorePageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	isFirst, err := isFirstLorePage(db, pageID)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to check page position: " + err.Error()})
		c.Abort()
		return
	}
	if isFirst && req.IsHidden {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "The index lore page cannot be hidden"})
		c.Abort()
		return
	}

	var result sql.Result
	if isFirst {
		result, err = db.Exec(
			"UPDATE lore_pages SET name = ?, position = ?, is_external_link = ?, external_link = ? WHERE id = ?",
			req.Name, req.Order, req.IsExternalLink, req.ExternalLink, pageID,
		)
	} else {
		result, err = db.Exec(
			"UPDATE lore_pages SET name = ?, is_hidden = ?, position = ?, is_external_link = ?, external_link = ? WHERE id = ?",
			req.Name, req.IsHidden, req.Order, req.IsExternalLink, req.ExternalLink, pageID,
		)
	}
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update lore page: " + err.Error()})
		c.Abort()
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Lore page not found"})
		c.Abort()
		return
	}

	// Re-read updated row to return current state
	var p LorePage
	var isExternalLink sql.NullBool
	var externalLink sql.NullString
	_ = db.QueryRow(
		"SELECT id, post_id, name, is_hidden, position, is_external_link, external_link FROM lore_pages WHERE id = ?", pageID,
	).Scan(&p.Id, &p.PostId, &p.Name, &p.IsHidden, &p.Order, &isExternalLink, &externalLink)
	if isExternalLink.Valid {
		v := isExternalLink.Bool
		p.IsExternalLink = &v
	}
	if externalLink.Valid {
		p.ExternalLink = &externalLink.String
	}
	c.JSON(http.StatusOK, p)
}

func DeleteLorePage(c *gin.Context, db *sql.DB) {
	pageID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid lore page ID"})
		c.Abort()
		return
	}

	isFirst, err := isFirstLorePage(db, pageID)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to check page position: " + err.Error()})
		c.Abort()
		return
	}
	if isFirst {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "The index lore page cannot be deleted"})
		c.Abort()
		return
	}

	result, err := db.Exec("DELETE FROM lore_pages WHERE id = ?", pageID)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to delete lore page: " + err.Error()})
		c.Abort()
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Lore page not found"})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Lore page deleted successfully"})
}
