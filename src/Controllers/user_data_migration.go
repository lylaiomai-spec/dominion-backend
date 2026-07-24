package Controllers

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	UserDataProcessingStatusPending   = 0
	UserDataProcessingStatusProcessed = 1
	UserDataProcessingStatusPublished = 2
	ForumTypeMyBB                     = 1
)

type UserCharacterMapEntry struct {
	OriginalUserId   string  `json:"original_user_id"`
	OriginalUserName string  `json:"original_user_name"`
	CharacterId      *int    `json:"character_id"`
	CharacterName    *string `json:"character_name"`
	UserId           *int    `json:"user_id"`
}

type UserDataProcessingResponse struct {
	Id                 int                               `json:"id"`
	DateCreated        time.Time                         `json:"date_created"`
	UserId             int                               `json:"user_id"`
	Status             int                               `json:"status"`
	OriginalTopicId    *string                           `json:"original_topic_id"`
	OriginalTopicTitle *string                           `json:"original_topic_title"`
	NewTopicId         *int                              `json:"new_topic_id"`
	OriginalPostCount  *int                              `json:"original_post_count"`
	ParsedPostCount    *int                              `json:"parsed_post_count"`
	ForumDomain        *string                           `json:"forum_domain"`
	DataExtractionUrls []string                          `json:"data_extraction_urls"`
	UserCharacterMap   map[string]UserCharacterMapEntry  `json:"user_character_map"`
}

type CreateUserDataProcessingRequest struct {
	OriginalTopicId   string `json:"original_topic_id" binding:"required"`
	OriginalPostCount int    `json:"original_post_count" binding:"required"`
	ForumDomain       string `json:"forum_domain" binding:"required"`
}

type MybbPostEntry struct {
	Id       string `json:"id"`
	Username string `json:"username"`
	UserId   string `json:"user_id"`
	Message  string `json:"message"`
	Posted   string `json:"posted"`
	TopicId  string `json:"topic_id"`
	ForumId  string `json:"forum_id"`
	Subject  string `json:"subject"`
}

type ProcessMybbTopicRequest struct {
	ProcessingId int    `json:"processing_id" binding:"required"`
	JsonBlob     string `json:"json_blob" binding:"required"`
}

type ProcessingResultResponse struct {
	FoundPosts        int                      `json:"found_posts"`
	SkippedDuplicates int                      `json:"skipped_duplicates"`
	AllParsed         bool                     `json:"all_parsed"`
	FoundUsers        []UserCharacterMapEntry  `json:"found_users"`
}

type PublishUserDataProcessingRequest struct {
	ProcessingId  int            `json:"processing_id" binding:"required"`
	TopicId       int            `json:"topic_id" binding:"required"`
	UserMap       map[string]int `json:"user_map" binding:"required"`
	SkipFirstPost *bool          `json:"skip_first_post"`
}

func GetUserDataProcessingList(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		c.Abort()
		return
	}

	rows, err := db.Query(`
		SELECT id, date_created, user_id, status,
		       original_topic_id, original_topic_title, new_topic_id,
		       original_post_count, parsed_post_count,
		       forum_domain, data_extraction_urls, user_character_map
		FROM user_data_processing
		WHERE user_id = ?
		ORDER BY date_created DESC`,
		userID,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch processing list: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	result := []UserDataProcessingResponse{}
	for rows.Next() {
		var r UserDataProcessingResponse
		var urlsJSON *string
		var charMapJSON *string
		if err := rows.Scan(
			&r.Id, &r.DateCreated, &r.UserId, &r.Status,
			&r.OriginalTopicId, &r.OriginalTopicTitle, &r.NewTopicId,
			&r.OriginalPostCount, &r.ParsedPostCount,
			&r.ForumDomain, &urlsJSON, &charMapJSON,
		); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan processing row: " + err.Error()})
			c.Abort()
			return
		}
		if urlsJSON != nil {
			_ = json.Unmarshal([]byte(*urlsJSON), &r.DataExtractionUrls)
		}
		if charMapJSON != nil {
			_ = json.Unmarshal([]byte(*charMapJSON), &r.UserCharacterMap)
		}
		if r.UserCharacterMap == nil {
			r.UserCharacterMap = map[string]UserCharacterMapEntry{}
		}
		result = append(result, r)
	}

	c.JSON(http.StatusOK, result)
}

func GetUserDataProcessing(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		c.Abort()
		return
	}

	id := c.Param("id")
	var r UserDataProcessingResponse
	var urlsJSON *string
	var charMapJSON *string
	err := db.QueryRow(`
		SELECT id, date_created, user_id, status,
		       original_topic_id, original_topic_title, new_topic_id,
		       original_post_count, parsed_post_count,
		       forum_domain, data_extraction_urls, user_character_map
		FROM user_data_processing
		WHERE id = ?`, id,
	).Scan(
		&r.Id, &r.DateCreated, &r.UserId, &r.Status,
		&r.OriginalTopicId, &r.OriginalTopicTitle, &r.NewTopicId,
		&r.OriginalPostCount, &r.ParsedPostCount,
		&r.ForumDomain, &urlsJSON, &charMapJSON,
	)
	if err == sql.ErrNoRows {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Processing record not found"})
		c.Abort()
		return
	}
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch processing: " + err.Error()})
		c.Abort()
		return
	}
	if r.UserId != userID {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "Access denied"})
		c.Abort()
		return
	}
	if urlsJSON != nil {
		_ = json.Unmarshal([]byte(*urlsJSON), &r.DataExtractionUrls)
	}
	if charMapJSON != nil {
		_ = json.Unmarshal([]byte(*charMapJSON), &r.UserCharacterMap)
	}
	if r.UserCharacterMap == nil {
		r.UserCharacterMap = map[string]UserCharacterMapEntry{}
	}

	c.JSON(http.StatusOK, r)
}

func CreateUserDataProcessing(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		c.Abort()
		return
	}

	var req CreateUserDataProcessingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	now := time.Now()
	urls := generateExtractionUrls(req.ForumDomain, req.OriginalTopicId, req.OriginalPostCount)
	urlsJSON, err := json.Marshal(urls)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to encode URLs: " + err.Error()})
		c.Abort()
		return
	}

	res, err := db.Exec(
		"INSERT INTO user_data_processing (date_created, user_id, status, original_topic_id, original_post_count, forum_domain, data_extraction_urls) VALUES (?, ?, ?, ?, ?, ?, ?)",
		now, userID, UserDataProcessingStatusPending, req.OriginalTopicId, req.OriginalPostCount, req.ForumDomain, urlsJSON,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create processing: " + err.Error()})
		c.Abort()
		return
	}

	id, err := res.LastInsertId()
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get processing ID: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusCreated, UserDataProcessingResponse{
		Id:                 int(id),
		DateCreated:        now,
		UserId:             userID,
		Status:             UserDataProcessingStatusPending,
		OriginalTopicId:    &req.OriginalTopicId,
		OriginalPostCount:  &req.OriginalPostCount,
		ForumDomain:        &req.ForumDomain,
		DataExtractionUrls: urls,
	})
}

func ProcessMybbTopicJson(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		c.Abort()
		return
	}

	var req ProcessMybbTopicRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	var ownerID int
	var originalTopicId string
	var forumDomain string
	err := db.QueryRow("SELECT user_id, COALESCE(original_topic_id, ''), COALESCE(forum_domain, '') FROM user_data_processing WHERE id = ?", req.ProcessingId).Scan(&ownerID, &originalTopicId, &forumDomain)
	if err == sql.ErrNoRows {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Processing record not found"})
		c.Abort()
		return
	}
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to verify processing: " + err.Error()})
		c.Abort()
		return
	}
	if ownerID != userID {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "Access denied"})
		c.Abort()
		return
	}

	var apiResponse struct {
		Response []MybbPostEntry `json:"response"`
	}
	if err := json.Unmarshal([]byte(req.JsonBlob), &apiResponse); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid JSON blob: " + err.Error()})
		c.Abort()
		return
	}
	posts := apiResponse.Response

	now := time.Now()
	usersSeen := map[string]string{}
	savedCount := 0
	skippedCount := 0

	for _, post := range posts {
		if post.TopicId != originalTopicId {
			continue
		}

		var existing int
		_ = db.QueryRow(
			"SELECT COUNT(*) FROM user_data_migration WHERE processing_id = ? AND original_topic_id = ? AND original_post_id = ?",
			req.ProcessingId, post.TopicId, post.Id,
		).Scan(&existing)
		if existing > 0 {
			skippedCount++
			usersSeen[post.UserId] = post.Username
			continue
		}

		bbParsed := Services.ParseMybbHTML(post.Message)
		_, err := db.Exec(`
			INSERT INTO user_data_migration
				(original_forum_domain, original_forum_type, original_topic_id, original_post_id,
				 original_user_id, original_user_name, original_post_content_html, post_content_bb_parsed,
				 is_published, date_processed, processing_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)`,
			forumDomain, ForumTypeMyBB, post.TopicId, post.Id,
			post.UserId, post.Username, post.Message, bbParsed,
			now, req.ProcessingId,
		)
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to save post " + post.Id + ": " + err.Error()})
			c.Abort()
			return
		}
		usersSeen[post.UserId] = post.Username
		savedCount++
	}

	var parsedPostCount int
	_ = db.QueryRow("SELECT COUNT(*) FROM user_data_migration WHERE processing_id = ?", req.ProcessingId).Scan(&parsedPostCount)

	// Merge newly seen users into user_character_map (add missing keys, never overwrite)
	var existingMapJSON *string
	_ = db.QueryRow("SELECT user_character_map FROM user_data_processing WHERE id = ?", req.ProcessingId).Scan(&existingMapJSON)
	charMap := map[string]UserCharacterMapEntry{}
	if existingMapJSON != nil {
		_ = json.Unmarshal([]byte(*existingMapJSON), &charMap)
	}
	for uid, uname := range usersSeen {
		if _, exists := charMap[uid]; !exists {
			charMap[uid] = UserCharacterMapEntry{
				OriginalUserId:   uid,
				OriginalUserName: uname,
			}
		}
	}
	charMapJSON, _ := json.Marshal(charMap)

	// Backfill the topic title from the first post's subject if not already set
	if len(posts) > 0 {
		_, _ = db.Exec(
			"UPDATE user_data_processing SET status = ?, original_topic_title = COALESCE(original_topic_title, ?), parsed_post_count = ?, user_character_map = ? WHERE id = ?",
			UserDataProcessingStatusProcessed, posts[0].Subject, parsedPostCount, charMapJSON, req.ProcessingId,
		)
	} else {
		_, _ = db.Exec(
			"UPDATE user_data_processing SET status = ?, parsed_post_count = ?, user_character_map = ? WHERE id = ?",
			UserDataProcessingStatusProcessed, parsedPostCount, charMapJSON, req.ProcessingId,
		)
	}

	foundUsers := make([]UserCharacterMapEntry, 0, len(usersSeen))
	for uid := range usersSeen {
		foundUsers = append(foundUsers, charMap[uid])
	}

	c.JSON(http.StatusOK, ProcessingResultResponse{
		FoundPosts:        savedCount,
		SkippedDuplicates: skippedCount,
		AllParsed:         true,
		FoundUsers:        foundUsers,
	})
}

func UserDataProcessingPublish(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		c.Abort()
		return
	}

	var req PublishUserDataProcessingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	// Verify the processing record belongs to this user
	var ownerID int
	err := db.QueryRow("SELECT user_id FROM user_data_processing WHERE id = ?", req.ProcessingId).Scan(&ownerID)
	if err == sql.ErrNoRows {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Processing record not found"})
		c.Abort()
		return
	}
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to verify processing: " + err.Error()})
		c.Abort()
		return
	}
	if ownerID != userID {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "Access denied"})
		c.Abort()
		return
	}

	// Verify target topic exists and get its subforum_id, type, and name
	var subforumID int
	var topicType Entities.TopicType
	var topicName string
	err = db.QueryRow("SELECT COALESCE(subforum_id, 0), type, name FROM topics WHERE id = ?", req.TopicId).Scan(&subforumID, &topicType, &topicName)
	if err == sql.ErrNoRows {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Target topic not found"})
		c.Abort()
		return
	}
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to verify topic: " + err.Error()})
		c.Abort()
		return
	}

	// Validate: current user must be participating in the target topic (via character, mask, or as author)
	var userParticipates int
	_ = db.QueryRow(`
		SELECT COUNT(*) FROM topics t
		LEFT JOIN episode_base eb ON eb.topic_id = t.id
		WHERE t.id = ?
		  AND (
		    t.author_user_id = ?
		    OR EXISTS (
		      SELECT 1 FROM episode_character ec
		      JOIN character_base cb ON cb.id = ec.character_id
		      WHERE ec.episode_id = eb.id AND cb.user_id = ?
		    )
		    OR EXISTS (
		      SELECT 1 FROM episode_mask em
		      JOIN character_profile_base cpb ON cpb.id = em.mask_id
		      WHERE em.episode_id = eb.id AND cpb.user_id = ?
		    )
		  )`,
		req.TopicId, userID, userID, userID,
	).Scan(&userParticipates)
	if userParticipates == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "You are not participating in this topic"})
		c.Abort()
		return
	}

	// Validate: all mapped characters must be in the episode character list
	for _, charID := range req.UserMap {
		var charParticipates int
		_ = db.QueryRow(`
			SELECT COUNT(*) FROM episode_character ec
			JOIN episode_base eb ON eb.id = ec.episode_id
			WHERE eb.topic_id = ? AND ec.character_id = ?`,
			req.TopicId, charID,
		).Scan(&charParticipates)
		if charParticipates == 0 {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: fmt.Sprintf("Character %d is not participating in this topic", charID)})
			c.Abort()
			return
		}
	}

	type charResolved struct {
		userID    int
		profileID int
	}
	resolvedChars := make(map[int]charResolved)
	for _, charID := range req.UserMap {
		if _, resolved := resolvedChars[charID]; resolved {
			continue
		}
		var charUserID int
		err := db.QueryRow("SELECT user_id FROM character_base WHERE id = ?", charID).Scan(&charUserID)
		if err == sql.ErrNoRows {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: fmt.Sprintf("Character %d not found", charID)})
			c.Abort()
			return
		}
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: fmt.Sprintf("Failed to resolve character %d: %s", charID, err.Error())})
			c.Abort()
			return
		}
		var profileID int
		err = db.QueryRow("SELECT id FROM character_profile_base WHERE character_id = ? AND is_mask IS NOT TRUE AND is_archived IS NOT TRUE LIMIT 1", charID).Scan(&profileID)
		if err != nil {
			profileID = 0
		}
		resolvedChars[charID] = charResolved{userID: charUserID, profileID: profileID}
	}

	skipFirst := req.SkipFirstPost == nil || *req.SkipFirstPost

	// Fetch all unpublished rows for this processing in original post order
	rows, err := db.Query(`
		SELECT id, original_post_id, original_user_id, COALESCE(post_content_bb_parsed, original_post_content_html, '')
		FROM user_data_migration
		WHERE processing_id = ? AND is_published = 0
		ORDER BY CAST(original_post_id AS UNSIGNED) ASC`,
		req.ProcessingId,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch migration rows: " + err.Error()})
		c.Abort()
		return
	}

	type pendingRow struct {
		migrationId    int
		originalPostId string
		originalUserId string
		content        string
	}
	var pending []pendingRow
	for rows.Next() {
		var r pendingRow
		if err := rows.Scan(&r.migrationId, &r.originalPostId, &r.originalUserId, &r.content); err != nil {
			rows.Close()
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan migration row: " + err.Error()})
			c.Abort()
			return
		}
		pending = append(pending, r)
	}
	rows.Close()

	publishedCount := 0
	skippedCount := 0
	firstPost := true

	for _, row := range pending {
		if skipFirst && firstPost {
			firstPost = false
			continue
		}
		firstPost = false
		charID, mapped := req.UserMap[row.originalUserId]
		if !mapped {
			skippedCount++
			continue
		}
		resolved := resolvedChars[charID]

		tx, err := db.Begin()
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to start transaction: " + err.Error()})
			c.Abort()
			return
		}

		useProfile := resolved.profileID != 0
		var profileIDArg interface{}
		if useProfile {
			profileIDArg = resolved.profileID
		} else {
			profileIDArg = nil
		}
		res, err := tx.Exec(
			"INSERT INTO posts (topic_id, author_user_id, content, date_created, use_character_profile, character_profile_id) VALUES (?, ?, ?, NOW(), ?, ?)",
			req.TopicId, resolved.userID, row.content, useProfile, profileIDArg,
		)
		if err != nil {
			tx.Rollback()
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to insert post: " + err.Error()})
			c.Abort()
			return
		}

		postID, err := res.LastInsertId()
		if err != nil {
			tx.Rollback()
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get post ID: " + err.Error()})
			c.Abort()
			return
		}

		_, err = tx.Exec(`
			UPDATE user_data_migration
			SET user_id = ?, character_id = ?, topic_id = ?, post_id = ?, is_published = 1, date_published = NOW()
			WHERE id = ?`,
			resolved.userID, charID, req.TopicId, postID, row.migrationId,
		)
		if err != nil {
			tx.Rollback()
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update migration record: " + err.Error()})
			c.Abort()
			return
		}

		if err := tx.Commit(); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to commit: " + err.Error()})
			c.Abort()
			return
		}

		// Update stats directly — no notifications, no currency, no websocket broadcast.

		// Global post counter
		_, _ = db.Exec("UPDATE global_stats SET stat_value = stat_value + 1 WHERE stat_name = 'total_post_number'")
		if topicType == Entities.EpisodeTopic {
			_, _ = db.Exec("UPDATE global_stats SET stat_value = stat_value + 1 WHERE stat_name = 'total_episode_post_number'")
		}

		// Topic post count, last-post metadata
		_, _ = db.Exec(`
			UPDATE topics
			SET post_number = post_number + 1,
			    date_last_post = NOW(),
			    last_post_author_user_id = ?
			WHERE id = ?`,
			resolved.userID, req.TopicId)

		// Subforum last-post metadata
		if subforumID != 0 {
			var username string
			_ = db.QueryRow("SELECT username FROM users WHERE id = ?", resolved.userID).Scan(&username)
			_, _ = db.Exec(`
				UPDATE subforums
				SET post_number              = COALESCE(post_number, 0) + 1,
				    last_post_topic_id       = ?,
				    last_post_topic_name     = ?,
				    last_post_id             = ?,
				    date_last_post           = NOW(),
				    last_post_author_user_name = ?,
				    show_last_topic          = false
				WHERE id = ?`,
				req.TopicId, topicName, postID, username, subforumID)
		}

		// User post counter
		if topicType == Entities.EpisodeTopic {
			_, _ = db.Exec("UPDATE users SET total_posts = total_posts + 1 WHERE id = ?", resolved.userID)
		} else {
			_, _ = db.Exec("UPDATE users SET total_general_posts = total_general_posts + 1 WHERE id = ?", resolved.userID)
		}

		// Character post counter (episode topics only)
		if topicType == Entities.EpisodeTopic && charID != 0 {
			_, _ = db.Exec(
				"UPDATE character_base SET total_posts = total_posts + 1, date_last_post = NOW() WHERE id = ?",
				charID)
		}

		publishedCount++
	}

	_, _ = db.Exec(
		"UPDATE user_data_processing SET status = ?, new_topic_id = ? WHERE id = ?",
		UserDataProcessingStatusPublished, req.TopicId, req.ProcessingId,
	)

	c.JSON(http.StatusOK, gin.H{
		"published_posts": publishedCount,
		"skipped_posts":   skippedCount,
	})
}

type UpdateUserCharacterMapRequest struct {
	ProcessingId int            `json:"processing_id" binding:"required"`
	UserMap      map[string]int `json:"user_map" binding:"required"`
}

func UpdateUserCharacterMap(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		c.Abort()
		return
	}

	var req UpdateUserCharacterMapRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	var ownerID int
	err := db.QueryRow("SELECT user_id FROM user_data_processing WHERE id = ?", req.ProcessingId).Scan(&ownerID)
	if err == sql.ErrNoRows {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Processing record not found"})
		c.Abort()
		return
	}
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to verify processing: " + err.Error()})
		c.Abort()
		return
	}
	if ownerID != userID {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "Access denied"})
		c.Abort()
		return
	}

	var existingMapJSON *string
	_ = db.QueryRow("SELECT user_character_map FROM user_data_processing WHERE id = ?", req.ProcessingId).Scan(&existingMapJSON)
	charMap := map[string]UserCharacterMapEntry{}
	if existingMapJSON != nil {
		_ = json.Unmarshal([]byte(*existingMapJSON), &charMap)
	}

	for uid, charID := range req.UserMap {
		var charName string
		var charUserID int
		err := db.QueryRow("SELECT name, user_id FROM character_base WHERE id = ?", charID).Scan(&charName, &charUserID)
		if err == sql.ErrNoRows {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: fmt.Sprintf("Character %d not found", charID)})
			c.Abort()
			return
		}
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: fmt.Sprintf("Failed to resolve character %d: %s", charID, err.Error())})
			c.Abort()
			return
		}
		id := charID
		existing := charMap[uid]
		charMap[uid] = UserCharacterMapEntry{
			OriginalUserId:   existing.OriginalUserId,
			OriginalUserName: existing.OriginalUserName,
			CharacterId:      &id,
			CharacterName:    &charName,
			UserId:           &charUserID,
		}
	}

	charMapJSON, _ := json.Marshal(charMap)
	_, err = db.Exec("UPDATE user_data_processing SET user_character_map = ? WHERE id = ?", charMapJSON, req.ProcessingId)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update character map: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"user_character_map": charMap})
}

func generateExtractionUrls(domain, topicId string, postCount int) []string {
	base := fmt.Sprintf("https://%s/api.php?method=post.get&topic_id=%s&limit=100", domain, topicId)
	urls := []string{base}
	for skip := 100; skip < postCount; skip += 100 {
		urls = append(urls, fmt.Sprintf("%s&skip=%d", base, skip))
	}
	return urls
}
