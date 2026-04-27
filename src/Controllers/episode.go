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

	"github.com/gin-gonic/gin"
)

type CreateEpisodeRequest struct {
	SubforumID   int                    `json:"subforum_id" binding:"required"`
	Name         string                 `json:"name" binding:"required"`
	CharacterIDs []int                  `json:"character_ids"`
	MaskIds      []int                  `json:"mask_ids"`
	CustomFields map[string]interface{} `json:"custom_fields"`
}

type UpdateEpisodeRequest struct {
	Name           string                 `json:"name" binding:"required"`
	CharacterIDs   []int                  `json:"character_ids"`
	MaskIds        []int                  `json:"mask_ids"`
	CustomFields   map[string]interface{} `json:"custom_fields"`
	OpenToEveryone *bool                  `json:"open_to_everyone"`
}

type GetEpisodesRequest struct {
	SubforumIDs  []int `json:"subforum_ids"`
	CharacterIDs []int `json:"character_ids"`
	FactionIDs   []int `json:"faction_ids"`
	Page         int   `json:"page"`
}

type EpisodeListItem struct {
	Id           int    `json:"id"`
	Name         string `json:"name"`
	TopicId      int    `json:"topic_id"`
	SubforumId   int    `json:"subforum_id"`
	SubforumName string `json:"subforum_name"`
	TopicStatus  int    `json:"topic_status"`
	LastPostDate string `json:"last_post_date"`
}

func CreateEpisode(c *gin.Context, db *sql.DB) {
	var req CreateEpisodeRequest
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

	tx, err := db.Begin()
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to start transaction"})
		c.Abort()
		return
	}
	defer tx.Rollback()

	// 1. Insert Topic (without first post)
	// Note: post_number = 0.
	res, err := tx.Exec("INSERT INTO topics (subforum_id, name, author_user_id, date_created, date_last_post, status, type, post_number, last_post_author_user_id) VALUES (?, ?, ?, NOW(), NOW(), 0, ?, 0, ?)",
		req.SubforumID, req.Name, userID, Entities.EpisodeTopic, userID)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to insert topic: " + err.Error()})
		c.Abort()
		return
	}
	topicID, err := res.LastInsertId()
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get topic ID"})
		c.Abort()
		return
	}

	// 2. Create Episode Entity using Service
	// Convert map[string]interface{} to map[string]Entities.CustomFieldValue
	cfMap := make(map[string]Entities.CustomFieldValue)
	for k, v := range req.CustomFields {
		cfMap[k] = Entities.CustomFieldValue{Content: v}
	}

	episode := Entities.Episode{
		Topic_Id: int(topicID),
		Name:     req.Name,
		CustomFields: Entities.CustomFieldEntity{
			CustomFields: cfMap,
		},
	}

	createdEntity, episodeID, err := Services.CreateEntity("episode", &episode, tx)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create episode entity: " + err.Error()})
		c.Abort()
		return
	}

	createdEpisode, ok := createdEntity.(*Entities.Episode)
	if !ok {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to cast created entity"})
		c.Abort()
		return
	}

	// 3. Insert Episode-Character Relations
	if len(req.CharacterIDs) > 0 {
		stmt, err := tx.Prepare("INSERT INTO episode_character (episode_id, character_id) VALUES (?, ?)")
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to prepare character relation statement"})
			c.Abort()
			return
		}
		defer stmt.Close()

		for _, charID := range req.CharacterIDs {
			_, err := stmt.Exec(createdEpisode.Id, charID)
			if err != nil {
				_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to insert character relation: " + err.Error()})
				c.Abort()
				return
			}
		}
	}

	// 4. Insert Episode-Mask Relations
	if len(req.MaskIds) > 0 {
		maskStmt, err := tx.Prepare("INSERT INTO episode_mask (episode_id, mask_id) VALUES (?, ?)")
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to prepare mask relation statement"})
			c.Abort()
			return
		}
		defer maskStmt.Close()

		for _, maskID := range req.MaskIds {
			_, err := maskStmt.Exec(createdEpisode.Id, maskID)
			if err != nil {
				_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to insert mask relation: " + err.Error()})
				c.Abort()
				return
			}
		}
	}

	if err := tx.Commit(); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to commit transaction"})
		c.Abort()
		return
	}

	// Emit EpisodeCreated event
	Events.Publish(db, Events.EpisodeCreated, Events.EpisodeCreatedEvent{
		EpisodeID:  episodeID,
		SubforumID: req.SubforumID,
		TopicID:    topicID,
		TopicName:  req.Name,
	})

	c.JSON(http.StatusCreated, gin.H{"message": "Episode created successfully", "episode_id": createdEpisode.Id, "topic_id": topicID})
}

func PreviewEpisode(c *gin.Context, db *sql.DB) {
	var req CreateEpisodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	cfMap := make(map[string]Entities.CustomFieldValue)
	for k, v := range req.CustomFields {
		cfMap[k] = Entities.CustomFieldValue{Content: v}
	}

	fieldConfig, _ := Services.GetFieldConfig("episode", db)
	for _, conf := range fieldConfig {
		if conf.FieldType == "text" {
			if val, ok := cfMap[conf.MachineFieldName]; ok {
				if s, ok := val.Content.(string); ok {
					val.ContentHtml = Services.ParseBBCode(s)
					cfMap[conf.MachineFieldName] = val
				}
			}
		}
	}

	episode := Entities.Episode{
		Name: req.Name,
		CustomFields: Entities.CustomFieldEntity{
			CustomFields: cfMap,
			FieldConfig:  fieldConfig,
		},
	}

	c.JSON(http.StatusOK, episode)
}

func GetEpisodes(c *gin.Context, db *sql.DB) {
	var req GetEpisodesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	userID := Services.GetUserIdFromContext(c)

	visibleSubforumIDs, err := Services.GetVisibleSubforums(userID, "subforum_read", db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to determine visible subforums: " + err.Error()})
		c.Abort()
		return
	}

	// If the request specifies subforums, intersect with visible ones; otherwise use all visible.
	allowedSubforumIDs := visibleSubforumIDs
	if len(req.SubforumIDs) > 0 {
		visibleMap := make(map[int]bool, len(visibleSubforumIDs))
		for _, id := range visibleSubforumIDs {
			visibleMap[id] = true
		}
		allowedSubforumIDs = allowedSubforumIDs[:0]
		for _, id := range req.SubforumIDs {
			if visibleMap[id] {
				allowedSubforumIDs = append(allowedSubforumIDs, id)
			}
		}
	}

	if len(allowedSubforumIDs) == 0 {
		c.JSON(http.StatusOK, []EpisodeListItem{})
		return
	}

	query := `SELECT e.id, e.name, e.topic_id, t.subforum_id, s.name, t.status, t.date_last_post
		FROM episode_base e
		JOIN topics t ON e.topic_id = t.id
		JOIN subforums s ON t.subforum_id = s.id
		WHERE 1=1`
	var args []interface{}

	placeholders := make([]string, len(allowedSubforumIDs))
	for i, id := range allowedSubforumIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	query += " AND t.subforum_id IN (" + strings.Join(placeholders, ",") + ")"

	if len(req.CharacterIDs) > 0 {
		placeholders := make([]string, len(req.CharacterIDs))
		for i, id := range req.CharacterIDs {
			placeholders[i] = "?"
			args = append(args, id)
		}
		query += " AND EXISTS (SELECT 1 FROM episode_character ec WHERE ec.episode_id = e.id AND ec.character_id IN (" + strings.Join(placeholders, ",") + "))"
	}

	if len(req.FactionIDs) > 0 {
		placeholders := make([]string, len(req.FactionIDs))
		for i, id := range req.FactionIDs {
			placeholders[i] = "?"
			args = append(args, id)
		}
		query += " AND EXISTS (SELECT 1 FROM episode_character ec JOIN character_faction cf ON ec.character_id = cf.character_id WHERE ec.episode_id = e.id AND cf.faction_id IN (" + strings.Join(placeholders, ",") + "))"
	}

	limit := 20
	page := req.Page
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * limit

	query += " ORDER BY t.date_last_post DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := db.Query(query, args...)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get episodes: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	var episodes []EpisodeListItem = []EpisodeListItem{}
	for rows.Next() {
		var ep EpisodeListItem
		if err := rows.Scan(&ep.Id, &ep.Name, &ep.TopicId, &ep.SubforumId, &ep.SubforumName, &ep.TopicStatus, &ep.LastPostDate); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan episode: " + err.Error()})
			c.Abort()
			return
		}
		episodes = append(episodes, ep)
	}

	c.JSON(http.StatusOK, episodes)
}

func GetEpisode(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid ID"})
		c.Abort()
		return
	}

	entity, err := Services.GetEntity(int64(id), "episode", db)
	if err != nil {
		if err == sql.ErrNoRows {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Episode not found"})
		} else {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get episode: " + err.Error()})
		}
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

		// Check CanEdit
		currentUserID := Services.GetUserIdFromContext(c)
		canEdit := false
		if currentUserID != 0 {
			// Fetch subforum ID for permission check
			var subforumID int
			err = db.QueryRow("SELECT subforum_id FROM topics WHERE id = ?", episode.Topic_Id).Scan(&subforumID)
			if err == nil {
				// Check for "Edit others' topic" permission
				permission := fmt.Sprintf("subforum_edit_others_topic:%d", subforumID)
				if hasPerm, err := Services.HasPermission(currentUserID, permission, db); err == nil && hasPerm {
					canEdit = true
				} else {
					// Check if user is the author of the topic
					var authorUserID int
					err = db.QueryRow("SELECT author_user_id FROM topics WHERE id = ?", episode.Topic_Id).Scan(&authorUserID)
					if err == nil && currentUserID == authorUserID {
						// Check for "Edit own topic" permission
						permission = fmt.Sprintf("subforum_edit_own_topic:%d", subforumID)
						if hasPerm, err := Services.HasPermission(currentUserID, permission, db); err == nil && hasPerm {
							canEdit = true
						}
					}
				}
			}
		}
		episode.CanEdit = &canEdit

		c.JSON(http.StatusOK, episode)
		return
	}

	c.JSON(http.StatusOK, entity)
}

func UpdateEpisode(c *gin.Context, db *sql.DB) {
	episodeID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid episode ID"})
		c.Abort()
		return
	}

	var req UpdateEpisodeRequest
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

	// 1. Fetch episode and topic details to check ownership and subforum
	var topicID int
	var authorUserID int
	var subforumID int
	query := `
		SELECT e.topic_id, t.author_user_id, t.subforum_id 
		FROM episode_base e 
		JOIN topics t ON e.topic_id = t.id 
		WHERE e.id = ?
	`
	err = db.QueryRow(query, episodeID).Scan(&topicID, &authorUserID, &subforumID)
	if err != nil {
		if err == sql.ErrNoRows {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Episode not found"})
		} else {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch episode details: " + err.Error()})
		}
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
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "You do not have permission to edit this episode"})
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

	// 3. Update Topic Name
	_, err = tx.Exec("UPDATE topics SET name = ? WHERE id = ?", req.Name, topicID)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update topic name: " + err.Error()})
		c.Abort()
		return
	}

	// 4. Update Episode Entity
	updates := map[string]interface{}{
		"name":          req.Name,
		"custom_fields": req.CustomFields,
	}
	if req.OpenToEveryone != nil {
		updates["open_to_everyone"] = *req.OpenToEveryone
	}
	_, err = Services.PatchEntity(int64(episodeID), "episode", updates, tx)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update episode entity: " + err.Error()})
		c.Abort()
		return
	}

	// 5. Update Episode-Character Relations
	// Wipe old relations
	_, err = tx.Exec("DELETE FROM episode_character WHERE episode_id = ?", episodeID)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to clear old character relations: " + err.Error()})
		c.Abort()
		return
	}

	// Insert new relations
	if len(req.CharacterIDs) > 0 {
		stmt, err := tx.Prepare("INSERT INTO episode_character (episode_id, character_id) VALUES (?, ?)")
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to prepare character relation statement"})
			c.Abort()
			return
		}
		defer stmt.Close()

		for _, charID := range req.CharacterIDs {
			_, err := stmt.Exec(episodeID, charID)
			if err != nil {
				_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to insert character relation: " + err.Error()})
				c.Abort()
				return
			}
		}
	}

	// 6. Update Episode-Mask Relations
	_, err = tx.Exec("DELETE FROM episode_mask WHERE episode_id = ?", episodeID)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to clear old mask relations: " + err.Error()})
		c.Abort()
		return
	}

	if len(req.MaskIds) > 0 {
		maskStmt, err := tx.Prepare("INSERT INTO episode_mask (episode_id, mask_id) VALUES (?, ?)")
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to prepare mask relation statement"})
			c.Abort()
			return
		}
		defer maskStmt.Close()

		for _, maskID := range req.MaskIds {
			_, err := maskStmt.Exec(episodeID, maskID)
			if err != nil {
				_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to insert mask relation: " + err.Error()})
				c.Abort()
				return
			}
		}
	}

	if err := tx.Commit(); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to commit transaction"})
		c.Abort()
		return
	}

	_, _ = db.Exec(
		"UPDATE subforums SET last_post_topic_name = ? WHERE last_post_topic_id = ?",
		req.Name, topicID,
	)

	// 6. Fetch updated episode and return
	updatedEpisode, err := Services.GetEntity(int64(episodeID), "episode", db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch updated episode: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, updatedEpisode)
}

func DeactivateEpisode(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid episode ID"})
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

	result, err := tx.Exec("UPDATE episode_base SET episode_status = ? WHERE id = ?", Entities.InactiveEpisode, id)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to deactivate episode: " + err.Error()})
		c.Abort()
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Episode not found"})
		c.Abort()
		return
	}

	_, err = tx.Exec("UPDATE topics SET status = ? WHERE id = (SELECT topic_id FROM episode_base WHERE id = ?)", Entities.InactiveTopic, id)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to deactivate episode topic: " + err.Error()})
		c.Abort()
		return
	}

	if err := tx.Commit(); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to commit transaction"})
		c.Abort()
		return
	}

	var topicStatus Entities.TopicStatus
	_ = db.QueryRow("SELECT status FROM topics WHERE id = (SELECT topic_id FROM episode_base WHERE id = ?)", id).Scan(&topicStatus)

	c.JSON(http.StatusOK, gin.H{
		"episode_status": Entities.InactiveEpisode,
		"topic_status":   topicStatus,
	})
}

func ActivateEpisode(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid episode ID"})
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

	result, err := tx.Exec("UPDATE episode_base SET episode_status = ? WHERE id = ?", Entities.ActiveEpisode, id)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to activate episode: " + err.Error()})
		c.Abort()
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Episode not found"})
		c.Abort()
		return
	}

	_, err = tx.Exec(
		"UPDATE topics SET status = ? WHERE id = (SELECT topic_id FROM episode_base WHERE id = ?) AND status = ?",
		Entities.ActiveTopic, id, Entities.InactiveTopic,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to activate episode topic: " + err.Error()})
		c.Abort()
		return
	}

	if err := tx.Commit(); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to commit transaction"})
		c.Abort()
		return
	}

	var topicStatus Entities.TopicStatus
	_ = db.QueryRow("SELECT status FROM topics WHERE id = (SELECT topic_id FROM episode_base WHERE id = ?)", id).Scan(&topicStatus)

	c.JSON(http.StatusOK, gin.H{
		"episode_status": Entities.ActiveEpisode,
		"topic_status":   topicStatus,
	})
}
