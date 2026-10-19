package Controllers

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func GetPostsByUser(c *gin.Context, db *sql.DB) {
	userIDStr := c.Param("user_id")
	pageStr := c.Query("page")

	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid user ID"})
		c.Abort()
		return
	}

	currentUserID := Services.GetUserIdFromContext(c)
	domain, _ := Services.GetGlobalSetting("domain", db)

	visibleSubforumIDs, err := Services.GetVisibleSubforums(currentUserID, "subforum_read", db)
	if err != nil || len(visibleSubforumIDs) == 0 {
		c.JSON(http.StatusOK, gin.H{"page": 1, "total_posts": 0, "posts": []interface{}{}})
		return
	}

	sfPlaceholders := make([]string, len(visibleSubforumIDs))
	sfArgs := make([]interface{}, len(visibleSubforumIDs))
	for i, id := range visibleSubforumIDs {
		sfPlaceholders[i] = "?"
		sfArgs[i] = id
	}
	sfInClause := strings.Join(sfPlaceholders, ",")

	postsPerPage := Services.GetPostsPerPage(db)
	page := 1
	if pageStr != "" {
		if p, e := strconv.Atoi(pageStr); e == nil && p > 0 {
			page = p
		}
	}
	offset := (page - 1) * postsPerPage

	countArgs := append(append([]interface{}{userID}, sfArgs...), Entities.DeletedTopic)
	var totalPosts int
	_ = db.QueryRow(
		fmt.Sprintf(
			"SELECT COUNT(*) FROM posts p JOIN topics t ON p.topic_id = t.id WHERE p.author_user_id = ? AND t.subforum_id IN (%s) AND (p.is_deleted IS NULL OR p.is_deleted <> 1) AND t.status != ?",
			sfInClause,
		),
		countArgs...,
	).Scan(&totalPosts)

	var configJSON string
	if err := db.QueryRow("SELECT config FROM custom_field_config WHERE entity_type = 'character_profile'").Scan(&configJSON); err != nil {
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

	mainArgs := append(append([]interface{}{userID}, sfArgs...), Entities.DeletedTopic, postsPerPage, offset)

	query := fmt.Sprintf(`
		SELECT
			p.id, p.topic_id, p.author_user_id, p.date_created, p.content, p.use_character_profile,
			u.username, u.avatar, u.total_posts, u.total_general_posts, p.guest_name,
			cp.id as character_profile_id, cp.character_id, cb.name as character_name, cp.avatar as character_avatar, cp.mask_name, cp.is_mask, cp.signature as character_signature,
			u.signature as user_signature,
			t.subforum_id, t.type as topic_type, t.name as topic_name,
			ec.custom_avatar as episode_custom_avatar,
			p.is_gm_post
			%s
		FROM posts p
		JOIN topics t ON p.topic_id = t.id
		LEFT JOIN users u ON p.author_user_id = u.id
		LEFT JOIN character_profile_base cp ON p.character_profile_id = cp.id
		LEFT JOIN character_base cb ON cp.character_id = cb.id
		LEFT JOIN character_profile_flattened cpf ON cp.id = cpf.entity_id
		LEFT JOIN episode_base eb ON eb.topic_id = t.id
		LEFT JOIN episode_character ec ON ec.episode_id = eb.id AND ec.character_id = cb.id
		%s
		WHERE p.author_user_id = ? AND t.subforum_id IN (%s) AND (p.is_deleted IS NULL OR p.is_deleted <> 1) AND t.status != ?
		ORDER BY p.id DESC
		LIMIT ? OFFSET ?
	`, colsSelect, currencyJoin, sfInClause)

	rows, err := db.Query(query, mainArgs...)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get posts: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	cols, _ := rows.Columns()
	posts := make([]Entities.Post, 0)
	userTimezone := Services.GetUserTimezone(currentUserID, db)

	dateCreatedIdx := -1
	for i, col := range cols {
		if col == "date_created" {
			dateCreatedIdx = i
			break
		}
	}

	for rows.Next() {
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
				rowMap[colName] = string(val)
			}
		}

		var post Entities.Post
		post.Id, _ = strconv.Atoi(rowMap["id"].(string))
		post.TopicId, _ = strconv.Atoi(rowMap["topic_id"].(string))
		post.AuthorUserId, _ = strconv.Atoi(rowMap["author_user_id"].(string))
		if username, ok := rowMap["username"]; ok {
			post.AuthorUserName = username.(string)
		}
		post.DateCreated = dateCreated
		post.DateCreatedLocalized = Services.LocalizeTime(post.DateCreated, userTimezone)
		post.Content = rowMap["content"].(string)
		post.ContentHtml = Services.ProcessHideBlocks(Services.LinkifyURLs(Services.ResolveNPCPlaceholders(Services.ParseBBCode(post.Content), db), domain, db), currentUserID, post.TopicId, db)
		post.UseCharacterProfile, _ = strconv.ParseBool(rowMap["use_character_profile"].(string))
		if v, ok := rowMap["is_gm_post"]; ok {
			b, _ := strconv.ParseBool(v.(string))
			post.IsGmPost = &b
		}
		if tn, ok := rowMap["topic_name"]; ok {
			post.TopicName = tn.(string)
		}

		subforumID, _ := strconv.Atoi(rowMap["subforum_id"].(string))
		topicTypeInt, _ := strconv.Atoi(rowMap["topic_type"].(string))
		if Entities.TopicType(topicTypeInt) != Entities.EpisodeTopic {
			post.ContentHtml = mentionRegexp.ReplaceAllString(post.ContentHtml, `<span class="mention">$0</span>`)
		}

		canEdit := false
		if currentUserID != 0 {
			if currentUserID == post.AuthorUserId {
				if hasPerm, err := Services.HasPermission(currentUserID, fmt.Sprintf("subforum_edit_own_post:%d", subforumID), db); err == nil && hasPerm {
					canEdit = true
				}
			} else {
				if hasPerm, err := Services.HasPermission(currentUserID, fmt.Sprintf("subforum_edit_others_post:%d", subforumID), db); err == nil && hasPerm {
					canEdit = true
				}
			}
		}
		post.CanEdit = &canEdit

		canDelete := false
		if currentUserID != 0 {
			if currentUserID == post.AuthorUserId {
				if hasPerm, err := Services.HasPermission(currentUserID, fmt.Sprintf("subforum_delete_own_post:%d", subforumID), db); err == nil && hasPerm {
					canDelete = true
				}
			} else {
				if hasPerm, err := Services.HasPermission(currentUserID, fmt.Sprintf("subforum_delete_others_post:%d", subforumID), db); err == nil && hasPerm {
					canDelete = true
				}
			}
		}
		post.CanDelete = &canDelete

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
			if customAvatar, ok := rowMap["episode_custom_avatar"]; ok && customAvatar.(string) != "" {
				avatarStr := customAvatar.(string)
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
			if sig, ok := rowMap["character_signature"]; ok {
				sigStr := sig.(string)
				sigHtml := Services.LinkifyURLs(Services.ParseBBCode(sigStr), domain, db)
				charProfile.Signature = &sigStr
				charProfile.SignatureHtml = &sigHtml
			}
			customFields := make(map[string]Entities.CustomFieldValue)
			for _, field := range customConfig {
				if val, ok := rowMap[field.MachineFieldName]; ok {
					cfValue := Entities.CustomFieldValue{Content: val}
					if field.FieldType == "text" {
						if s, ok := val.(string); ok {
							cfValue.ContentHtml = Services.ParseBBCode(s)
						}
					} else if field.FieldType == "select" && field.Options != nil {
						cfValue.Content = Services.ResolveSelectField(val, field.Options)
					}
					customFields[field.MachineFieldName] = cfValue
				}
			}
			charProfile.CustomFields.CustomFields = customFields
			charProfile.CustomFields.FieldConfig = customConfig
			charProfile.Factions = []Entities.Faction{}
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
			if sig, ok := rowMap["user_signature"]; ok {
				sigStr := sig.(string)
				sigHtml := Services.LinkifyURLs(Services.ParseBBCode(sigStr), domain, db)
				userProfile.Signature = &sigStr
				userProfile.SignatureHtml = &sigHtml
			}
			post.UserProfile = &userProfile
		}
		posts = append(posts, post)
	}

	var charIDs []interface{}
	charIDToPostIdxs := make(map[int][]int)
	for i, p := range posts {
		if p.CharacterProfile != nil && p.CharacterProfile.CharacterId != nil {
			cid := *p.CharacterProfile.CharacterId
			if _, seen := charIDToPostIdxs[cid]; !seen {
				charIDs = append(charIDs, cid)
			}
			charIDToPostIdxs[cid] = append(charIDToPostIdxs[cid], i)
		}
	}
	if len(charIDs) > 0 {
		factionPlaceholders := make([]string, len(charIDs))
		for i := range charIDs {
			factionPlaceholders[i] = "?"
		}
		factionRows, err := db.Query(fmt.Sprintf(`
			SELECT cf.character_id, f.id, f.name, f.parent_id, f.level, f.description, f.icon, f.faction_status
			FROM character_faction cf
			JOIN factions f ON f.id = cf.faction_id
			WHERE f.show_on_profile = true AND cf.character_id IN (%s)
			ORDER BY f.level ASC
		`, strings.Join(factionPlaceholders, ",")), charIDs...)
		if err == nil {
			defer factionRows.Close()
			for factionRows.Next() {
				var cid int
				var f Entities.Faction
				if err := factionRows.Scan(&cid, &f.Id, &f.Name, &f.ParentId, &f.Level, &f.Description, &f.Icon, &f.FactionStatus); err != nil {
					continue
				}
				for _, idx := range charIDToPostIdxs[cid] {
					posts[idx].CharacterProfile.Factions = append(posts[idx].CharacterProfile.Factions, f)
				}
			}
		}
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
			postIndex := make(map[int]int, len(posts))
			for i, p := range posts {
				postIndex[p.Id] = i
				posts[i].Reactions = []Entities.PostReaction{}
			}
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
		"page":        page,
		"total_posts": totalPosts,
		"posts":       posts,
	})
}
