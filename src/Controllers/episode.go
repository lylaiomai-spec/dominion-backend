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
	SubforumID     int                    `json:"subforum_id" binding:"required"`
	Name           string                 `json:"name" binding:"required"`
	CharacterIDs   []int                  `json:"character_ids"`
	MaskIds        []int                  `json:"mask_ids"`
	WarningIds     []int                  `json:"warning_ids"`
	RatingSet      bool                   `json:"rating_set"`
	RatingLanguage int                    `json:"rating_language"`
	RatingViolence int                    `json:"rating_violence"`
	RatingSex      int                    `json:"rating_sex"`
	CustomFields   map[string]interface{} `json:"custom_fields"`
}

type UpdateEpisodeRequest struct {
	Name           string                 `json:"name" binding:"required"`
	CharacterIDs   []int                  `json:"character_ids"`
	MaskIds        []int                  `json:"mask_ids"`
	WarningIds     []int                  `json:"warning_ids"`
	RatingSet      *bool                  `json:"rating_set"`
	RatingLanguage *int                   `json:"rating_language"`
	RatingViolence *int                   `json:"rating_violence"`
	RatingSex      *int                   `json:"rating_sex"`
	CustomFields   map[string]interface{} `json:"custom_fields"`
	OpenToEveryone *bool                  `json:"open_to_everyone"`
}

type GetEpisodesRequest struct {
	SubforumIDs  []int    `json:"subforum_ids"`
	CharacterIDs []int    `json:"character_ids"`
	FactionIDs   []int    `json:"faction_ids"`
	Page         int      `json:"page"`
	Order        []string `json:"order"`
}

type GetEpisodesByMaskRequest struct {
	MaskID int      `json:"mask_id" binding:"required"`
	Page   int      `json:"page"`
	Order  []string `json:"order"`
}

type EpisodeListItem struct {
	Id           int                       `json:"id"`
	Name         string                    `json:"name"`
	TopicId      int                       `json:"topic_id"`
	SubforumId   int                       `json:"subforum_id"`
	SubforumName string                    `json:"subforum_name"`
	TopicStatus  int                       `json:"topic_status"`
	LastPostDate string                    `json:"last_post_date"`
	CustomFields map[string]interface{}    `json:"custom_fields"`
	Characters   []Entities.ShortCharacter `json:"characters"`
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
		Topic_Id:       int(topicID),
		Name:           req.Name,
		RatingSet:      req.RatingSet,
		RatingLanguage: req.RatingLanguage,
		RatingViolence: req.RatingViolence,
		RatingSex:      req.RatingSex,
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

	// 5. Insert Episode-Warning Relations
	if len(req.WarningIds) > 0 {
		warnStmt, err := tx.Prepare("INSERT INTO episode_warnings (episode_id, warning_id) VALUES (?, ?)")
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to prepare warning relation statement"})
			c.Abort()
			return
		}
		defer warnStmt.Close()

		for _, warningID := range req.WarningIds {
			_, err := warnStmt.Exec(createdEpisode.Id, warningID)
			if err != nil {
				_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to insert warning relation: " + err.Error()})
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
		UserID:     userID,
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

	// Fetch field config and filter out image/long_text content field types
	fieldConfig, _ := Services.GetFieldConfig("episode", db)
	var allowedFields []Entities.CustomFieldConfig
	for _, f := range fieldConfig {
		if f.ContentFieldType != "image" && f.ContentFieldType != "long_text" && f.ContentFieldType != "cropped_image" {
			allowedFields = append(allowedFields, f)
		}
	}

	// Build whitelist of sortable columns: base fields + allowed custom fields
	baseColumnMap := map[string]string{
		"id":             "e.id",
		"name":           "e.name",
		"topic_id":       "e.topic_id",
		"subforum_id":    "t.subforum_id",
		"subforum_name":  "s.name",
		"topic_status":   "t.status",
		"last_post_date": "t.date_last_post",
	}
	for _, f := range allowedFields {
		baseColumnMap[f.MachineFieldName] = "ef." + f.MachineFieldName
	}

	// Build ORDER BY clause
	var orderClauses []string
	for _, o := range req.Order {
		desc := false
		field := o
		if strings.HasPrefix(o, "-") {
			desc = true
			field = o[1:]
		}
		if col, ok := baseColumnMap[field]; ok {
			dir := "ASC"
			if desc {
				dir = "DESC"
			}
			orderClauses = append(orderClauses, col+" "+dir)
		}
	}
	orderBy := "t.date_last_post DESC"
	if len(orderClauses) > 0 {
		orderBy = strings.Join(orderClauses, ", ")
	}

	// Build custom field SELECT columns
	var customSelects []string
	for _, f := range allowedFields {
		customSelects = append(customSelects, "ef."+f.MachineFieldName)
	}
	customColSQL := ""
	if len(customSelects) > 0 {
		customColSQL = ", " + strings.Join(customSelects, ", ")
	}

	query := fmt.Sprintf(`SELECT e.id, e.name, e.topic_id, t.subforum_id, s.name, t.status, t.date_last_post%s
		FROM episode_base e
		JOIN topics t ON e.topic_id = t.id
		JOIN subforums s ON t.subforum_id = s.id
		LEFT JOIN episode_flattened ef ON ef.entity_id = e.id
		WHERE t.status != ?`, customColSQL)

	var args []interface{}
	args = append(args, Entities.DeletedTopic)

	placeholders := make([]string, len(allowedSubforumIDs))
	for i, id := range allowedSubforumIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	query += " AND t.subforum_id IN (" + strings.Join(placeholders, ",") + ")"

	if len(req.CharacterIDs) > 0 {
		ph := make([]string, len(req.CharacterIDs))
		for i, id := range req.CharacterIDs {
			ph[i] = "?"
			args = append(args, id)
		}
		query += " AND EXISTS (SELECT 1 FROM episode_character ec WHERE ec.episode_id = e.id AND ec.character_id IN (" + strings.Join(ph, ",") + "))"
	}

	if len(req.FactionIDs) > 0 {
		ph := make([]string, len(req.FactionIDs))
		for i, id := range req.FactionIDs {
			ph[i] = "?"
			args = append(args, id)
		}
		query += " AND EXISTS (SELECT 1 FROM episode_character ec JOIN character_faction cf ON ec.character_id = cf.character_id WHERE ec.episode_id = e.id AND cf.faction_id IN (" + strings.Join(ph, ",") + "))"
	}

	limit := 20
	page := req.Page
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * limit

	query += fmt.Sprintf(" ORDER BY %s LIMIT ? OFFSET ?", orderBy)
	args = append(args, limit, offset)

	rows, err := db.Query(query, args...)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get episodes: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	episodes := []EpisodeListItem{}
	for rows.Next() {
		var ep EpisodeListItem
		ep.CustomFields = map[string]interface{}{}
		ep.Characters = []Entities.ShortCharacter{}

		// Scan base fields + custom fields using RawBytes
		customDests := make([]interface{}, len(allowedFields))
		rawVals := make([]*sql.RawBytes, len(allowedFields))
		for i := range allowedFields {
			rawVals[i] = new(sql.RawBytes)
			customDests[i] = rawVals[i]
		}
		scanDests := []interface{}{&ep.Id, &ep.Name, &ep.TopicId, &ep.SubforumId, &ep.SubforumName, &ep.TopicStatus, &ep.LastPostDate}
		scanDests = append(scanDests, customDests...)

		if err := rows.Scan(scanDests...); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan episode: " + err.Error()})
			c.Abort()
			return
		}

		for i, f := range allowedFields {
			if rawVals[i] != nil && *rawVals[i] != nil {
				ep.CustomFields[f.MachineFieldName] = string(*rawVals[i])
			}
		}

		episodes = append(episodes, ep)
	}

	// Batch-fetch characters for all episodes
	if len(episodes) > 0 {
		epIDs := make([]interface{}, len(episodes))
		epPH := make([]string, len(episodes))
		epIdx := make(map[int]int, len(episodes))
		for i, ep := range episodes {
			epIDs[i] = ep.Id
			epPH[i] = "?"
			epIdx[ep.Id] = i
		}
		charRows, err := db.Query(fmt.Sprintf(
			"SELECT ec.episode_id, cb.id, cb.name FROM episode_character ec JOIN character_base cb ON ec.character_id = cb.id WHERE ec.episode_id IN (%s) ORDER BY cb.name ASC",
			strings.Join(epPH, ","),
		), epIDs...)
		if err == nil {
			defer charRows.Close()
			for charRows.Next() {
				var epID int
				var ch Entities.ShortCharacter
				if charRows.Scan(&epID, &ch.Id, &ch.Name) == nil {
					if idx, ok := epIdx[epID]; ok {
						episodes[idx].Characters = append(episodes[idx].Characters, ch)
					}
				}
			}
		}
	}

	c.JSON(http.StatusOK, episodes)
}

func GetEpisodesByMask(c *gin.Context, db *sql.DB) {
	var req GetEpisodesByMaskRequest
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

	if len(visibleSubforumIDs) == 0 {
		c.JSON(http.StatusOK, []EpisodeListItem{})
		return
	}

	fieldConfig, _ := Services.GetFieldConfig("episode", db)
	var allowedFields []Entities.CustomFieldConfig
	for _, f := range fieldConfig {
		if f.ContentFieldType != "image" && f.ContentFieldType != "long_text" && f.ContentFieldType != "cropped_image" {
			allowedFields = append(allowedFields, f)
		}
	}

	baseColumnMap := map[string]string{
		"id":             "e.id",
		"name":           "e.name",
		"topic_id":       "e.topic_id",
		"subforum_id":    "t.subforum_id",
		"subforum_name":  "s.name",
		"topic_status":   "t.status",
		"last_post_date": "t.date_last_post",
	}
	for _, f := range allowedFields {
		baseColumnMap[f.MachineFieldName] = "ef." + f.MachineFieldName
	}

	var orderClauses []string
	for _, o := range req.Order {
		desc := false
		field := o
		if strings.HasPrefix(o, "-") {
			desc = true
			field = o[1:]
		}
		if col, ok := baseColumnMap[field]; ok {
			dir := "ASC"
			if desc {
				dir = "DESC"
			}
			orderClauses = append(orderClauses, col+" "+dir)
		}
	}
	orderBy := "t.date_last_post DESC"
	if len(orderClauses) > 0 {
		orderBy = strings.Join(orderClauses, ", ")
	}

	var customSelects []string
	for _, f := range allowedFields {
		customSelects = append(customSelects, "ef."+f.MachineFieldName)
	}
	customColSQL := ""
	if len(customSelects) > 0 {
		customColSQL = ", " + strings.Join(customSelects, ", ")
	}

	subforumPH := make([]string, len(visibleSubforumIDs))
	var args []interface{}
	args = append(args, Entities.DeletedTopic)
	args = append(args, req.MaskID)
	for i, id := range visibleSubforumIDs {
		subforumPH[i] = "?"
		args = append(args, id)
	}

	query := fmt.Sprintf(`SELECT e.id, e.name, e.topic_id, t.subforum_id, s.name, t.status, t.date_last_post%s
		FROM episode_base e
		JOIN topics t ON e.topic_id = t.id
		JOIN subforums s ON t.subforum_id = s.id
		LEFT JOIN episode_flattened ef ON ef.entity_id = e.id
		WHERE t.status != ?
		AND EXISTS (SELECT 1 FROM episode_mask em WHERE em.episode_id = e.id AND em.mask_id = ?)
		AND t.subforum_id IN (%s)`, customColSQL, strings.Join(subforumPH, ","))

	limit := 20
	page := req.Page
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * limit

	query += fmt.Sprintf(" ORDER BY %s LIMIT ? OFFSET ?", orderBy)
	args = append(args, limit, offset)

	rows, err := db.Query(query, args...)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get episodes: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	episodes := []EpisodeListItem{}
	for rows.Next() {
		var ep EpisodeListItem
		ep.CustomFields = map[string]interface{}{}
		ep.Characters = []Entities.ShortCharacter{}

		customDests := make([]interface{}, len(allowedFields))
		rawVals := make([]*sql.RawBytes, len(allowedFields))
		for i := range allowedFields {
			rawVals[i] = new(sql.RawBytes)
			customDests[i] = rawVals[i]
		}
		scanDests := []interface{}{&ep.Id, &ep.Name, &ep.TopicId, &ep.SubforumId, &ep.SubforumName, &ep.TopicStatus, &ep.LastPostDate}
		scanDests = append(scanDests, customDests...)

		if err := rows.Scan(scanDests...); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan episode: " + err.Error()})
			c.Abort()
			return
		}

		for i, f := range allowedFields {
			if rawVals[i] != nil && *rawVals[i] != nil {
				ep.CustomFields[f.MachineFieldName] = string(*rawVals[i])
			}
		}

		episodes = append(episodes, ep)
	}

	if len(episodes) > 0 {
		epIDs := make([]interface{}, len(episodes))
		epPH := make([]string, len(episodes))
		epIdx := make(map[int]int, len(episodes))
		for i, ep := range episodes {
			epIDs[i] = ep.Id
			epPH[i] = "?"
			epIdx[ep.Id] = i
		}
		charRows, err := db.Query(fmt.Sprintf(
			"SELECT ec.episode_id, cb.id, cb.name FROM episode_character ec JOIN character_base cb ON ec.character_id = cb.id WHERE ec.episode_id IN (%s) ORDER BY cb.name ASC",
			strings.Join(epPH, ","),
		), epIDs...)
		if err == nil {
			defer charRows.Close()
			for charRows.Next() {
				var epID int
				var ch Entities.ShortCharacter
				if charRows.Scan(&epID, &ch.Id, &ch.Name) == nil {
					if idx, ok := epIdx[epID]; ok {
						episodes[idx].Characters = append(episodes[idx].Characters, ch)
					}
				}
			}
		}
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
		var topicStatus Entities.TopicStatus
		if err := db.QueryRow("SELECT status FROM topics WHERE id = ?", episode.Topic_Id).Scan(&topicStatus); err == nil && topicStatus == Entities.DeletedTopic {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Episode not found"})
			c.Abort()
			return
		}

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

	// Capture previous character IDs before relations are wiped
	var prevCharacterIDs []int
	if prevCharRows, err2 := db.Query(`SELECT character_id FROM episode_character WHERE episode_id = ?`, episodeID); err2 == nil {
		defer prevCharRows.Close()
		for prevCharRows.Next() {
			var cid int
			if prevCharRows.Scan(&cid) == nil {
				prevCharacterIDs = append(prevCharacterIDs, cid)
			}
		}
	}

	// Capture previous mask owner user IDs before relations are wiped
	var prevMaskUserIDs []int
	if prevRows, err2 := db.Query(`SELECT DISTINCT cpb.user_id FROM character_profile_base cpb JOIN episode_mask em ON em.mask_id = cpb.id WHERE em.episode_id = ?`, episodeID); err2 == nil {
		defer prevRows.Close()
		for prevRows.Next() {
			var uid int
			if prevRows.Scan(&uid) == nil {
				prevMaskUserIDs = append(prevMaskUserIDs, uid)
			}
		}
	}

	// Resolve new mask owner user IDs from requested mask IDs
	var newMaskUserIDs []int
	if len(req.MaskIds) > 0 {
		placeholders := strings.Repeat("?,", len(req.MaskIds)-1) + "?"
		args := make([]interface{}, len(req.MaskIds))
		for i, id := range req.MaskIds {
			args[i] = id
		}
		if newRows, err2 := db.Query(fmt.Sprintf("SELECT DISTINCT user_id FROM character_profile_base WHERE id IN (%s)", placeholders), args...); err2 == nil {
			defer newRows.Close()
			for newRows.Next() {
				var uid int
				if newRows.Scan(&uid) == nil {
					newMaskUserIDs = append(newMaskUserIDs, uid)
				}
			}
		}
	}

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
	if req.RatingSet != nil {
		updates["rating_set"] = *req.RatingSet
	}
	if req.RatingLanguage != nil {
		updates["rating_language"] = *req.RatingLanguage
	}
	if req.RatingViolence != nil {
		updates["rating_violence"] = *req.RatingViolence
	}
	if req.RatingSex != nil {
		updates["rating_sex"] = *req.RatingSex
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

	// 7. Update Episode-Warning Relations
	_, err = tx.Exec("DELETE FROM episode_warnings WHERE episode_id = ?", episodeID)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to clear old warning relations: " + err.Error()})
		c.Abort()
		return
	}

	if len(req.WarningIds) > 0 {
		warnStmt, err := tx.Prepare("INSERT INTO episode_warnings (episode_id, warning_id) VALUES (?, ?)")
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to prepare warning relation statement"})
			c.Abort()
			return
		}
		defer warnStmt.Close()

		for _, warningID := range req.WarningIds {
			_, err := warnStmt.Exec(episodeID, warningID)
			if err != nil {
				_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to insert warning relation: " + err.Error()})
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

	Events.Publish(db, Events.EpisodeUpdated, Events.EpisodeUpdatedEvent{
		EpisodeID:        int64(episodeID),
		SubforumID:       subforumID,
		UserID:           userID,
		PrevMaskUserIDs:  prevMaskUserIDs,
		NewMaskUserIDs:   newMaskUserIDs,
		PrevCharacterIDs: prevCharacterIDs,
		NewCharacterIDs:  req.CharacterIDs,
	})

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

func AddEpisodeWarningsConsent(c *gin.Context, db *sql.DB) {
	episodeId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid episode id"})
		c.Abort()
		return
	}

	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		c.Abort()
		return
	}

	_, err = db.Exec(
		"INSERT IGNORE INTO user_episode_warnings_consent (episode_id, user_id) VALUES (?, ?)",
		episodeId, userID,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to save consent: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Consent recorded"})
}
