package Controllers

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Events"
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

type CreateCharacterRequest struct {
	SubforumID   int                    `json:"subforum_id" binding:"required"`
	Name         string                 `json:"name" binding:"required"`
	Avatar       *string                `json:"avatar"`
	CustomFields map[string]interface{} `json:"custom_fields"`
	FactionIDs   []Entities.Faction     `json:"factions"`
	ClaimId      *int                   `json:"claim_id"`
	ClaimType    string                 `json:"claim_type"`
}

type UpdateCharacterRequest struct {
	Name         string                 `json:"name" binding:"required"`
	Avatar       *string                `json:"avatar"`
	CustomFields map[string]interface{} `json:"custom_fields"`
	FactionIDs   []Entities.Faction     `json:"factions"`
	ClaimId      *int                   `json:"claim_id"`
	ClaimType    string                 `json:"claim_type"`
}

func GetCharacter(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid Id"})
		c.Abort()
		return
	}

	entity, err := Services.GetEntity(int64(id), "character", db)
	if err != nil {
		if err == sql.ErrNoRows {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Character not found"})
		} else {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get character: " + err.Error()})
		}
		c.Abort()
		return
	}

	if character, ok := entity.(*Entities.Character); ok {
		// Fetch episodes for this character
		query := `
			SELECT e.id, e.name, e.topic_id, t.date_last_post, u.username as last_post_author_username
			FROM episode_base e
			JOIN episode_character ec ON e.id = ec.episode_id
			JOIN topics t ON e.topic_id = t.id
			LEFT JOIN users u ON t.last_post_author_user_id = u.id
			WHERE ec.character_id = ?
			ORDER BY t.date_last_post DESC
		`
		rows, err := db.Query(query, character.Id)
		if err == nil {
			defer rows.Close()
			var episodes []Entities.EpisodeListItem
			for rows.Next() {
				var ep Entities.EpisodeListItem
				if err := rows.Scan(&ep.Id, &ep.Name, &ep.TopicId, &ep.DateLastPost, &ep.LastPostAuthorUsername); err == nil {
					// Fetch all characters for this episode
					charRows, err := db.Query(`
						SELECT cb.id, cb.name 
						FROM character_base cb 
						JOIN episode_character ec ON cb.id = ec.character_id 
						WHERE ec.episode_id = ?`, ep.Id)
					if err == nil {
						var characters []*Entities.ShortCharacter
						for charRows.Next() {
							var char Entities.ShortCharacter
							if err := charRows.Scan(&char.Id, &char.Name); err == nil {
								characters = append(characters, &char)
							}
						}
						ep.Characters = characters
						charRows.Close()
					}
					episodes = append(episodes, ep)
				}
			}
			character.Episodes = episodes
		}

		// Fetch factions
		character.Factions, _ = Services.GetFactionTreeByCharacter(character.Id, db)

		// Check CanEdit
		currentUserID := Services.GetUserIdFromContext(c)
		canEdit := false
		if currentUserID != 0 {
			// Fetch subforum ID for permission check
			var subforumID int
			err = db.QueryRow("SELECT subforum_id FROM topics WHERE id = ?", character.TopicId).Scan(&subforumID)
			if err == nil {
				// Check for "Edit others' topic" permission
				permission := fmt.Sprintf("subforum_edit_others_topic:%d", subforumID)
				if hasPerm, err := Services.HasPermission(currentUserID, permission, db); err == nil && hasPerm {
					canEdit = true
				} else {
					// Check if user is the author of the topic
					var authorUserID int
					err = db.QueryRow("SELECT author_user_id FROM topics WHERE id = ?", character.TopicId).Scan(&authorUserID)
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
		character.CanEdit = &canEdit

		var cr Entities.ClaimRecord
		err = db.QueryRow(`
			SELECT id, claim_id, user_id, guest_hash, is_guest, claim_date, claim_expiration_date, character_id, claim_created_with_character_sheet
			FROM claim_record
			WHERE character_id = ?
			ORDER BY claim_date DESC
			LIMIT 1
		`, character.Id).Scan(&cr.Id, &cr.ClaimId, &cr.UserId, &cr.GuestHash, &cr.IsGuest, &cr.ClaimDate, &cr.ClaimExpirationDate, &cr.CharacterId, &cr.ClaimCreatedWithCharacterSheet)
		if err == nil {
			character.ClaimRecord = &cr
		}

		c.JSON(http.StatusOK, character)
		return
	}

	c.JSON(http.StatusOK, entity)
}

func CreateCharacter(c *gin.Context, db *sql.DB) {
	var req CreateCharacterRequest
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

	// Insert Topic (without first post)
	res, err := tx.Exec("INSERT INTO topics (subforum_id, name, author_user_id, date_created, date_last_post, status, type, post_number, last_post_author_user_id) VALUES (?, ?, ?, NOW(), NOW(), 0, ?, 0, ?)",
		req.SubforumID, req.Name, userID, Entities.CharacterSheetTopic, userID)
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

	// Convert CustomFields to the expected type
	cfMap := make(map[string]Entities.CustomFieldValue)
	for k, v := range req.CustomFields {
		cfMap[k] = Entities.CustomFieldValue{Content: v}
	}

	character := Entities.Character{
		UserId:          userID,
		TopicId:         int(topicID),
		Name:            req.Name,
		Avatar:          req.Avatar,
		CharacterStatus: Entities.PendingCharacter,
		CustomFields: Entities.CustomFieldEntity{
			CustomFields: cfMap,
		},
	}

	createdEntity, characterID, err := Services.CreateEntity("character", &character, tx)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create character: " + err.Error()})
		c.Abort()
		return
	}

	// Handle factions
	for _, faction := range req.FactionIDs {
		var factionID int

		// If faction ID is negative, create a new faction
		if faction.Id < 0 {
			newFactionID, err := Services.CreateFaction(faction, tx)
			if err != nil {
				_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create faction: " + err.Error()})
				c.Abort()
				return
			}
			factionID = int(newFactionID)
		} else {
			factionID = faction.Id
		}

		// Add faction to character
		if err := Services.AddFactionCharacter(factionID, int(characterID), tx); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to add faction to character: " + err.Error()})
			c.Abort()
			return
		}
	}

	if req.ClaimId != nil && req.ClaimType != "" {
		claimId := *req.ClaimId

		if req.ClaimType == "wanted_character" {
			var characterClaimId *int
			if err := tx.QueryRow("SELECT character_claim_id FROM wanted_character_base WHERE id = ?", claimId).Scan(&characterClaimId); err != nil || characterClaimId == nil {
				_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Failed to resolve wanted character claim"})
				c.Abort()
				return
			}
			claimId = *characterClaimId
		}

		// Expire the existing standalone claim record if present
		var existingRecordId int
		err := tx.QueryRow("SELECT id FROM claim_record WHERE claim_id = ? AND user_id = ? AND character_id IS NULL", claimId, userID).Scan(&existingRecordId)
		if err == nil {
			if _, err := tx.Exec("UPDATE claim_record SET claim_expiration_date = DATE_SUB(NOW(), INTERVAL 1 MINUTE) WHERE id = ?", existingRecordId); err != nil {
				_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to expire old claim record: " + err.Error()})
				c.Abort()
				return
			}
		} else if err != sql.ErrNoRows {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to check claim record: " + err.Error()})
			c.Abort()
			return
		}

		// Create a new claim record associated with the character sheet
		newClaimRes, err := tx.Exec(
			"INSERT INTO claim_record (claim_id, user_id, is_guest, claim_date, claim_expiration_date, character_id, claim_created_with_character_sheet) VALUES (?, ?, false, NOW(), DATE_ADD(NOW(), INTERVAL 5 DAY), ?, true)",
			claimId, userID, characterID,
		)
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create claim record: " + err.Error()})
			c.Abort()
			return
		}
		newClaimRecordId, err := newClaimRes.LastInsertId()
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get new claim record ID"})
			c.Abort()
			return
		}

		// Update character_claim so the new record shows in the character list
		if _, err := tx.Exec("UPDATE character_claim SET claim_record_id = ? WHERE id = ?", newClaimRecordId, claimId); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update character claim: " + err.Error()})
			c.Abort()
			return
		}
	}

	if err := tx.Commit(); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to commit transaction"})
		c.Abort()
		return
	}

	// Emit CharacterCreated event
	Events.Publish(db, Events.CharacterCreated, Events.CharacterCreatedEvent{
		CharacterID: characterID,
		SubforumID:  req.SubforumID,
		TopicID:     topicID,
		TopicName:   req.Name,
	})

	c.JSON(http.StatusCreated, createdEntity)
}

func PreviewCharacter(c *gin.Context, db *sql.DB) {
	var req CreateCharacterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	cfMap := make(map[string]Entities.CustomFieldValue)
	for k, v := range req.CustomFields {
		cfMap[k] = Entities.CustomFieldValue{Content: v}
	}

	fieldConfig, _ := Services.GetFieldConfig("character", db)
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

	character := Entities.Character{
		Name:   req.Name,
		Avatar: req.Avatar,
		CustomFields: Entities.CustomFieldEntity{
			CustomFields: cfMap,
			FieldConfig:  fieldConfig,
		},
		Factions: req.FactionIDs,
	}

	c.JSON(http.StatusOK, character)
}

func UpdateCharacter(c *gin.Context, db *sql.DB) {
	characterID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid character ID"})
		c.Abort()
		return
	}

	var req UpdateCharacterRequest
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

	// 1. Fetch character and topic details to check ownership and subforum
	var topicID int
	var authorUserID int
	var subforumID int
	query := `
		SELECT c.topic_id, t.author_user_id, t.subforum_id 
		FROM character_base c 
		JOIN topics t ON c.topic_id = t.id 
		WHERE c.id = ?
	`
	err = db.QueryRow(query, characterID).Scan(&topicID, &authorUserID, &subforumID)
	if err != nil {
		if err == sql.ErrNoRows {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Character not found"})
		} else {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch character details: " + err.Error()})
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
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "You do not have permission to edit this character"})
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

	// 4. Update Character Entity
	updates := map[string]interface{}{
		"name":          req.Name,
		"avatar":        req.Avatar,
		"custom_fields": req.CustomFields,
	}
	_, err = Services.PatchEntity(int64(characterID), "character", updates, tx)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update character entity: " + err.Error()})
		c.Abort()
		return
	}

	// 5. Update Character-Faction Relations
	// Wipe old relations
	_, err = tx.Exec("DELETE FROM character_faction WHERE character_id = ?", characterID)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to clear old faction relations: " + err.Error()})
		c.Abort()
		return
	}

	// Insert new relations
	for _, faction := range req.FactionIDs {
		var factionID int
		if faction.Id < 0 {
			newFactionID, err := Services.CreateFaction(faction, tx)
			if err != nil {
				_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create faction: " + err.Error()})
				c.Abort()
				return
			}
			factionID = int(newFactionID)
		} else {
			factionID = faction.Id
		}

		if err := Services.AddFactionCharacter(factionID, characterID, tx); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to add faction to character: " + err.Error()})
			c.Abort()
			return
		}
	}

	if req.ClaimId != nil {
		newClaimId := *req.ClaimId

		if newClaimId != -1 && req.ClaimType == "wanted_character" {
			var characterClaimId *int
			if err := tx.QueryRow("SELECT character_claim_id FROM wanted_character_base WHERE id = ?", newClaimId).Scan(&characterClaimId); err != nil || characterClaimId == nil {
				_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Failed to resolve wanted character claim"})
				c.Abort()
				return
			}
			newClaimId = *characterClaimId
		}

		var existingRecordId int
		var existingClaimId int
		err := tx.QueryRow("SELECT id, claim_id FROM claim_record WHERE character_id = ? AND user_id = ?", characterID, userID).Scan(&existingRecordId, &existingClaimId)
		if err == nil {
			if newClaimId == -1 || existingClaimId != newClaimId {
				if _, err := tx.Exec("UPDATE claim_record SET claim_expiration_date = DATE_SUB(NOW(), INTERVAL 1 MINUTE) WHERE id = ?", existingRecordId); err != nil {
					_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to expire claim record: " + err.Error()})
					c.Abort()
					return
				}
				if newClaimId != -1 {
					if _, err := tx.Exec(
						"INSERT INTO claim_record (claim_id, user_id, is_guest, claim_date, claim_expiration_date, character_id, claim_created_with_character_sheet) VALUES (?, ?, false, NOW(), DATE_ADD(NOW(), INTERVAL 5 DAY), ?, true)",
						newClaimId, userID, characterID,
					); err != nil {
						_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create claim record: " + err.Error()})
						c.Abort()
						return
					}
				}
			}
		} else if err == sql.ErrNoRows && newClaimId != -1 {
			if _, err := tx.Exec(
				"INSERT INTO claim_record (claim_id, user_id, is_guest, claim_date, claim_expiration_date, character_id, claim_created_with_character_sheet) VALUES (?, ?, false, NOW(), DATE_ADD(NOW(), INTERVAL 5 DAY), ?, true)",
				newClaimId, userID, characterID,
			); err != nil {
				_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create claim record: " + err.Error()})
				c.Abort()
				return
			}
		} else if err != nil && err != sql.ErrNoRows {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to check claim record: " + err.Error()})
			c.Abort()
			return
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

	// 6. Fetch updated character and return
	updatedCharacter, err := Services.GetEntity(int64(characterID), "character", db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch updated character: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, updatedCharacter)
}

func PatchCharacter(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid Id"})
		c.Abort()
		return
	}

	var jsonMap map[string]interface{}
	if err := c.ShouldBindJSON(&jsonMap); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	updatedEntity, err := Services.PatchEntity(int64(id), "character", jsonMap, db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to patch character: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, updatedEntity)
}

func GetCharacterList(c *gin.Context, db *sql.DB) {
	// 1. Get the faction tree
	factions, err := Services.GetFactionTree(db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get faction tree: " + err.Error()})
		c.Abort()
		return
	}

	// 2. Create a map for easy access to factions by ID
	factionMap := make(map[int]*Entities.Faction)
	for i := range factions {
		factions[i].Characters = []Entities.CharacterListItem{}
		factionMap[factions[i].Id] = &factions[i]
	}

	// 3. Fetch active characters with their deepest faction
	charQuery := `
		WITH RankedFactions AS (
			SELECT
				c.id,
				c.name,
				f.id AS faction_id,
				ROW_NUMBER() OVER(PARTITION BY c.id ORDER BY f.level DESC) AS rn
			FROM character_base c
			JOIN character_faction cf ON c.id = cf.character_id
			JOIN factions f ON cf.faction_id = f.id
			WHERE c.character_status = 0
		)
		SELECT id, name, faction_id FROM RankedFactions WHERE rn = 1
	`
	charRows, err := db.Query(charQuery)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get characters: " + err.Error()})
		c.Abort()
		return
	}
	defer charRows.Close()

	for charRows.Next() {
		var item Entities.CharacterListItem
		var factionID int
		if err := charRows.Scan(&item.Id, &item.Name, &factionID); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan character: " + err.Error()})
			c.Abort()
			return
		}
		item.IsClaim = false
		item.WantedCharacterId = nil
		if faction, ok := factionMap[factionID]; ok {
			faction.Characters = append(faction.Characters, item)
		}
	}

	// 4. Collect current user identity for pending faction exception
	userID := Services.GetUserIdFromContext(c)
	userTimezone := Services.GetUserTimezone(userID, db)
	guestHashes := make([]string, 3)
	for i := 1; i <= 3; i++ {
		if hash, err := c.Cookie(fmt.Sprintf("claim_hash_%d", i)); err == nil {
			guestHashes[i-1] = hash
		}
	}

	// 5. Fetch character claims with their deepest faction and associated wanted character.
	//    Pending factions are included only when the claim belongs to the current user.
	claimQuery := `
		WITH RankedFactions AS (
			SELECT
				cc.id,
				cc.name,
				cc.show_only_with_active_claim,
				cc.claim_record_id,
				f.id AS faction_id,
				f.name AS faction_name,
				f.faction_status,
				ROW_NUMBER() OVER(PARTITION BY cc.id ORDER BY f.level DESC) AS rn
			FROM character_claim cc
			JOIN character_claim_faction ccf ON cc.id = ccf.character_claim_id
			JOIN factions f ON ccf.faction_id = f.id
			LEFT JOIN claim_record cr ON cr.id = cc.claim_record_id
			WHERE cc.is_claimed IS NOT TRUE
			  AND (f.faction_status != 2
			       OR (? > 0 AND cr.user_id = ?)
			       OR (cr.guest_hash IS NOT NULL AND cr.guest_hash != '' AND cr.guest_hash IN (?, ?, ?)))
		)
		SELECT r.id, r.name, r.faction_id, r.faction_name, r.faction_status, wc.topic_id AS wanted_character_id,
		       cr.id AS claim_record_id, cr.user_id AS claim_author_id, u.username AS claim_author_username, cr.guest_hash AS claim_guest_hash, cr.claim_expiration_date
		FROM RankedFactions r
		LEFT JOIN (
			SELECT character_claim_id,
			       MIN(id) AS id,
			       MIN(wanted_character_status) AS wanted_character_status,
			       MIN(CASE WHEN wanted_character_status = 0 THEN topic_id END) AS topic_id
			FROM wanted_character_base
			GROUP BY character_claim_id
		) wc ON wc.character_claim_id = r.id
		LEFT JOIN claim_record cr ON cr.id = r.claim_record_id AND cr.claim_expiration_date > NOW()
		LEFT JOIN users u ON u.id = cr.user_id
		LEFT JOIN character_base cb ON cb.id = cr.character_id
		WHERE r.rn = 1
		  AND (wc.id IS NULL OR wc.wanted_character_status = 0)
		  AND (r.show_only_with_active_claim = false OR cr.id IS NOT NULL)
		  AND (cr.character_id IS NULL OR cb.character_status = 2)
	`
	claimRows, err := db.Query(claimQuery, userID, userID, guestHashes[0], guestHashes[1], guestHashes[2])
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get character claims: " + err.Error()})
		c.Abort()
		return
	}
	defer claimRows.Close()

	for claimRows.Next() {
		var item Entities.CharacterListItem
		var factionID int
		var factionName string
		var factionStatus Entities.FactionStatus
		var claimExpirationDate *time.Time
		if err := claimRows.Scan(&item.Id, &item.Name, &factionID, &factionName, &factionStatus, &item.WantedCharacterId, &item.ClaimRecordId, &item.ClaimAuthorId, &item.ClaimAuthorUsername, &item.ClaimGuestHash, &claimExpirationDate); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan character claim: " + err.Error()})
			c.Abort()
			return
		}
		item.IsClaim = true
		if claimExpirationDate != nil {
			item.ClaimExpirationDate = Services.LocalizeTime(*claimExpirationDate, userTimezone)
		}
		if _, ok := factionMap[factionID]; !ok {
			// Pending faction only visible to the current user — add it on-the-fly
			factions = append(factions, Entities.Faction{
				Id:            factionID,
				Name:          factionName,
				FactionStatus: factionStatus,
				Characters:    []Entities.CharacterListItem{},
			})
			factionMap[factionID] = &factions[len(factions)-1]
		}
		factionMap[factionID].Characters = append(factionMap[factionID].Characters, item)
	}

	noFaction := Entities.Faction{Id: 0, Name: "No Faction", Characters: []Entities.CharacterListItem{}}

	// Characters with no faction
	noFactionCharRows, err := db.Query(`
		SELECT id, name FROM character_base
		WHERE character_status = 0
		AND id NOT IN (SELECT character_id FROM character_faction)
	`)
	if err == nil {
		defer noFactionCharRows.Close()
		for noFactionCharRows.Next() {
			var item Entities.CharacterListItem
			if err := noFactionCharRows.Scan(&item.Id, &item.Name); err == nil {
				item.IsClaim = false
				noFaction.Characters = append(noFaction.Characters, item)
			}
		}
	}

	// Claims with no faction
	noFactionClaimRows, err := db.Query(`
		SELECT cc.id, cc.name, wc.topic_id
		FROM character_claim cc
		LEFT JOIN (
			SELECT character_claim_id,
			       MIN(id) AS id,
			       MIN(wanted_character_status) AS wanted_character_status,
			       MIN(CASE WHEN wanted_character_status = 0 THEN topic_id END) AS topic_id
			FROM wanted_character_base
			GROUP BY character_claim_id
		) wc ON wc.character_claim_id = cc.id
		LEFT JOIN claim_record cr ON cr.id = cc.claim_record_id
		LEFT JOIN character_base cb ON cb.id = cr.character_id
		WHERE cc.is_claimed IS NOT TRUE
		AND cc.id NOT IN (SELECT character_claim_id FROM character_claim_faction)
		AND (wc.id IS NULL OR wc.wanted_character_status = 0)
		AND (cc.show_only_with_active_claim = false OR (cr.id IS NOT NULL AND cr.claim_expiration_date > NOW()))
		AND (cr.character_id IS NULL OR cb.character_status = 2)
	`)
	if err == nil {
		defer noFactionClaimRows.Close()
		for noFactionClaimRows.Next() {
			var item Entities.CharacterListItem
			if err := noFactionClaimRows.Scan(&item.Id, &item.Name, &item.WantedCharacterId); err == nil {
				item.IsClaim = true
				noFaction.Characters = append(noFaction.Characters, item)
			}
		}
	}

	if len(noFaction.Characters) > 0 {
		factions = append(factions, noFaction)
	}

	c.JSON(http.StatusOK, factions)
}

func GetCharacterAutocomplete(c *gin.Context, db *sql.DB) {
	query := `
		SELECT id, name FROM character_base WHERE name LIKE ? AND character_status = 0 ORDER BY name ASC LIMIT 10
	`
	rows, err := db.Query(query, "%"+c.Param("term")+"%")
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get characters: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()
	var characters []Entities.ShortCharacter
	for rows.Next() {
		var tempCharacter Entities.ShortCharacter
		if err := rows.Scan(&tempCharacter.Id, &tempCharacter.Name); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan character: " + err.Error()})
		}
		characters = append(characters, tempCharacter)
	}
	c.JSON(http.StatusOK, characters)
}

func GetMaskAutocomplete(c *gin.Context, db *sql.DB) {
	query := `
		SELECT cpb.id, cpb.mask_name, cpb.user_id, u.username 
		FROM character_profile_base cpb JOIN users u ON cpb.user_id = u.id 
		WHERE cpb.mask_name LIKE ? AND cpb.is_mask = true ORDER BY cpb.mask_name ASC LIMIT 10
	`
	rows, err := db.Query(query, "%"+c.Param("term")+"%")
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get masks: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()
	var masks []Entities.ShortMask
	for rows.Next() {
		var tempMask Entities.ShortMask
		if err := rows.Scan(&tempMask.Id, &tempMask.MaskName, &tempMask.UserId, &tempMask.UserName); err != nil {
			// Log the error and skip this row
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan mask: " + err.Error()})
			continue
		}
		masks = append(masks, tempMask)
	}
	c.JSON(http.StatusOK, masks)
}

func GetUserCharacters(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		c.Abort()
		return
	}

	query := "SELECT id, name FROM character_base WHERE user_id = ?"
	rows, err := db.Query(query, userID)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get user characters: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	var characters []Entities.ShortCharacter
	for rows.Next() {
		var char Entities.ShortCharacter
		if err := rows.Scan(&char.Id, &char.Name); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan character: " + err.Error()})
			c.Abort()
			return
		}
		characters = append(characters, char)
	}

	c.JSON(http.StatusOK, characters)
}

func GetCharacterProfilesByUser(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		c.Abort()
		return
	}

	// Get IDs of profiles for this user
	query := "SELECT cp.id, cp.character_id, cb.name, cp.avatar FROM character_profile_base cp JOIN character_base cb ON cp.character_id = cb.id WHERE cb.user_id = ?"
	rows, err := db.Query(query, userID)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get character profiles: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	var profiles []Entities.CharacterProfile
	for rows.Next() {
		var id int
		var characterId int
		var name string
		var avatar *string
		if err := rows.Scan(&id, &characterId, &name, &avatar); err != nil {
			continue
		}

		entity, err := Services.GetEntity(int64(id), "character_profile", db)
		if err != nil {
			continue
		}

		if profile, ok := entity.(*Entities.CharacterProfile); ok {
			profile.CharacterId = &characterId
			profile.CharacterName = name
			profile.Avatar = avatar
			profiles = append(profiles, *profile)
		}
	}

	c.JSON(http.StatusOK, profiles)
}

func GetCharacterProfile(c *gin.Context, db *sql.DB) {
	characterID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid character ID"})
		c.Abort()
		return
	}

	// Find profile ID for this character
	var profileID int
	err = db.QueryRow("SELECT id FROM character_profile_base WHERE character_id = ?", characterID).Scan(&profileID)
	if err != nil {
		if err == sql.ErrNoRows {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Character profile not found"})
		} else {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get character profile ID: " + err.Error()})
		}
		c.Abort()
		return
	}

	entity, err := Services.GetEntity(int64(profileID), "character_profile", db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get character profile: " + err.Error()})
		c.Abort()
		return
	}

	if profile, ok := entity.(*Entities.CharacterProfile); ok {
		// Fetch character name from character_base and avatar from character_profile_base
		err := db.QueryRow("SELECT cb.name, cpb.avatar FROM character_base cb JOIN character_profile_base cpb ON cb.id = cpb.character_id WHERE cpb.id = ?", profile.Id).Scan(&profile.CharacterName, &profile.Avatar)
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get character details: " + err.Error()})
			c.Abort()
			return
		}

		// Check CanEdit
		currentUserID := Services.GetUserIdFromContext(c)
		canEdit := false
		if currentUserID != 0 {
			// Fetch subforum ID for permission check
			var subforumID int
			err = db.QueryRow("SELECT subforum_id FROM topics WHERE id = (SELECT topic_id FROM character_base WHERE id = ?)", profile.CharacterId).Scan(&subforumID)
			if err == nil {
				// Check for "Edit others' topic" permission
				permission := fmt.Sprintf("subforum_edit_others_topic:%d", subforumID)
				if hasPerm, err := Services.HasPermission(currentUserID, permission, db); err == nil && hasPerm {
					canEdit = true
				} else {
					// Check if user is the author of the topic
					var authorUserID int
					err = db.QueryRow("SELECT author_user_id FROM topics WHERE id = (SELECT topic_id FROM character_base WHERE id = ?)", profile.CharacterId).Scan(&authorUserID)
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
		profile.CanEdit = &canEdit

		c.JSON(http.StatusOK, profile)
		return
	}

	c.JSON(http.StatusOK, entity)
}

func CharacterProfileUpdate(c *gin.Context, db *sql.DB) {
	characterID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid character ID"})
		c.Abort()
		return
	}

	// Find profile ID for this character
	var profileID int
	err = db.QueryRow("SELECT id FROM character_profile_base WHERE character_id = ?", characterID).Scan(&profileID)
	if err != nil {
		if err == sql.ErrNoRows {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Character profile not found"})
		} else {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get character profile ID: " + err.Error()})
		}
		c.Abort()
		return
	}

	var jsonMap map[string]interface{}
	if err := c.ShouldBindJSON(&jsonMap); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	updatedEntity, err := Services.PatchEntity(int64(profileID), "character_profile", jsonMap, db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update character profile: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, updatedEntity)
}

func GetCharacterProfilesByUserAndTopic(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		c.JSON(http.StatusOK, []Entities.CharacterProfile{})
		return
	}

	topicIDStr := c.Param("topicID")
	topicID, err := strconv.Atoi(topicIDStr)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid topic ID"})
		c.Abort()
		return
	}

	// 1. Get topic type
	var topicType Entities.TopicType
	err = db.QueryRow("SELECT type FROM topics WHERE id = ?", topicID).Scan(&topicType)
	if err != nil {
		if err == sql.ErrNoRows {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Topic not found"})
		} else {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get topic type: " + err.Error()})
		}
		c.Abort()
		return
	}

	profiles := []Entities.CharacterProfile{}

	scanCharacterRows := func(rows *sql.Rows) {
		defer rows.Close()
		for rows.Next() {
			var id, characterId int
			var name string
			var avatar *string
			if err := rows.Scan(&id, &characterId, &name, &avatar); err != nil {
				continue
			}
			entity, err := Services.GetEntity(int64(id), "character_profile", db)
			if err != nil {
				continue
			}
			if profile, ok := entity.(*Entities.CharacterProfile); ok {
				profile.CharacterId = &characterId
				profile.CharacterName = name
				profile.Avatar = avatar
				profiles = append(profiles, *profile)
			}
		}
	}

	appendMaskRows := func(rows *sql.Rows) {
		defer rows.Close()
		for rows.Next() {
			var id int
			if err := rows.Scan(&id); err != nil {
				continue
			}
			entity, err := Services.GetEntity(int64(id), "character_profile", db)
			if err != nil {
				continue
			}
			if profile, ok := entity.(*Entities.CharacterProfile); ok {
				profiles = append(profiles, *profile)
			}
		}
	}

	switch topicType {
	case Entities.CharacterSheetTopic, Entities.WantedCharacterTopic:
		// Only user profile — return empty character profiles list

	case Entities.GeneralTopic:
		// All user's active characters, no masks
		rows, err := db.Query(`
			SELECT cp.id, cp.character_id, cb.name, cp.avatar
			FROM character_profile_base cp
			JOIN character_base cb ON cp.character_id = cb.id
			WHERE cb.user_id = ? AND cb.character_status = 0 AND cp.is_mask IS NOT TRUE
		`, userID)
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get character profiles: " + err.Error()})
			c.Abort()
			return
		}
		scanCharacterRows(rows)

	case Entities.EpisodeTopic:
		var openToEveryone bool
		_ = db.QueryRow("SELECT open_to_everyone FROM episode_base WHERE topic_id = ?", topicID).Scan(&openToEveryone)

		if openToEveryone {
			// All user's characters + all user's masks
			rows, err := db.Query(`
				SELECT cp.id, cp.character_id, cb.name, cp.avatar
				FROM character_profile_base cp
				JOIN character_base cb ON cp.character_id = cb.id
				WHERE cb.user_id = ? AND cb.character_status = 0 AND cp.is_mask IS NOT TRUE
			`, userID)
			if err != nil {
				_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get episode characters: " + err.Error()})
				c.Abort()
				return
			}
			scanCharacterRows(rows)

			maskRows, err := db.Query(`
				SELECT id FROM character_profile_base WHERE user_id = ? AND is_mask = true
			`, userID)
			if err == nil {
				appendMaskRows(maskRows)
			}
		} else {
			// Only characters and masks associated with this episode
			rows, err := db.Query(`
				SELECT cp.id, cp.character_id, cb.name, cp.avatar
				FROM character_profile_base cp
				JOIN character_base cb ON cp.character_id = cb.id
				JOIN episode_character ec ON cb.id = ec.character_id
				JOIN episode_base e ON ec.episode_id = e.id
				WHERE cb.user_id = ? AND e.topic_id = ? AND cp.is_mask IS NOT TRUE
			`, userID, topicID)
			if err != nil {
				_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get episode characters: " + err.Error()})
				c.Abort()
				return
			}
			scanCharacterRows(rows)

			maskRows, err := db.Query(`
				SELECT cpb.id
				FROM character_profile_base cpb
				JOIN episode_mask em ON cpb.id = em.mask_id
				JOIN episode_base e ON em.episode_id = e.id
				WHERE cpb.user_id = ? AND e.topic_id = ? AND cpb.is_mask = true
			`, userID, topicID)
			if err == nil {
				appendMaskRows(maskRows)
			}
		}
	}

	c.JSON(http.StatusOK, profiles)
}

func AcceptCharacter(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid character ID"})
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

	// 1. Change character's status to Active (0)
	var userID int
	var name string
	var avatar *string
	var topicID int
	err = tx.QueryRow("SELECT user_id, name, avatar, topic_id FROM character_base WHERE id = ?", id).Scan(&userID, &name, &avatar, &topicID)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Character not found"})
		c.Abort()
		return
	}

	_, err = tx.Exec("UPDATE character_base SET character_status = 0 WHERE id = ?", id)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update character status"})
		c.Abort()
		return
	}

	// 2. Create a character profile
	profile := Entities.CharacterProfile{
		CharacterId:   &id,
		CharacterName: name,
		Avatar:        avatar,
	}

	_, profileID, err := Services.CreateEntity("character_profile", &profile, tx)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create character profile: " + err.Error()})
		c.Abort()
		return
	}

	var claimRecordId int
	var claimId int
	if err := tx.QueryRow("SELECT id, claim_id FROM claim_record WHERE character_id = ? ORDER BY claim_date DESC LIMIT 1", id).Scan(&claimRecordId, &claimId); err == nil {
		if _, err := tx.Exec("UPDATE character_claim SET is_claimed = true, claim_record_id = ? WHERE id = ?", claimRecordId, claimId); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update character claim: " + err.Error()})
			c.Abort()
			return
		}
		if _, err := tx.Exec("UPDATE wanted_character_base SET is_claimed = true WHERE character_claim_id = ?", claimId); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update wanted character: " + err.Error()})
			c.Abort()
			return
		}
	}

	if err := tx.Commit(); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to commit transaction"})
		c.Abort()
		return
	}

	// 3. Send event to EventBus
	Events.Publish(db, Events.CharacterAccepted, Events.CharacterAcceptedEvent{
		CharacterID:   id,
		CharacterName: name,
		UserID:        userID,
		TopicID:       topicID,
	})

	c.JSON(http.StatusOK, gin.H{"message": "Character accepted", "profile_id": profileID})
}

func GetMask(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid mask ID"})
		c.Abort()
		return
	}

	entity, err := Services.GetEntity(int64(id), "character_profile", db)
	if err != nil {
		if err == sql.ErrNoRows {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Mask not found"})
		} else {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get mask: " + err.Error()})
		}
		c.Abort()
		return
	}

	if profile, ok := entity.(*Entities.CharacterProfile); ok {
		// Masks must have is_mask = true and character_id = null
		var isMask bool
		var avatar *string
		var userID int
		err := db.QueryRow("SELECT is_mask, mask_name, avatar, user_id FROM character_profile_base WHERE id = ?", profile.Id).Scan(&isMask, &profile.MaskName, &avatar, &userID)
		if err != nil || !isMask {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Mask not found"})
			c.Abort()
			return
		}

		if profile.MaskName != nil {
			profile.CharacterName = *profile.MaskName
		}
		profile.Avatar = avatar

		// Check CanEdit (Masks are owned by the creator)
		currentUserID := Services.GetUserIdFromContext(c)
		canEdit := currentUserID != 0 && currentUserID == userID
		profile.CanEdit = &canEdit

		c.JSON(http.StatusOK, profile)
		return
	}

	c.JSON(http.StatusBadRequest, gin.H{"error": "Entity is not a valid character profile"})
}

func CreateMask(c *gin.Context, db *sql.DB) {
	var req struct {
		MaskName     string                               `json:"mask_name" binding:"required"`
		Avatar       *string                              `json:"avatar"`
		CustomFields map[string]Entities.CustomFieldValue `json:"custom_fields"`
	}

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

	isMaskTrue := true
	mask := Entities.CharacterProfile{
		UserId:   &userID,
		MaskName: &req.MaskName,
		Avatar:   req.Avatar,
		IsMask:   &isMaskTrue, // Constraint: Masks always have is_mask = true
	}
	mask.CustomFields.CustomFields = req.CustomFields

	createdEntity, maskID, err := Services.CreateEntity("character_profile", &mask, db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create mask: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": maskID, "entity": createdEntity})
}

func UpdateMask(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid mask ID"})
		c.Abort()
		return
	}

	// 1. Verify Ownership: Only the creator can update the mask
	var ownerID int
	err = db.QueryRow("SELECT user_id FROM character_profile_base WHERE id = ? AND is_mask = true", id).Scan(&ownerID)
	if err != nil {
		if err == sql.ErrNoRows {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Mask not found"})
		} else {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to verify ownership: " + err.Error()})
		}
		c.Abort()
		return
	}

	currentUserID := Services.GetUserIdFromContext(c)
	if currentUserID != ownerID {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "You do not have permission to update this mask"})
		c.Abort()
		return
	}

	// 2. Use a generic map for binding, matching the working CharacterProfileUpdate logic
	var jsonMap map[string]interface{}
	if err := c.ShouldBindJSON(&jsonMap); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	updatedEntity, err := Services.PatchEntity(int64(id), "character_profile", jsonMap, db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update mask: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, updatedEntity)
}

func GetUserMasks(c *gin.Context, db *sql.DB) {
	userIDStr := c.Param("userID")
	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid user ID"})
		c.Abort()
		return
	}

	query := "SELECT id, mask_name, avatar FROM character_profile_base WHERE user_id = ? AND is_mask = true"
	rows, err := db.Query(query, userID)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get user masks: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	currentUserID := Services.GetUserIdFromContext(c)

	var masks []Entities.CharacterProfile = []Entities.CharacterProfile{}
	for rows.Next() {
		var id int
		var maskName *string
		var avatar *string
		if err := rows.Scan(&id, &maskName, &avatar); err != nil {
			continue
		}

		// Fetch the full entity to get custom fields
		entity, err := Services.GetEntity(int64(id), "character_profile", db)
		if err != nil {
			continue
		}

		if profile, ok := entity.(*Entities.CharacterProfile); ok {
			profile.MaskName = maskName
			if maskName != nil {
				profile.CharacterName = *maskName
			}
			profile.Avatar = avatar

			// Check if current user can edit (only if they are the owner)
			canEdit := currentUserID != 0 && currentUserID == userID
			profile.CanEdit = &canEdit

			masks = append(masks, *profile)
		}
	}

	c.JSON(http.StatusOK, masks)
}

func GetCharacterClaims(c *gin.Context, db *sql.DB) {
	factions, err := Services.GetFullFactionTree(db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get faction tree: " + err.Error()})
		c.Abort()
		return
	}

	response := make([]Entities.ClaimFactionResponse, len(factions))
	factionMap := make(map[int]*Entities.ClaimFactionResponse)
	for i, f := range factions {
		response[i] = Entities.ClaimFactionResponse{
			Id:            f.Id,
			Name:          f.Name,
			ParentId:      f.ParentId,
			Level:         f.Level,
			Description:   f.Description,
			Icon:          f.Icon,
			ShowOnProfile: f.ShowOnProfile,
			FactionStatus: f.FactionStatus,
			Claims:        []Entities.CharacterClaim{},
		}
		factionMap[f.Id] = &response[i]
	}

	claimQuery := `
		WITH RankedFactions AS (
			SELECT
				cc.id,
				cc.name,
				cc.description,
				cc.is_claimed,
				cc.claim_record_id,
				cc.can_change_name,
				cc.show_only_with_active_claim,
				f.id AS faction_id,
				ROW_NUMBER() OVER(PARTITION BY cc.id ORDER BY f.level DESC) AS rn
			FROM character_claim cc
			JOIN character_claim_faction ccf ON cc.id = ccf.character_claim_id
			JOIN factions f ON ccf.faction_id = f.id
		)
		SELECT id, name, description, is_claimed, claim_record_id, can_change_name, show_only_with_active_claim, faction_id
		FROM RankedFactions
		WHERE rn = 1
	`
	claimRows, err := db.Query(claimQuery)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get character claims: " + err.Error()})
		c.Abort()
		return
	}
	defer claimRows.Close()

	for claimRows.Next() {
		var claim Entities.CharacterClaim
		var factionID int
		if err := claimRows.Scan(&claim.Id, &claim.Name, &claim.Description, &claim.IsClaimed, &claim.ClaimRecordId, &claim.CanChangeName, &claim.ShowOnlyWithActiveClaim, &factionID); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan character claim: " + err.Error()})
			c.Abort()
			return
		}
		if faction, ok := factionMap[factionID]; ok {
			faction.Claims = append(faction.Claims, claim)
		}
	}

	c.JSON(http.StatusOK, response)
}

type CreateCharacterClaimRequest struct {
	Claim      Entities.CharacterClaim `json:"claim" binding:"required"`
	FactionIDs []int                   `json:"faction_ids" binding:"required"`
}

func CreateCharacterClaim(c *gin.Context, db *sql.DB) {
	var req CreateCharacterClaimRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
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

	res, err := tx.Exec(
		"INSERT INTO character_claim (name, description, is_claimed, can_change_name, show_only_with_active_claim) VALUES (?, ?, ?, ?, ?)",
		req.Claim.Name, req.Claim.Description, req.Claim.IsClaimed, req.Claim.CanChangeName, req.Claim.ShowOnlyWithActiveClaim,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to insert character claim: " + err.Error()})
		c.Abort()
		return
	}

	claimID, err := res.LastInsertId()
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get claim ID"})
		c.Abort()
		return
	}

	for _, factionID := range req.FactionIDs {
		if _, err := tx.Exec("INSERT INTO character_claim_faction (character_claim_id, faction_id) VALUES (?, ?)", claimID, factionID); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: fmt.Sprintf("Failed to insert faction %d: %s", factionID, err.Error())})
			c.Abort()
			return
		}
	}

	if err := tx.Commit(); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to commit transaction"})
		c.Abort()
		return
	}

	req.Claim.Id = int(claimID)
	c.JSON(http.StatusOK, req.Claim)
}

func DeactivateCharacter(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid character ID"})
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

	result, err := tx.Exec("UPDATE character_base SET character_status = ? WHERE id = ?", Entities.InactiveCharacter, id)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to deactivate character: " + err.Error()})
		c.Abort()
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Character not found"})
		c.Abort()
		return
	}

	_, err = tx.Exec("UPDATE topics SET status = ? WHERE id = (SELECT topic_id FROM character_base WHERE id = ?)", Entities.InactiveTopic, id)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to deactivate character topic: " + err.Error()})
		c.Abort()
		return
	}

	_, err = tx.Exec("UPDATE character_profile_base SET is_archived = true WHERE character_id = ?", id)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to archive character profiles: " + err.Error()})
		c.Abort()
		return
	}

	if err := tx.Commit(); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to commit transaction"})
		c.Abort()
		return
	}

	var topicStatus Entities.TopicStatus
	_ = db.QueryRow("SELECT status FROM topics WHERE id = (SELECT topic_id FROM character_base WHERE id = ?)", id).Scan(&topicStatus)

	c.JSON(http.StatusOK, gin.H{
		"character_status": Entities.InactiveCharacter,
		"topic_status":     topicStatus,
	})
}

func DeclineCharacter(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid character ID"})
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

	result, err := tx.Exec("UPDATE character_base SET character_status = ? WHERE id = ?", Entities.DeclinedCharacter, id)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to decline character: " + err.Error()})
		c.Abort()
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Character not found"})
		c.Abort()
		return
	}

	_, err = tx.Exec("UPDATE topics SET status = ? WHERE id = (SELECT topic_id FROM character_base WHERE id = ?)", Entities.InactiveTopic, id)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to deactivate character topic: " + err.Error()})
		c.Abort()
		return
	}

	// Expire the claim record and free up the claim slot
	var claimRecordId int
	var claimId int
	if err := tx.QueryRow("SELECT id, claim_id FROM claim_record WHERE character_id = ? ORDER BY claim_date DESC LIMIT 1", id).Scan(&claimRecordId, &claimId); err == nil {
		if _, err := tx.Exec("UPDATE claim_record SET claim_expiration_date = DATE_SUB(NOW(), INTERVAL 1 MINUTE) WHERE id = ?", claimRecordId); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to expire claim record: " + err.Error()})
			c.Abort()
			return
		}
		if _, err := tx.Exec("UPDATE character_claim SET claim_record_id = NULL WHERE id = ?", claimId); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to reset character claim: " + err.Error()})
			c.Abort()
			return
		}
	}

	if err := tx.Commit(); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to commit transaction"})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"character_status": Entities.DeclinedCharacter})
}

func ActivateCharacter(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid character ID"})
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

	result, err := tx.Exec("UPDATE character_base SET character_status = ? WHERE id = ?", Entities.ActiveCharacter, id)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to activate character: " + err.Error()})
		c.Abort()
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Character not found"})
		c.Abort()
		return
	}

	// Only restore the topic to active if it is currently inactive — never override FullTopic
	_, err = tx.Exec(
		"UPDATE topics SET status = ? WHERE id = (SELECT topic_id FROM character_base WHERE id = ?) AND status = ?",
		Entities.ActiveTopic, id, Entities.InactiveTopic,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to activate character topic: " + err.Error()})
		c.Abort()
		return
	}

	_, err = tx.Exec("UPDATE character_profile_base SET is_archived = false WHERE character_id = ?", id)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to unarchive character profiles: " + err.Error()})
		c.Abort()
		return
	}

	if err := tx.Commit(); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to commit transaction"})
		c.Abort()
		return
	}

	var topicStatus Entities.TopicStatus
	_ = db.QueryRow("SELECT status FROM topics WHERE id = (SELECT topic_id FROM character_base WHERE id = ?)", id).Scan(&topicStatus)

	c.JSON(http.StatusOK, gin.H{
		"character_status": Entities.ActiveCharacter,
		"topic_status":     topicStatus,
	})
}

func CustomFieldList(c *gin.Context, db *sql.DB) {
	machineName := c.Param("machine_name")

	var configJSON string
	err := db.QueryRow("SELECT config FROM custom_field_config WHERE entity_type = 'character'").Scan(&configJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "No character field config found"})
		} else {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to load field config: " + err.Error()})
		}
		c.Abort()
		return
	}

	var fieldConfigs []Entities.CustomFieldConfig
	if err := json.Unmarshal([]byte(configJSON), &fieldConfigs); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to parse field config: " + err.Error()})
		c.Abort()
		return
	}

	var matched *Entities.CustomFieldConfig
	for i := range fieldConfigs {
		if fieldConfigs[i].MachineFieldName == machineName {
			matched = &fieldConfigs[i]
			break
		}
	}
	if matched == nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Custom field not found"})
		c.Abort()
		return
	}
	if matched.FieldType != "string" {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Field is not of type string"})
		c.Abort()
		return
	}

	query := fmt.Sprintf(
		"SELECT DISTINCT `%s` FROM character_flattened WHERE `%s` IS NOT NULL AND `%s` != '' ORDER BY `%s` ASC",
		machineName, machineName, machineName, machineName,
	)
	rows, err := db.Query(query)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to query field values: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	values := []string{}
	for rows.Next() {
		var val string
		if err := rows.Scan(&val); err != nil {
			continue
		}
		values = append(values, val)
	}

	c.JSON(http.StatusOK, gin.H{"human_field_name": matched.HumanFieldName, "values": values})
}
