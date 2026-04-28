package Controllers

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Events"
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

var mentionRegexp = regexp.MustCompile(`@[^\x{200A}]+\x{200A}`)

type ViewforumRow struct {
	Id                     int                  `json:"id"`
	Status                 Entities.TopicStatus `json:"status"`
	Name                   string               `json:"name"`
	Type                   Entities.TopicType   `json:"type"`
	DateLastPost           *time.Time           `json:"date_last_post"`
	DateLastPostLocalized  *string              `json:"date_last_post_localized,omitempty"`
	PostNumber             int                  `json:"post_number"`
	AuthorUserId           int                  `json:"author_user_id"`
	AuthorUsername         string               `json:"author_username"`
	LastPostAuthorUserId   *int                 `json:"last_post_author_user_id"`
	LastPostAuthorUsername *string              `json:"last_post_author_username"`
	LastPostId             *int                 `json:"last_post_id"`
	NotViewed              bool                 `json:"not_viewed"`
	LastViewedId           *int                 `json:"last_viewed_id"`
	IsSticky               bool                 `json:"is_sticky"`
	IsStickyFirstPost      bool                 `json:"is_sticky_first_post"`
	FirstPostId            *int                 `json:"first_post_id"`
}

type CreateTopicRequest struct {
	SubforumId        int    `json:"subforum_id" binding:"required"`
	Title             string `json:"title" binding:"required"`
	Content           string `json:"content" binding:"required"`
	IsStickyFirstPost bool   `json:"is_sticky_first_post"`
}

type CreatePostRequest struct {
	TopicID             int     `json:"topic_id" binding:"required"`
	Content             string  `json:"content" binding:"required"`
	UseCharacterProfile bool    `json:"use_character_profile"`
	CharacterProfileID  *int    `json:"character_profile_id"`
	GuestName           *string `json:"guest_name"`
}

type UpdatePostRequest struct {
	Content string `json:"content" binding:"required"`
}

type UpdateTopicRequest struct {
	Name              *string               `json:"name"`
	Status            *Entities.TopicStatus `json:"status"`
	IsStickyFirstPost *bool                 `json:"is_sticky_first_post"`
}

type PostRow struct {
	Id             int       `json:"id"`
	AuthorUserId   int       `json:"author_user_id"`
	AuthorUsername string    `json:"author_username"`
	Content        string    `json:"content"`
	ContentHtml    string    `json:"content_html"`
	DatePosted     time.Time `json:"date_posted"`
}

func GetTopicsBySubforum(c *gin.Context, db *sql.DB) {
	subforumStr := c.Param("subforum")
	pageStr := c.Param("page")
	subforum64, err := strconv.ParseInt(subforumStr, 10, 0)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Incorrect subforum parameter"})
		return
	}
	subforum := int(subforum64)
	page64, err := strconv.ParseInt(pageStr, 10, 0)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Incorrect page parameter"})
		return
	}
	page := int(page64) - 1

	userID := Services.GetUserIdFromContext(c)
	userTimezone := Services.GetUserTimezone(userID, db)

	var topics []ViewforumRow

	limit := 30
	query := `
		SELECT topics.id, topics.status, topics.name, topics.type, topics.date_last_post, topics.post_number,
		       topics.author_user_id, u.username as author_username,
		       topics.last_post_author_user_id, u2.username as last_post_author_username,
		       COALESCE(topics.last_post_id, (SELECT MAX(id) FROM posts WHERE topic_id = topics.id)) as last_post_id,
		       (CASE WHEN ? != 0 AND (utv.post_id IS NULL OR utv.post_id < COALESCE(topics.last_post_id, (SELECT MAX(id) FROM posts WHERE topic_id = topics.id))) THEN 1 ELSE 0 END) as not_viewed,
		       utv.post_id as last_viewed_id,
		       COALESCE(topics.is_sticky, false) as is_sticky,
		       COALESCE(topics.is_sticky_first_post, false) as is_sticky_first_post,
		       (SELECT MIN(id) FROM posts WHERE topic_id = topics.id AND (is_deleted IS NULL OR is_deleted = 0)) as first_post_id
		FROM topics
		JOIN users u ON topics.author_user_id = u.id
		LEFT JOIN users u2 ON topics.last_post_author_user_id = u2.id
		LEFT JOIN user_topic_view utv ON topics.id = utv.topic_id AND utv.user_id = ?
		WHERE subforum_id = ?
		ORDER BY COALESCE(topics.is_sticky, false) DESC, date_last_post DESC
		LIMIT ? OFFSET ?
	`
	rows, err := db.Query(query, userID, userID, subforum, limit, page*limit)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get topics: " + err.Error()})
		return
	}

	defer rows.Close()

	for rows.Next() {
		var topic ViewforumRow
		err := rows.Scan(
			&topic.Id,
			&topic.Status,
			&topic.Name,
			&topic.Type,
			&topic.DateLastPost,
			&topic.PostNumber,
			&topic.AuthorUserId,
			&topic.AuthorUsername,
			&topic.LastPostAuthorUserId,
			&topic.LastPostAuthorUsername,
			&topic.LastPostId,
			&topic.NotViewed,
			&topic.LastViewedId,
			&topic.IsSticky,
			&topic.IsStickyFirstPost,
			&topic.FirstPostId,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan topics: " + err.Error()})
			return
		}
		if topic.DateLastPost != nil {
			localized := Services.LocalizeTime(*topic.DateLastPost, userTimezone)
			topic.DateLastPostLocalized = &localized
		}
		topics = append(topics, topic)
	}

	c.JSON(http.StatusOK, topics)
}

func CreateTopic(c *gin.Context, db *sql.DB) {
	var req CreateTopicRequest
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
	err := db.QueryRow("SELECT username FROM users WHERE id = ?", userID).Scan(&username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get user details: " + err.Error()})
		return
	}

	var authorUsername string
	// Fetch username for notification purposes
	_ = db.QueryRow("SELECT username FROM users WHERE id = ?", userID).Scan(&authorUsername)

	tx, err := db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
		return
	}
	defer tx.Rollback()

	// Insert Topic
	res, err := tx.Exec("INSERT INTO topics (subforum_id, name, author_user_id, date_created, date_last_post, status, type, post_number, last_post_author_user_id, is_sticky_first_post) VALUES (?, ?, ?, NOW(), NOW(), 0, 0, 1, ?, ?)",
		req.SubforumId, req.Title, userID, userID, req.IsStickyFirstPost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert topic: " + err.Error()})
		return
	}
	topicID, err := res.LastInsertId()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get topic ID"})
		return
	}

	// Insert Post
	res, err = tx.Exec("INSERT INTO posts (topic_id, author_user_id, content, date_created) VALUES (?, ?, ?, NOW())",
		topicID, userID, req.Content)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert post: " + err.Error()})
		return
	}
	postID, err := res.LastInsertId()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get post ID"})
		return
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
		return
	}

	// Publish event to update stats asynchronously
	Events.Publish(db, Events.TopicCreated, Events.TopicCreatedEvent{
		Type:       "topic_created",
		TopicID:    topicID,
		SubforumID: req.SubforumId,
		Title:      req.Title,
		PostID:     postID,
		UserID:     userID,
		Username:   username,
	})

	c.JSON(http.StatusCreated, gin.H{"message": "Topic created successfully", "topic_id": topicID})
}

func GetPostsByTopic(c *gin.Context, db *sql.DB) {
	topicIDStr := c.Param("id")
	pageStr := c.Query("page")
	postIDStr := c.Query("post_id")

	topicID, err := strconv.Atoi(topicIDStr)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid topic ID"})
		c.Abort()
		return
	}

	postsPerPage := Services.GetPostsPerPage(db)
	page := 1

	if pageStr != "" {
		page, _ = strconv.Atoi(pageStr)
	} else if postIDStr != "" {
		postID, err := strconv.Atoi(postIDStr)
		if err == nil {
			// Determine the page based on the post's position in the topic
			var position int
			query := "SELECT COUNT(*) FROM posts WHERE topic_id = ? AND id <= ?"
			err = db.QueryRow(query, topicID, postID).Scan(&position)
			if err == nil {
				page = int(math.Ceil(float64(position) / float64(postsPerPage)))
			}
		}
	}

	if page < 1 {
		page = 1
	}

	offset := (page - 1) * postsPerPage

	// 1. Get custom field columns from the config table
	var configJSON string
	err = db.QueryRow("SELECT config FROM custom_field_config WHERE entity_type = 'character_profile'").Scan(&configJSON)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get character profile config: " + err.Error()})
		c.Abort()
		return
	}

	var customConfig []Entities.CustomFieldConfig
	if err := json.Unmarshal([]byte(configJSON), &customConfig); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to parse character profile config: " + err.Error()})
		c.Abort()
		return
	}

	currencyActive := Services.IsFeatureEnabled("currency", c)

	var flattenedCols []string
	for _, field := range customConfig {
		flattenedCols = append(flattenedCols, "cpf."+field.MachineFieldName)
	}

	colsSelect := ""
	if len(flattenedCols) > 0 {
		colsSelect = ", " + strings.Join(flattenedCols, ", ")
	}

	currencyJoin := ""
	if currencyActive {
		colsSelect += ", COALESCE(cua.amount, 0) as currency_amount"
		currencyJoin = "LEFT JOIN currency_user_account cua ON p.author_user_id = cua.user_id"
	}

	// 2. Check if this topic pins the first post on every page
	var isStickyFirstPost bool
	_ = db.QueryRow("SELECT COALESCE(is_sticky_first_post, false) FROM topics WHERE id = ?", topicID).Scan(&isStickyFirstPost)

	// 3. Construct the main query
	baseQuery := fmt.Sprintf(`
		SELECT
			p.id, p.author_user_id, p.date_created, p.content, p.use_character_profile,
			u.username, u.avatar, u.total_posts, u.total_general_posts, p.guest_name,
			cp.id as character_profile_id, cp.character_id, cb.name as character_name, cp.avatar as character_avatar, cp.mask_name, cp.is_mask,
			t.subforum_id, t.type as topic_type
			%s
		FROM posts p
		JOIN topics t ON p.topic_id = t.id
		LEFT JOIN users u ON p.author_user_id = u.id
		LEFT JOIN character_profile_base cp ON p.character_profile_id = cp.id
		LEFT JOIN character_base cb ON cp.character_id = cb.id
		LEFT JOIN character_profile_flattened cpf ON cp.id = cpf.entity_id
		%s
		WHERE p.topic_id = ? AND (p.is_deleted IS NULL OR p.is_deleted <> 1)
	`, colsSelect, currencyJoin)

	var query string
	var queryArgs []interface{}
	if isStickyFirstPost && page > 1 {
		query = fmt.Sprintf(
			`(%s ORDER BY p.date_created ASC LIMIT 1) UNION ALL (%s ORDER BY p.date_created ASC LIMIT ? OFFSET ?) ORDER BY date_created ASC`,
			baseQuery, baseQuery,
		)
		queryArgs = []interface{}{topicID, topicID, postsPerPage, offset}
	} else {
		query = fmt.Sprintf(`%s ORDER BY p.date_created ASC LIMIT ? OFFSET ?`, baseQuery)
		queryArgs = []interface{}{topicID, postsPerPage, offset}
	}

	rows, err := db.Query(query, queryArgs...)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get posts: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	// 3. Scan and process results
	cols, _ := rows.Columns()
	posts := make([]Entities.Post, 0) // Initialize slice

	currentUserID := Services.GetUserIdFromContext(c)
	userTimezone := Services.GetUserTimezone(currentUserID, db)
	var subforumID int

	// Find date_created column index to scan it directly into time.Time
	dateCreatedIdx := -1
	for i, col := range cols {
		if col == "date_created" {
			dateCreatedIdx = i
			break
		}
	}

	for rows.Next() {
		// Scan into a map, using *time.Time for date_created
		values := make([]interface{}, len(cols))
		var dateCreated time.Time
		for i := range values {
			if i == dateCreatedIdx {
				values[i] = &dateCreated
			} else {
				values[i] = new(sql.RawBytes)
			}
		}
		if err := rows.Scan(values...); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan post data: " + err.Error()})
			c.Abort()
			return
		}

		rowMap := make(map[string]interface{})
		for i, colName := range cols {
			if i == dateCreatedIdx {
				continue
			}
			val := *(values[i].(*sql.RawBytes))
			if val != nil {
				rowMap[colName] = string(val) // Store as string for now
			}
		}

		// Populate Post struct from map
		var post Entities.Post
		post.Id, _ = strconv.Atoi(rowMap["id"].(string))
		post.AuthorUserId, _ = strconv.Atoi(rowMap["author_user_id"].(string))
		if username, ok := rowMap["username"]; ok {
			post.AuthorUserName = username.(string)
		}
		post.DateCreated = dateCreated
		post.DateCreatedLocalized = Services.LocalizeTime(post.DateCreated, userTimezone)
		post.Content = rowMap["content"].(string)
		post.ContentHtml = Services.ParseBBCode(post.Content)
		post.UseCharacterProfile, _ = strconv.ParseBool(rowMap["use_character_profile"].(string))
		subforumID, _ = strconv.Atoi(rowMap["subforum_id"].(string))
		topicTypeInt, _ := strconv.Atoi(rowMap["topic_type"].(string))
		if Entities.TopicType(topicTypeInt) != Entities.EpisodeTopic {
			post.ContentHtml = mentionRegexp.ReplaceAllString(post.ContentHtml, `<span class="mention">$0</span>`)
		}

		// Check CanEdit
		canEdit := false
		if currentUserID != 0 {
			if currentUserID == post.AuthorUserId {
				// Check for "Edit own post" permission in this subforum
				permission := fmt.Sprintf("subforum_edit_own_post:%d", subforumID)
				if hasPerm, err := Services.HasPermission(currentUserID, permission, db); err == nil && hasPerm {
					canEdit = true
				}
			} else {
				// Check for "Edit others' post" permission in this subforum
				permission := fmt.Sprintf("subforum_edit_others_post:%d", subforumID)
				if hasPerm, err := Services.HasPermission(currentUserID, permission, db); err == nil && hasPerm {
					canEdit = true
				}
			}
		}
		post.CanEdit = &canEdit

		if post.UseCharacterProfile {
			var charProfile Entities.CharacterProfile
			if id, ok := rowMap["character_profile_id"]; ok {
				charProfile.Id, _ = strconv.Atoi(id.(string))
			}
			if id, ok := rowMap["character_id"]; ok {
				val, _ := strconv.Atoi(id.(string))
				charProfile.CharacterId = &val
			}
			if name, ok := rowMap["character_name"]; ok {
				charProfile.CharacterName = name.(string)
			}
			if avatar, ok := rowMap["character_avatar"]; ok {
				avatarStr := avatar.(string)
				charProfile.Avatar = &avatarStr
			}
			if maskName, ok := rowMap["mask_name"]; ok {
				maskNameStr := maskName.(string)
				charProfile.MaskName = &maskNameStr
			}
			if isMask, ok := rowMap["is_mask"]; ok {
				isMaskBool, _ := strconv.ParseBool(isMask.(string))
				charProfile.IsMask = &isMaskBool
			}

			// Populate custom fields
			customFields := make(map[string]Entities.CustomFieldValue)
			for _, field := range customConfig {
				if val, ok := rowMap[field.MachineFieldName]; ok {
					cfValue := Entities.CustomFieldValue{Content: val}
					if field.FieldType == "text" {
						if s, ok := val.(string); ok {
							cfValue.ContentHtml = Services.ParseBBCode(s)
						}
					}
					customFields[field.MachineFieldName] = cfValue
				}
			}
			charProfile.CustomFields.CustomFields = customFields
			charProfile.CustomFields.FieldConfig = customConfig // Add this line
			post.CharacterProfile = &charProfile
		} else {
			var userProfile Entities.UserProfile
			userProfile.UserId = post.AuthorUserId
			if guestName, ok := rowMap["guest_name"]; ok && guestName.(string) != "" {
				userProfile.UserName = guestName.(string)
			} else if username, ok := rowMap["username"]; ok {
				userProfile.UserName = username.(string)
			}
			if avatar, ok := rowMap["avatar"]; ok {
				userProfile.Avatar = avatar.(string)
			}
			if v, ok := rowMap["total_posts"]; ok {
				userProfile.TotalPosts, _ = strconv.Atoi(v.(string))
			}
			if v, ok := rowMap["total_general_posts"]; ok {
				userProfile.TotalGeneralPosts, _ = strconv.Atoi(v.(string))
			}
			if v, ok := rowMap["currency_amount"]; ok {
				amount, _ := strconv.Atoi(v.(string))
				userProfile.CurrencyAmount = &amount
			}
			post.UserProfile = &userProfile
		}
		posts = append(posts, post)
	}

	if len(posts) > 0 {
		postIDs := make([]interface{}, len(posts))
		placeholders := make([]string, len(posts))
		for i, p := range posts {
			postIDs[i] = p.Id
			placeholders[i] = "?"
		}
		reactionQuery := fmt.Sprintf(`
			SELECT pr.post_id, pr.reaction_id, r.url, pr.user_id, u.username
			FROM post_reaction pr
			JOIN reactions r ON r.id = pr.reaction_id
			JOIN users u ON u.id = pr.user_id
			WHERE pr.post_id IN (%s) AND r.is_active = true
			ORDER BY pr.post_id, pr.reaction_id
		`, strings.Join(placeholders, ","))

		reactionRows, err := db.Query(reactionQuery, postIDs...)
		if err == nil {
			defer reactionRows.Close()

			// postIndex maps post ID to its index in posts slice
			postIndex := make(map[int]int, len(posts))
			for i, p := range posts {
				postIndex[p.Id] = i
				posts[i].Reactions = []Entities.PostReaction{}
			}
			// reactionIndex maps (postId, reactionId) to index in that post's Reactions slice
			type reactionKey struct{ postId, reactionId int }
			reactionIndex := make(map[reactionKey]int)

			for reactionRows.Next() {
				var postId, reactionId, userId int
				var url, username string
				if err := reactionRows.Scan(&postId, &reactionId, &url, &userId, &username); err != nil {
					continue
				}
				pi := postIndex[postId]
				key := reactionKey{postId, reactionId}
				if _, exists := reactionIndex[key]; !exists {
					reactionIndex[key] = len(posts[pi].Reactions)
					posts[pi].Reactions = append(posts[pi].Reactions, Entities.PostReaction{
						ReactionId: reactionId,
						Url:        url,
						Users:      []Entities.PostReactionUser{},
					})
				}
				ri := reactionIndex[key]
				posts[pi].Reactions[ri].Users = append(posts[pi].Reactions[ri].Users, Entities.PostReactionUser{Id: userId, Name: username})
				posts[pi].Reactions[ri].Number++
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"page":  page,
		"posts": posts,
	})
}

func GetTopic(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid ID"})
		c.Abort()
		return
	}

	var topic Entities.Topic
	query := "SELECT t.id, t.status, t.name, t.type, t.date_created, t.date_last_post, t.post_number, t.author_user_id, u.username, t.last_post_author_user_id, u2.username, t.subforum_id, COALESCE(t.is_sticky_first_post, false) FROM topics t JOIN users u ON t.author_user_id = u.id LEFT JOIN users u2 ON t.last_post_author_user_id = u2.id WHERE t.id = ?"
	err = db.QueryRow(query, id).Scan(
		&topic.Id,
		&topic.Status,
		&topic.Name,
		&topic.Type,
		&topic.DateCreated,
		&topic.DateLastPost,
		&topic.PostNumber,
		&topic.AuthorUserId,
		&topic.AuthorUsername,
		&topic.LastPostAuthorUserId,
		&topic.LastPostAuthorName,
		&topic.SubforumId,
		&topic.IsStickyFirstPost,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Topic not found"})
		} else {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get topic: " + err.Error()})
		}
		c.Abort()
		return
	}

	// Check CanEdit
	currentUserID := Services.GetUserIdFromContext(c)
	topic.DateLastPostLocalized = Services.LocalizeTime(topic.DateLastPost, Services.GetUserTimezone(currentUserID, db))

	// Get all permissions for this user in this subforum context
	userPerms, err := Services.GetSubforumPermissions(currentUserID, topic.SubforumId, db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get subforum permissions: " + err.Error()})
		c.Abort()
		return
	}

	// Populate the topic permissions for the frontend
	topic.Permissions = userPerms

	canEdit := false
	isAuthor := currentUserID != 0 && currentUserID == topic.AuthorUserId

	// Directly check the boolean fields of the SubforumPermissions struct
	if userPerms != nil {
		if (isAuthor && userPerms.SubforumEditOwnTopic) || userPerms.SubforumEditOthersTopic {
			canEdit = true
		}
	}

	topic.CanEdit = &canEdit

	if topic.Type == Entities.EpisodeTopic {
		var episodeID int
		err := db.QueryRow("SELECT id FROM episode_base WHERE topic_id = ?", topic.Id).Scan(&episodeID)
		if err != nil {
			if err != sql.ErrNoRows {
				_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get episode ID for topic: " + err.Error()})
				c.Abort()
			}
			c.JSON(http.StatusOK, topic)
			return
		}

		entity, err := Services.GetEntity(int64(episodeID), "episode", db)
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get episode entity: " + err.Error()})
			c.Abort()
			return
		}

		if episode, ok := entity.(*Entities.Episode); ok {
			// Fetch characters for the episode
			charRows, err := db.Query("SELECT cb.id, cb.name FROM character_base cb JOIN episode_character ec ON cb.id = ec.character_id WHERE ec.episode_id = ?", episode.Id)
			if err == nil {
				var characters []*Entities.ShortCharacter
				for charRows.Next() {
					var char Entities.ShortCharacter
					if err := charRows.Scan(&char.Id, &char.Name); err == nil {
						characters = append(characters, &char)
					}
				}
				episode.Characters = characters
				charRows.Close()
			}

			// Fetch masks for the episode
			maskRows, err := db.Query(`SELECT cpb.id, cpb.mask_name, cpb.user_id, u.username FROM character_profile_base cpb JOIN episode_mask em ON cpb.id = em.mask_id JOIN users u ON cpb.user_id = u.id WHERE em.episode_id = ?`, episode.Id)
			if err == nil {
				var masks []Entities.ShortMask
				for maskRows.Next() {
					var mask Entities.ShortMask
					if err := maskRows.Scan(&mask.Id, &mask.MaskName, &mask.UserId, &mask.UserName); err == nil {
						masks = append(masks, mask)
					}
				}
				episode.Masks = masks
				maskRows.Close()
			}

			topic.Episode = episode
		}
	}

	if topic.Type == Entities.CharacterSheetTopic {
		var characterID int
		err := db.QueryRow("SELECT id FROM character_base WHERE topic_id = ?", topic.Id).Scan(&characterID)
		if err != nil {
			if err != sql.ErrNoRows {
				// This is not an error, it just means the topic is not linked to a character sheet yet
				// _ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get character sheet ID for topic: " + err.Error()})
				// c.Abort()
			}
		} else {
			entity, err := Services.GetEntity(int64(characterID), "character", db)
			if err != nil {
				_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get character entity: " + err.Error()})
				c.Abort()
				return
			}
			if character, ok := entity.(*Entities.Character); ok {
				character.Factions, _ = Services.GetFactionTreeByCharacter(characterID, db)

				var cr Entities.ClaimRecord
				err := db.QueryRow(`
					SELECT id, claim_id, user_id, guest_hash, is_guest, claim_date, claim_expiration_date, character_id, claim_created_with_character_sheet
					FROM claim_record
					WHERE character_id = ?
					ORDER BY claim_date DESC
					LIMIT 1
				`, characterID).Scan(&cr.Id, &cr.ClaimId, &cr.UserId, &cr.GuestHash, &cr.IsGuest, &cr.ClaimDate, &cr.ClaimExpirationDate, &cr.CharacterId, &cr.ClaimCreatedWithCharacterSheet)
				if err == nil {
					character.ClaimRecord = &cr
				}

				topic.Character = character
			}
		}
	}

	if topic.Type == Entities.WantedCharacterTopic {
		var wantedCharacterID int
		err := db.QueryRow("SELECT id FROM wanted_character_base WHERE topic_id = ?", topic.Id).Scan(&wantedCharacterID)
		if err == nil {
			entity, err := Services.GetEntity(int64(wantedCharacterID), "wanted_character", db)
			if err != nil {
				_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get wanted character entity: " + err.Error()})
				c.Abort()
				return
			}
			if wc, ok := entity.(*Entities.WantedCharacter); ok {
				if wc.CharacterClaimId != nil {
					wc.Factions, _ = Services.GetFactionTreeByCharacterClaim(*wc.CharacterClaimId, db)
					wc.ClaimRecord = fetchActiveClaimRecord(*wc.CharacterClaimId, db)
				}
				topic.WantedCharacter = wc
			}
		}
	}

	c.JSON(http.StatusOK, topic)
}

func CreatePost(c *gin.Context, db *sql.DB) {
	var req CreatePostRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	userID := Services.GetUserIdFromContext(c)

	tx, err := db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
		return
	}
	defer tx.Rollback()

	// Check topic status — reject if full or inactive
	var topicStatus Entities.TopicStatus
	if err := tx.QueryRow("SELECT status FROM topics WHERE id = ?", req.TopicID).Scan(&topicStatus); err != nil {
		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "Topic not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check topic status"})
		}
		return
	}
	if topicStatus == Entities.FullTopic {
		c.JSON(http.StatusForbidden, gin.H{"error": "This topic has reached its post limit and is no longer accepting posts"})
		return
	}

	var guestName *string
	if userID == 0 {
		guestName = req.GuestName
	}

	// For episode topics, check character membership unless open_to_everyone
	if req.UseCharacterProfile && req.CharacterProfileID != nil {
		var topicType int
		if err := tx.QueryRow("SELECT type FROM topics WHERE id = ?", req.TopicID).Scan(&topicType); err == nil &&
			topicType == int(Entities.EpisodeTopic) {

			var openToEveryone bool
			err := tx.QueryRow(`
				SELECT eb.open_to_everyone
				FROM episode_base eb
				JOIN topics t ON t.id = eb.topic_id
				WHERE t.id = ?
			`, req.TopicID).Scan(&openToEveryone)

			if err == nil && !openToEveryone {
				var characterID *int
				var isMask sql.NullBool
				if err := tx.QueryRow(
					"SELECT character_id, is_mask FROM character_profile_base WHERE id = ?",
					*req.CharacterProfileID,
				).Scan(&characterID, &isMask); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to resolve character profile"})
					return
				}

				var episodeID int
				if err := tx.QueryRow(
					"SELECT id FROM episode_base WHERE topic_id = ?",
					req.TopicID,
				).Scan(&episodeID); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to resolve episode"})
					return
				}

				var memberCount int
				if isMask.Valid && isMask.Bool {
					// Masks are checked against episode_mask
					_ = tx.QueryRow(
						"SELECT COUNT(*) FROM episode_mask WHERE episode_id = ? AND mask_id = ?",
						episodeID, *req.CharacterProfileID,
					).Scan(&memberCount)
				} else if characterID != nil {
					// Regular character profiles are checked against episode_character
					_ = tx.QueryRow(
						"SELECT COUNT(*) FROM episode_character WHERE episode_id = ? AND character_id = ?",
						episodeID, *characterID,
					).Scan(&memberCount)
				}

				if memberCount == 0 {
					c.JSON(http.StatusForbidden, gin.H{"error": "This character is not a participant in this episode"})
					return
				}
			}
		}
	}

	// Insert Post
	res, err := tx.Exec("INSERT INTO posts (topic_id, author_user_id, content, date_created, use_character_profile, character_profile_id, guest_name) VALUES (?, ?, ?, NOW(), ?, ?, ?)",
		req.TopicID, userID, req.Content, req.UseCharacterProfile, req.CharacterProfileID, guestName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert post: " + err.Error()})
		return
	}
	postID, err := res.LastInsertId()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get post ID"})
		return
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
		return
	}

	// Fetch topic name for notifications
	var topicNameForNotif string
	_ = db.QueryRow("SELECT name FROM topics WHERE id = ?", req.TopicID).Scan(&topicNameForNotif)

	// Handle Mentions — format is @<username>\u200A
	re := regexp.MustCompile(`@([^\x{200A}]+)\x{200A}`)
	matches := re.FindAllStringSubmatch(req.Content, -1)

	if len(matches) > 0 {
		seen := make(map[string]bool)
		var usernames []string
		for _, match := range matches {
			if len(match) > 1 {
				username := match[1]
				if !seen[username] {
					usernames = append(usernames, username)
					seen[username] = true
				}
			}
		}

		if len(usernames) > 0 {
			query := "SELECT id, username FROM users WHERE username IN (?" + strings.Repeat(",?", len(usernames)-1) + ")"
			args := make([]interface{}, len(usernames))
			for i, u := range usernames {
				args[i] = u
			}

			rows, err := db.Query(query, args...)
			if err == nil {
				defer rows.Close()
				for rows.Next() {
					var mUserID int
					var mUsername string
					if err := rows.Scan(&mUserID, &mUsername); err == nil {
						if mUserID != userID {
							// Fetch author details for the mention
							var authorName string
							var authorCharacterID *int
							var authorCharacterName *string

							err = db.QueryRow("SELECT username FROM users WHERE id = ?", userID).Scan(&authorName)
							if req.UseCharacterProfile && req.CharacterProfileID != nil {
								var charID int
								var charName string
								err = db.QueryRow("SELECT character_id, cb.name FROM character_profile_base cpb JOIN character_base cb ON cpb.character_id = cb.id WHERE cpb.id = ?", *req.CharacterProfileID).Scan(&charID, &charName)
								if err == nil {
									authorCharacterID = &charID
									authorCharacterName = &charName
								}
							}

							mentionData := Entities.NotificationMention{
								UserId:        userID,
								UserName:      authorName,
								CharacterId:   authorCharacterID,
								CharacterName: authorCharacterName,
								PostId:        int(postID),
								TopicId:       req.TopicID,
								TopicName:     topicNameForNotif,
							}

							Events.Publish(db, Events.NotificationCreated, Events.NotificationEvent{
								UserID:  mUserID,
								Type:    "mention",
								Message: fmt.Sprintf("%s mentioned you in %s", authorName, topicNameForNotif),
								Data:    mentionData,
							})
						}
					}
				}
			}
		}
	}

	// Fetch additional data for the event
	var subforumID int
	var topicName string
	err = db.QueryRow("SELECT subforum_id, name FROM topics WHERE id = ?", req.TopicID).Scan(&subforumID, &topicName)
	if err == nil {
		// Fetch full post data
		fullPost, postErr := Services.GetPostById(int(postID), db, Services.IsFeatureEnabled("currency", c))
		if postErr != nil {
			fmt.Printf("Error getting post details for event publishing: %v\n", postErr)
		} else {
			// Publish event to broadcast to all users
			Events.Publish(db, Events.PostCreated, Events.PostCreatedEvent{
				Type:       "post_created",
				TopicID:    int64(req.TopicID),
				SubforumID: subforumID,
				Post:       *fullPost,
			})
		}
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Post created successfully", "post_id": postID})
}

func PreviewPost(c *gin.Context, db *sql.DB) {
	var req CreatePostRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	userID := Services.GetUserIdFromContext(c)

	var post Entities.Post
	post.TopicId = req.TopicID
	post.AuthorUserId = userID
	post.Content = req.Content
	post.ContentHtml = Services.ParseBBCode(req.Content)
	post.UseCharacterProfile = req.UseCharacterProfile
	post.GuestName = req.GuestName

	var topicType int
	if err := db.QueryRow("SELECT type FROM topics WHERE id = ?", req.TopicID).Scan(&topicType); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Topic not found"})
		c.Abort()
		return
	}

	if Entities.TopicType(topicType) != Entities.EpisodeTopic {
		post.ContentHtml = mentionRegexp.ReplaceAllString(post.ContentHtml, `<span class="mention">$0</span>`)
	}

	if req.UseCharacterProfile && req.CharacterProfileID != nil {
		row := db.QueryRow(`
			SELECT cp.id, cp.character_id, cb.name, cp.avatar, cp.mask_name, cp.is_mask
			FROM character_profile_base cp
			JOIN character_base cb ON cp.character_id = cb.id
			WHERE cp.id = ?`, *req.CharacterProfileID)

		var charProfile Entities.CharacterProfile
		var isMask bool
		var maskName, avatar sql.NullString
		var charID int
		var charName string
		if err := row.Scan(&charProfile.Id, &charID, &charName, &avatar, &maskName, &isMask); err == nil {
			charProfile.CharacterId = &charID
			charProfile.CharacterName = charName
			if avatar.Valid {
				charProfile.Avatar = &avatar.String
			}
			if maskName.Valid {
				charProfile.MaskName = &maskName.String
			}
			charProfile.IsMask = &isMask
		}
		post.CharacterProfile = &charProfile
	} else {
		var userProfile Entities.UserProfile
		userProfile.UserId = userID
		if userID != 0 {
			var username, avatar string
			var totalPosts, totalGeneralPosts int
			var currencyAmount sql.NullInt64
			createPostCurrencyActive := Services.IsFeatureEnabled("currency", c)
			if createPostCurrencyActive {
				if err := db.QueryRow(`
					SELECT u.username, u.avatar, u.total_posts, u.total_general_posts, COALESCE(cua.amount, 0)
					FROM users u
					LEFT JOIN currency_user_account cua ON u.id = cua.user_id
					WHERE u.id = ?`, userID).Scan(&username, &avatar, &totalPosts, &totalGeneralPosts, &currencyAmount); err == nil {
					userProfile.UserName = username
					userProfile.Avatar = avatar
					userProfile.TotalPosts = totalPosts
					userProfile.TotalGeneralPosts = totalGeneralPosts
					post.AuthorUserName = username
					amount := int(currencyAmount.Int64)
					userProfile.CurrencyAmount = &amount
				}
			} else {
				if err := db.QueryRow("SELECT username, avatar, total_posts, total_general_posts FROM users WHERE id = ?", userID).Scan(&username, &avatar, &totalPosts, &totalGeneralPosts); err == nil {
					userProfile.UserName = username
					userProfile.Avatar = avatar
					userProfile.TotalPosts = totalPosts
					userProfile.TotalGeneralPosts = totalGeneralPosts
					post.AuthorUserName = username
				}
			}
		} else if req.GuestName != nil {
			userProfile.UserName = *req.GuestName
		}
		post.UserProfile = &userProfile
	}

	c.JSON(http.StatusOK, post)
}

func UpdatePost(c *gin.Context, db *sql.DB) {
	postID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid post ID"})
		c.Abort()
		return
	}

	var req UpdatePostRequest
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

	// 1. Fetch post details to check ownership and subforum
	var authorUserID int
	var subforumID int
	query := `
		SELECT p.author_user_id, t.subforum_id 
		FROM posts p 
		JOIN topics t ON p.topic_id = t.id 
		WHERE p.id = ?
	`
	err = db.QueryRow(query, postID).Scan(&authorUserID, &subforumID)
	if err != nil {
		if err == sql.ErrNoRows {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Post not found"})
		} else {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch post details: " + err.Error()})
		}
		c.Abort()
		return
	}

	// 2. Check permissions
	canEdit := false
	if userID == authorUserID {
		// Check for "Edit own post" permission
		permission := fmt.Sprintf("subforum_edit_own_post:%d", subforumID)
		if hasPerm, err := Services.HasPermission(userID, permission, db); err == nil && hasPerm {
			canEdit = true
		}
	} else {
		// Check for "Edit others' post" permission
		permission := fmt.Sprintf("subforum_edit_others_post:%d", subforumID)
		if hasPerm, err := Services.HasPermission(userID, permission, db); err == nil && hasPerm {
			canEdit = true
		}
	}

	if !canEdit {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "You do not have permission to edit this post"})
		c.Abort()
		return
	}

	// 3. Update post content
	_, err = db.Exec("UPDATE posts SET content = ? WHERE id = ?", req.Content, postID)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update post: " + err.Error()})
		c.Abort()
		return
	}

	// 4. Fetch updated post and emit event
	updatedPost, err := Services.GetPostById(postID, db, Services.IsFeatureEnabled("currency", c))
	if err == nil {
		// We need topicID and subforumID for the event
		var topicID int64
		err = db.QueryRow("SELECT topic_id FROM posts WHERE id = ?", postID).Scan(&topicID)
		if err == nil {
			Events.Publish(db, Events.PostCreated, Events.PostCreatedEvent{ // Reusing PostCreatedEvent for broadcast
				Type:       "post_updated",
				TopicID:    topicID,
				SubforumID: subforumID,
				Post:       *updatedPost,
			})
		}
	}

	c.JSON(http.StatusOK, updatedPost)
}

func UpdateTopic(c *gin.Context, db *sql.DB) {
	topicID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid topic ID"})
		c.Abort()
		return
	}

	var req UpdateTopicRequest
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

	// 1. Fetch topic details to check ownership and subforum
	var authorUserID int
	var subforumID int
	var topicType Entities.TopicType
	query := "SELECT author_user_id, subforum_id, type FROM topics WHERE id = ?"
	err = db.QueryRow(query, topicID).Scan(&authorUserID, &subforumID, &topicType)
	if err != nil {
		if err == sql.ErrNoRows {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Topic not found"})
		} else {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch topic details: " + err.Error()})
		}
		c.Abort()
		return
	}

	// Verify topic is not full — this status is system-managed and cannot be overridden
	var currentStatus Entities.TopicStatus
	if err := db.QueryRow("SELECT status FROM topics WHERE id = ?", topicID).Scan(&currentStatus); err == nil {
		if currentStatus == Entities.FullTopic {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "Cannot update a full topic"})
			c.Abort()
			return
		}
	}

	// Verify topic type is general
	if topicType != Entities.GeneralTopic {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Only general topics can be updated via this endpoint"})
		c.Abort()
		return
	}

	// 2. Check permissions
	canEdit := false
	if userID == authorUserID {
		// Check for "Edit own topic" permission
		permission := fmt.Sprintf("subforum_edit_own_topic:%d", subforumID)
		if hasPerm, err := Services.HasPermission(userID, permission, db); err == nil && hasPerm {
			canEdit = true
		}
	} else {
		// Check for "Edit others' topic" permission
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

	// 3. Validate requested status — FullTopic is system-managed and cannot be set manually
	if req.Status != nil && *req.Status == Entities.FullTopic {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "FullTopic status cannot be set manually"})
		c.Abort()
		return
	}

	// 4. Build and execute update
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
	_, err = db.Exec("UPDATE topics SET "+strings.Join(setClauses, ", ")+" WHERE id = ?", args...)
	if err != nil {
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

func BulkUpdateTopics(c *gin.Context, db *sql.DB) {
	var req struct {
		TopicIDs []int                 `json:"topic_ids" binding:"required"`
		Status   *Entities.TopicStatus `json:"status"`
		IsSticky *bool                 `json:"is_sticky"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}
	if len(req.TopicIDs) == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "topic_ids must not be empty"})
		c.Abort()
		return
	}
	if req.Status == nil && req.IsSticky == nil {
		c.JSON(http.StatusOK, gin.H{"updated": 0})
		return
	}
	if req.Status != nil && *req.Status == Entities.FullTopic {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "FullTopic status cannot be set manually"})
		c.Abort()
		return
	}

	userID := Services.GetUserIdFromContext(c)

	placeholders := strings.Repeat("?,", len(req.TopicIDs)-1) + "?"
	idArgs := make([]interface{}, len(req.TopicIDs))
	for i, id := range req.TopicIDs {
		idArgs[i] = id
	}

	// Fetch subforum IDs and validate no topic is Full
	rows, err := db.Query(
		fmt.Sprintf("SELECT id, subforum_id, status FROM topics WHERE id IN (%s)", placeholders),
		idArgs...,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch topics: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	subforumSeen := make(map[int]bool)
	for rows.Next() {
		var tID, subforumID int
		var status Entities.TopicStatus
		if err := rows.Scan(&tID, &subforumID, &status); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan topic: " + err.Error()})
			c.Abort()
			return
		}
		if status == Entities.FullTopic {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: fmt.Sprintf("Topic %d is full and cannot be updated", tID)})
			c.Abort()
			return
		}
		subforumSeen[subforumID] = true
	}

	// Check edit-others permission for every subforum involved
	for subforumID := range subforumSeen {
		perm := fmt.Sprintf("subforum_edit_others_topic:%d", subforumID)
		if ok, err := Services.HasPermission(userID, perm, db); err != nil || !ok {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: fmt.Sprintf("No permission to edit topics in subforum %d", subforumID)})
			c.Abort()
			return
		}
	}

	var setClauses []string
	var setArgs []interface{}
	if req.Status != nil {
		setClauses = append(setClauses, "status = ?")
		setArgs = append(setArgs, *req.Status)
	}
	if req.IsSticky != nil {
		setClauses = append(setClauses, "is_sticky = ?")
		setArgs = append(setArgs, *req.IsSticky)
	}

	args := append(setArgs, idArgs...)
	result, err := db.Exec(
		fmt.Sprintf("UPDATE topics SET %s WHERE id IN (%s)", strings.Join(setClauses, ", "), placeholders),
		args...,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update topics: " + err.Error()})
		c.Abort()
		return
	}

	updated, _ := result.RowsAffected()
	c.JSON(http.StatusOK, gin.H{"updated": updated})
}

func GetActiveTopics(c *gin.Context, db *sql.DB) {
	pageStr := c.Query("page")
	notViewedStr := c.Query("not_viewed")
	subforumIDsStr := c.Query("subforum_ids")

	page := 1
	if pageStr != "" {
		page, _ = strconv.Atoi(pageStr)
	}
	if page < 1 {
		page = 1
	}

	notViewed := false
	if notViewedStr == "true" {
		notViewed = true
	}

	var subforumIDs []int
	if subforumIDsStr != "" {
		for _, idStr := range strings.Split(subforumIDsStr, ",") {
			if id, err := strconv.Atoi(idStr); err == nil {
				subforumIDs = append(subforumIDs, id)
			}
		}
	}

	userID := Services.GetUserIdFromContext(c)

	// Get visible subforum IDs based on user permissions
	visibleSubforumIDs, err := Services.GetVisibleSubforums(userID, "subforum_read", db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to determine visible subforums: " + err.Error()})
		c.Abort()
		return
	}

	// Filter subforumIDs based on visibility
	var filteredSubforumIDs []int
	if len(subforumIDs) > 0 {
		visibleMap := make(map[int]bool)
		for _, id := range visibleSubforumIDs {
			visibleMap[id] = true
		}
		for _, id := range subforumIDs {
			if visibleMap[id] {
				filteredSubforumIDs = append(filteredSubforumIDs, id)
			}
		}
	} else {
		filteredSubforumIDs = visibleSubforumIDs
	}

	if len(filteredSubforumIDs) == 0 {
		c.JSON(http.StatusOK, []ViewforumRow{})
		return
	}

	limit := Services.GetPostsPerPage(db)
	offset := (page - 1) * limit

	placeholders := strings.Repeat("?,", len(filteredSubforumIDs)-1) + "?"
	query := fmt.Sprintf(`
		SELECT t.id, t.status, t.name, t.type, t.date_last_post, t.post_number, 
		       t.author_user_id, u.username as author_username,
		       t.last_post_author_user_id, u2.username as last_post_author_username, 
		       COALESCE(t.last_post_id, (SELECT MAX(id) FROM posts WHERE topic_id = t.id)) as last_post_id,
		       (CASE WHEN ? != 0 AND (utv.post_id IS NULL OR utv.post_id < COALESCE(t.last_post_id, (SELECT MAX(id) FROM posts WHERE topic_id = t.id))) THEN 1 ELSE 0 END) as not_viewed,
		       utv.post_id as last_viewed_id
		FROM topics t
		JOIN users u ON t.author_user_id = u.id
		LEFT JOIN users u2 ON t.last_post_author_user_id = u2.id
		LEFT JOIN user_topic_view utv ON t.id = utv.topic_id AND utv.user_id = ?
		WHERE t.subforum_id IN (%s)
	`, placeholders)

	var args []interface{}
	args = append(args, userID, userID)
	for _, id := range filteredSubforumIDs {
		args = append(args, id)
	}

	if notViewed && userID != 0 {
		query += " AND (utv.post_id IS NULL OR utv.post_id < COALESCE(t.last_post_id, (SELECT MAX(id) FROM posts WHERE topic_id = t.id)))"
	}

	query += " AND t.date_last_post >= DATE_SUB(NOW(), INTERVAL 10 DAY)"

	query += " ORDER BY t.date_last_post DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := db.Query(query, args...)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get active topics: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	userTimezone := Services.GetUserTimezone(userID, db)

	var topics []ViewforumRow
	for rows.Next() {
		var topic ViewforumRow
		if err := rows.Scan(
			&topic.Id,
			&topic.Status,
			&topic.Name,
			&topic.Type,
			&topic.DateLastPost,
			&topic.PostNumber,
			&topic.AuthorUserId,
			&topic.AuthorUsername,
			&topic.LastPostAuthorUserId,
			&topic.LastPostAuthorUsername,
			&topic.LastPostId,
			&topic.NotViewed,
			&topic.LastViewedId,
		); err != nil {
			continue
		}
		if topic.DateLastPost != nil {
			localized := Services.LocalizeTime(*topic.DateLastPost, userTimezone)
			topic.DateLastPostLocalized = &localized
		}
		topics = append(topics, topic)
	}

	if topics == nil {
		topics = []ViewforumRow{}
	}

	c.JSON(http.StatusOK, topics)
}

func GetActiveTopicCount(c *gin.Context, db *sql.DB) {
	notViewedStr := c.Query("not_viewed")
	subforumIDsStr := c.Query("subforum_ids")

	notViewed := false
	if notViewedStr == "true" {
		notViewed = true
	}

	var subforumIDs []int
	if subforumIDsStr != "" {
		for _, idStr := range strings.Split(subforumIDsStr, ",") {
			if id, err := strconv.Atoi(idStr); err == nil {
				subforumIDs = append(subforumIDs, id)
			}
		}
	}

	userID := Services.GetUserIdFromContext(c)

	// Get visible subforum IDs based on user permissions
	visibleSubforumIDs, err := Services.GetVisibleSubforums(userID, "subforum_read", db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to determine visible subforums: " + err.Error()})
		c.Abort()
		return
	}

	// Filter subforumIDs based on visibility
	var filteredSubforumIDs []int
	if len(subforumIDs) > 0 {
		visibleMap := make(map[int]bool)
		for _, id := range visibleSubforumIDs {
			visibleMap[id] = true
		}
		for _, id := range subforumIDs {
			if visibleMap[id] {
				filteredSubforumIDs = append(filteredSubforumIDs, id)
			}
		}
	} else {
		filteredSubforumIDs = visibleSubforumIDs
	}

	if len(filteredSubforumIDs) == 0 {
		c.JSON(http.StatusOK, gin.H{"total": 0})
		return
	}

	placeholders := strings.Repeat("?,", len(filteredSubforumIDs)-1) + "?"
	query := fmt.Sprintf(`
		SELECT COUNT(*)
		FROM topics t
		JOIN users u ON t.author_user_id = u.id
		LEFT JOIN user_topic_view utv ON t.id = utv.topic_id AND utv.user_id = ?
		WHERE t.subforum_id IN (%s)
	`, placeholders)

	var args []interface{}
	args = append(args, userID)
	for _, id := range filteredSubforumIDs {
		args = append(args, id)
	}

	if notViewed && userID != 0 {
		query += " AND (utv.post_id IS NULL OR utv.post_id < COALESCE(t.last_post_id, (SELECT MAX(id) FROM posts WHERE topic_id = t.id)))"
	}

	query += " AND t.date_last_post >= DATE_SUB(NOW(), INTERVAL 10 DAY)"

	var count int
	err = db.QueryRow(query, args...).Scan(&count)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get active topic count: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"total": count})
}

func MoveTopics(c *gin.Context, db *sql.DB) {
	var req struct {
		SubforumID int   `json:"subforum_id" binding:"required"`
		TopicIDs   []int `json:"topic_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	if len(req.TopicIDs) == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "topic_ids must not be empty"})
		c.Abort()
		return
	}

	var exists bool
	if err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM subforums WHERE id = ?)", req.SubforumID).Scan(&exists); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to verify subforum: " + err.Error()})
		c.Abort()
		return
	}
	if !exists {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Target subforum not found"})
		c.Abort()
		return
	}

	placeholders := strings.Repeat("?,", len(req.TopicIDs)-1) + "?"

	// Collect source subforum IDs before moving
	idArgs := make([]interface{}, len(req.TopicIDs))
	for i, id := range req.TopicIDs {
		idArgs[i] = id
	}
	sourceRows, err := db.Query(fmt.Sprintf("SELECT DISTINCT subforum_id FROM topics WHERE id IN (%s)", placeholders), idArgs...)
	var sourceSubforumIDs []int
	if err == nil {
		defer sourceRows.Close()
		for sourceRows.Next() {
			var sfID int
			if sourceRows.Scan(&sfID) == nil {
				sourceSubforumIDs = append(sourceSubforumIDs, sfID)
			}
		}
	}

	moveArgs := make([]interface{}, 0, len(req.TopicIDs)+1)
	moveArgs = append(moveArgs, req.SubforumID)
	moveArgs = append(moveArgs, idArgs...)

	result, err := db.Exec(fmt.Sprintf("UPDATE topics SET subforum_id = ? WHERE id IN (%s)", placeholders), moveArgs...)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to move topics: " + err.Error()})
		c.Abort()
		return
	}

	moved, _ := result.RowsAffected()
	c.JSON(http.StatusOK, gin.H{"moved": moved})

	affected := make(map[int]bool)
	affected[req.SubforumID] = true
	for _, id := range sourceSubforumIDs {
		affected[id] = true
	}
	subforumIDs := make([]int, 0, len(affected))
	for id := range affected {
		subforumIDs = append(subforumIDs, id)
	}
	Events.Publish(db, Events.TopicsMoved, Events.TopicsMovedEvent{SubforumIDs: subforumIDs})
}

func GetPostById(c *gin.Context, db *sql.DB) {
	postID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid post ID"})
		c.Abort()
		return
	}

	userID := Services.GetUserIdFromContext(c)

	var subforumID int
	err = db.QueryRow(`SELECT t.subforum_id FROM posts p JOIN topics t ON p.topic_id = t.id WHERE p.id = ?`, postID).Scan(&subforumID)
	if err == sql.ErrNoRows {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Post not found"})
		c.Abort()
		return
	}
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get post: " + err.Error()})
		c.Abort()
		return
	}

	permission := fmt.Sprintf("subforum_read:%d", subforumID)
	if hasPerm, err := Services.HasPermission(userID, permission, db); err != nil || !hasPerm {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Post not found"})
		c.Abort()
		return
	}

	post, err := Services.GetPostById(postID, db, Services.IsFeatureEnabled("currency", c))
	if err == sql.ErrNoRows {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Post not found"})
		c.Abort()
		return
	}
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get post: " + err.Error()})
		c.Abort()
		return
	}

	post.DateCreatedLocalized = Services.LocalizeTime(post.DateCreated, Services.GetUserTimezone(userID, db))

	c.JSON(http.StatusOK, post)
}
