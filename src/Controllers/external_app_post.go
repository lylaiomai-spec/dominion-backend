package Controllers

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Events"
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"database/sql"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// authenticateExternalApp validates the API key and that the app is active.
// Returns the app ID on success.
func authenticateExternalApp(c *gin.Context, db *sql.DB) (int, bool) {
	apiKey := c.GetHeader("X-Api-Key")
	if apiKey == "" {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Missing API key"})
		c.Abort()
		return 0, false
	}

	var appId int
	var appStatus bool
	err := db.QueryRow("SELECT id, status FROM external_apps WHERE api_key = ?", apiKey).
		Scan(&appId, &appStatus)
	if err == sql.ErrNoRows {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Invalid API key"})
		c.Abort()
		return 0, false
	}
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to authenticate"})
		c.Abort()
		return 0, false
	}
	if !appStatus {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "External app is inactive"})
		c.Abort()
		return 0, false
	}

	return appId, true
}

func ExternalAppPost(c *gin.Context, db *sql.DB) {
	appId, ok := authenticateExternalApp(c, db)
	if !ok {
		return
	}

	var appUserId sql.NullInt64
	if err := db.QueryRow("SELECT user_id FROM external_apps WHERE id = ?", appId).Scan(&appUserId); err != nil || !appUserId.Valid {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "External app has no associated user"})
		c.Abort()
		return
	}
	userId := int(appUserId.Int64)

	var req struct {
		TopicId int    `json:"topic_id" binding:"required"`
		Content string `json:"content" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	var subforumId int
	var topicStatus Entities.TopicStatus
	if err := db.QueryRow("SELECT COALESCE(subforum_id, 0), status FROM topics WHERE id = ?", req.TopicId).
		Scan(&subforumId, &topicStatus); err != nil {
		if err == sql.ErrNoRows {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Topic not found"})
		} else {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to check topic"})
		}
		c.Abort()
		return
	}
	if topicStatus == Entities.FullTopic {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "Topic has reached its post limit"})
		c.Abort()
		return
	}

	var hasPermission bool
	if err := db.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM external_app_permissions WHERE external_app_id = ? AND subforum_id = ? AND permission = ?)",
		appId, subforumId, string(Entities.ExternalAppPermissionPost),
	).Scan(&hasPermission); err != nil || !hasPermission {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "External app does not have post permission for this subforum"})
		c.Abort()
		return
	}

	// Escape raw HTML before storing; BB codes use [] so they are unaffected
	content := html.EscapeString(req.Content)

	res, err := db.Exec(
		"INSERT INTO posts (topic_id, author_user_id, content, date_created, use_character_profile) VALUES (?, ?, ?, NOW(), false)",
		req.TopicId, userId, content,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create post: " + err.Error()})
		c.Abort()
		return
	}
	postId, _ := res.LastInsertId()

	fullPost, postErr := Services.GetPostById(int(postId), db, Services.IsFeatureEnabled("currency", c))
	if postErr != nil {
		fmt.Printf("Error getting post details for event publishing: %v\n", postErr)
	} else {
		Events.Publish(db, Events.PostCreated, Events.PostCreatedEvent{
			Type:       "post_created",
			TopicID:    int64(req.TopicId),
			SubforumID: subforumId,
			Post:       *fullPost,
		})
	}

	c.JSON(http.StatusCreated, gin.H{"post_id": postId})
}

func ExternalAppGetTopicFirstPost(c *gin.Context, db *sql.DB) {
	appId, ok := authenticateExternalApp(c, db)
	if !ok {
		return
	}

	topicId, err := strconv.Atoi(c.Query("topic_id"))
	if err != nil || topicId <= 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid topic_id"})
		c.Abort()
		return
	}

	var subforumId sql.NullInt64
	if err := db.QueryRow("SELECT subforum_id FROM topics WHERE id = ?", topicId).Scan(&subforumId); err != nil {
		if err == sql.ErrNoRows {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Topic not found"})
		} else {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to check topic"})
		}
		c.Abort()
		return
	}

	var hasPermission bool
	if err := db.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM external_app_permissions WHERE external_app_id = ? AND subforum_id = ? AND permission = ?)",
		appId, subforumId, string(Entities.ExternalAppPermissionGetFirstPost),
	).Scan(&hasPermission); err != nil || !hasPermission {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "External app does not have get_first_post permission for this subforum"})
		c.Abort()
		return
	}

	var postId int
	var content string
	err = db.QueryRow(
		"SELECT id, content FROM posts WHERE topic_id = ? AND (is_deleted IS NULL OR is_deleted = 0) ORDER BY id ASC LIMIT 1",
		topicId,
	).Scan(&postId, &content)
	if err == sql.ErrNoRows {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "No posts found for topic"})
		c.Abort()
		return
	}
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get first post"})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"post_id": postId, "content": content, "content_html": Services.ParseBBCode(content)})
}

func ExternalAppGetActiveTopics(c *gin.Context, db *sql.DB) {
	appId, ok := authenticateExternalApp(c, db)
	if !ok {
		return
	}

	// Collect subforums this app has get_active_topics permission for
	permRows, err := db.Query(
		"SELECT subforum_id FROM external_app_permissions WHERE external_app_id = ? AND permission = ?",
		appId, string(Entities.ExternalAppPermissionGetActiveTopics),
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to load permissions: " + err.Error()})
		c.Abort()
		return
	}
	defer permRows.Close()

	permittedMap := make(map[int]bool)
	for permRows.Next() {
		var sid int
		if err := permRows.Scan(&sid); err == nil {
			permittedMap[sid] = true
		}
	}

	if len(permittedMap) == 0 {
		c.JSON(http.StatusOK, []ViewforumRow{})
		return
	}

	// Optional subforum_ids filter — intersect with permitted set
	var subforumIDs []int
	if raw := c.Query("subforum_ids"); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			if id, err := strconv.Atoi(strings.TrimSpace(part)); err == nil && permittedMap[id] {
				subforumIDs = append(subforumIDs, id)
			}
		}
		if len(subforumIDs) == 0 {
			c.JSON(http.StatusOK, []ViewforumRow{})
			return
		}
	} else {
		for id := range permittedMap {
			subforumIDs = append(subforumIDs, id)
		}
	}

	page := 1
	if pageStr := c.Query("page"); pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}

	limit := Services.GetPostsPerPage(db)
	offset := (page - 1) * limit

	placeholders := strings.Repeat("?,", len(subforumIDs)-1) + "?"
	query := fmt.Sprintf(`
		SELECT t.id, t.status, t.name, t.type, t.date_last_post, t.post_number,
		       t.author_user_id, u.username,
		       t.last_post_author_user_id, u2.username,
		       COALESCE(t.last_post_id, (SELECT MAX(id) FROM posts WHERE topic_id = t.id))
		FROM topics t
		JOIN users u ON t.author_user_id = u.id
		LEFT JOIN users u2 ON t.last_post_author_user_id = u2.id
		WHERE t.subforum_id IN (%s)
		  AND t.status = ?
		ORDER BY t.date_last_post DESC
		LIMIT ? OFFSET ?
	`, placeholders)

	args := make([]interface{}, 0, len(subforumIDs)+3)
	for _, id := range subforumIDs {
		args = append(args, id)
	}
	args = append(args, Entities.ActiveTopic, limit, offset)

	rows, err := db.Query(query, args...)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get active topics: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	var topics []ViewforumRow
	for rows.Next() {
		var t ViewforumRow
		if err := rows.Scan(
			&t.Id, &t.Status, &t.Name, &t.Type, &t.DateLastPost, &t.PostNumber,
			&t.AuthorUserId, &t.AuthorUsername,
			&t.LastPostAuthorUserId, &t.LastPostAuthorUsername,
			&t.LastPostId,
		); err != nil {
			continue
		}
		topics = append(topics, t)
	}

	if topics == nil {
		topics = []ViewforumRow{}
	}

	c.JSON(http.StatusOK, topics)
}
