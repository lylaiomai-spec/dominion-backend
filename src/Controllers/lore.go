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
	Name     string `json:"name"`
	IsHidden bool   `json:"is_hidden"`
	Order    int    `json:"order"`
}

type LorePage struct {
	PostId   int64  `json:"post_id"`
	Name     string `json:"name"`
	IsHidden bool   `json:"is_hidden"`
	Order    int    `json:"order"`
}

func GetLorePagesByTopic(c *gin.Context, db *sql.DB) {
	topicID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid topic ID"})
		c.Abort()
		return
	}

	rows, err := db.Query(
		"SELECT post_id, name, is_hidden, position FROM lore_pages WHERE topic_id = ? AND is_hidden = false ORDER BY position ASC",
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
		if err := rows.Scan(&p.PostId, &p.Name, &p.IsHidden, &p.Order); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan lore page: " + err.Error()})
			c.Abort()
			return
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
		SELECT p.id, p.date_created, lp.name, lp.is_hidden, lp.position
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
		if err := rows.Scan(&row.Id, &row.DateCreated, &lpName, &lpIsHidden, &lpOrder); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan row: " + err.Error()})
			c.Abort()
			return
		}
		if lpName.Valid {
			row.LorePage = &LorePageInfo{Name: lpName.String, IsHidden: lpIsHidden.Bool, Order: int(lpOrder.Int64)}
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

	c.JSON(http.StatusOK, gin.H{"message": "Topic updated successfully"})
}

type CreateLorePageRequest struct {
	TopicId  int64  `json:"topic_id" binding:"required"`
	PostId   int64  `json:"post_id" binding:"required"`
	Name     string `json:"name" binding:"required"`
	IsHidden bool   `json:"is_hidden"`
	Order    int    `json:"order"`
}

type UpdateLorePageRequest struct {
	Name     string `json:"name" binding:"required"`
	IsHidden bool   `json:"is_hidden"`
	Order    int    `json:"order"`
}

func CreateLorePage(c *gin.Context, db *sql.DB) {
	var req CreateLorePageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	_, err := db.Exec(
		"INSERT INTO lore_pages (topic_id, post_id, name, is_hidden, position) VALUES (?, ?, ?, ?, ?)",
		req.TopicId, req.PostId, req.Name, req.IsHidden, req.Order,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create lore page: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, LorePage{PostId: req.PostId, Name: req.Name, IsHidden: req.IsHidden, Order: req.Order})
}

func isFirstLorePost(db *sql.DB, postId int64) (bool, error) {
	var firstPostId int64
	err := db.QueryRow(
		"SELECT MIN(p.id) FROM posts p JOIN lore_pages lp ON lp.post_id = p.id WHERE p.topic_id = (SELECT topic_id FROM posts WHERE id = ?)",
		postId,
	).Scan(&firstPostId)
	if err != nil {
		return false, err
	}
	return postId == firstPostId, nil
}

func UpdateLorePage(c *gin.Context, db *sql.DB) {
	postId, err := strconv.ParseInt(c.Param("post_id"), 10, 64)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid post ID"})
		c.Abort()
		return
	}

	var req UpdateLorePageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	isFirst, err := isFirstLorePost(db, postId)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to check post position: " + err.Error()})
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
		result, err = db.Exec("UPDATE lore_pages SET name = ?, position = ? WHERE post_id = ?", req.Name, req.Order, postId)
	} else {
		result, err = db.Exec("UPDATE lore_pages SET name = ?, is_hidden = ?, position = ? WHERE post_id = ?", req.Name, req.IsHidden, req.Order, postId)
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

	isHidden := req.IsHidden
	if isFirst {
		isHidden = false
	}
	c.JSON(http.StatusOK, LorePage{PostId: postId, Name: req.Name, IsHidden: isHidden, Order: req.Order})
}

func DeleteLorePage(c *gin.Context, db *sql.DB) {
	postId, err := strconv.ParseInt(c.Param("post_id"), 10, 64)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid post ID"})
		c.Abort()
		return
	}

	isFirst, err := isFirstLorePost(db, postId)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to check post position: " + err.Error()})
		c.Abort()
		return
	}
	if isFirst {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "The index lore page cannot be deleted"})
		c.Abort()
		return
	}

	result, err := db.Exec("DELETE FROM lore_pages WHERE post_id = ?", postId)
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
