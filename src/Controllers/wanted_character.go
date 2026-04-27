package Controllers

import (
	"crypto/rand"
	"cuento-backend/src/Entities"
	"cuento-backend/src/Events"
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"database/sql"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type GetWantedCharacterListRequest struct {
	FactionIDs []int `json:"faction_ids"`
	Page       int   `json:"page"`
}

type UpdateWantedCharacterRequest struct {
	Name         string                 `json:"name" binding:"required"`
	CustomFields map[string]interface{} `json:"custom_fields"`
	Factions     []Entities.Faction     `json:"factions"`
}

type CreateWantedCharacterRequest struct {
	SubforumID       int                    `json:"subforum_id" binding:"required"`
	Name             string                 `json:"name" binding:"required"`
	CharacterClaimId *int                   `json:"character_claim_id"`
	CustomFields     map[string]interface{} `json:"custom_fields"`
	Factions         []Entities.Faction     `json:"factions"`
}

func GetWantedCharacterList(c *gin.Context, db *sql.DB) {
	var req GetWantedCharacterListRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	baseWhere := `
		FROM wanted_character_base
		WHERE is_claimed = false AND (is_deleted IS NULL OR is_deleted = false) AND wanted_character_status = 0`

	var args []interface{}
	if len(req.FactionIDs) > 0 {
		placeholders := make([]string, len(req.FactionIDs))
		for i, id := range req.FactionIDs {
			placeholders[i] = "?"
			args = append(args, id)
		}
		baseWhere += " AND character_claim_id IN (SELECT character_claim_id FROM character_claim_faction WHERE faction_id IN (" + strings.Join(placeholders, ",") + "))"
	}

	var totalCount int
	if err := db.QueryRow("SELECT COUNT(*) "+baseWhere, args...).Scan(&totalCount); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to count wanted characters: " + err.Error()})
		c.Abort()
		return
	}

	limit := 20
	page := req.Page
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * limit
	totalPages := (totalCount + limit - 1) / limit

	query := "SELECT id, name, is_claimed, author_user_id, date_created, character_claim_id, is_deleted, topic_id" +
		baseWhere + " ORDER BY date_created DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := db.Query(query, args...)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get wanted characters: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	var list []*Entities.WantedCharacter
	for rows.Next() {
		var wc Entities.WantedCharacter
		if err := rows.Scan(&wc.Id, &wc.Name, &wc.IsClaimed, &wc.AuthorUserId, &wc.DateCreated, &wc.CharacterClaimId, &wc.IsDeleted, &wc.TopicId); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan wanted character: " + err.Error()})
			c.Abort()
			return
		}
		list = append(list, &wc)
	}

	for _, wc := range list {
		if entity, err := Services.GetEntity(int64(wc.Id), "wanted_character", db); err == nil {
			if full, ok := entity.(*Entities.WantedCharacter); ok {
				wc.CustomFields = full.CustomFields
			}
		}
		if wc.CharacterClaimId != nil {
			wc.Factions, _ = Services.GetFactionTreeByCharacterClaim(*wc.CharacterClaimId, db)
			wc.ClaimRecord = fetchActiveClaimRecord(*wc.CharacterClaimId, db)
		} else {
			wc.Factions = []Entities.Faction{}
		}
	}

	if list == nil {
		list = []*Entities.WantedCharacter{}
	}

	c.JSON(http.StatusOK, gin.H{
		"items":       list,
		"total_pages": totalPages,
	})
}

func GetWantedCharacterTreeList(c *gin.Context, db *sql.DB) {
	factions, err := Services.GetFactionTree(db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get faction tree: " + err.Error()})
		c.Abort()
		return
	}

	factionMap := make(map[int]*Entities.Faction)
	for i := range factions {
		factions[i].Characters = []Entities.CharacterListItem{}
		factionMap[factions[i].Id] = &factions[i]
	}

	rows, err := db.Query(`
		WITH RankedFactions AS (
			SELECT
				cc.id,
				cc.name,
				f.id AS faction_id,
				ROW_NUMBER() OVER(PARTITION BY cc.id ORDER BY f.level DESC) AS rn
			FROM character_claim cc
			JOIN character_claim_faction ccf ON cc.id = ccf.character_claim_id
			JOIN factions f ON ccf.faction_id = f.id
			WHERE cc.is_claimed IS NOT TRUE
		)
		SELECT r.id, r.name, r.faction_id, wc.topic_id
		FROM RankedFactions r
		JOIN wanted_character_base wc ON wc.character_claim_id = r.id
		WHERE r.rn = 1
		  AND wc.is_claimed = false
		  AND (wc.is_deleted IS NULL OR wc.is_deleted = false)
		  AND wc.wanted_character_status = 0
	`)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get wanted characters: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	for rows.Next() {
		var item Entities.CharacterListItem
		var factionID int
		if err := rows.Scan(&item.Id, &item.Name, &factionID, &item.WantedCharacterId); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan wanted character: " + err.Error()})
			c.Abort()
			return
		}
		item.IsClaim = true
		if faction, ok := factionMap[factionID]; ok {
			faction.Characters = append(faction.Characters, item)
		}
	}

	keepIDs := make(map[int]bool)
	for _, f := range factions {
		if len(f.Characters) > 0 {
			keepIDs[f.Id] = true
			parentId := f.ParentId
			for parentId != nil {
				if keepIDs[*parentId] {
					break
				}
				keepIDs[*parentId] = true
				if parent, ok := factionMap[*parentId]; ok {
					parentId = parent.ParentId
				} else {
					break
				}
			}
		}
	}

	result := make([]Entities.Faction, 0)
	for _, f := range factions {
		if keepIDs[f.Id] {
			result = append(result, f)
		}
	}

	noFaction := Entities.Faction{Id: 0, Name: "No Faction", Characters: []Entities.CharacterListItem{}}

	noFactionRows, err := db.Query(`
		SELECT cc.id, cc.name, wc.topic_id
		FROM character_claim cc
		JOIN wanted_character_base wc ON wc.character_claim_id = cc.id
		WHERE cc.is_claimed IS NOT TRUE
		AND wc.is_claimed = false
		AND (wc.is_deleted IS NULL OR wc.is_deleted = false)
		AND wc.wanted_character_status = 0
		AND cc.id NOT IN (SELECT character_claim_id FROM character_claim_faction)
	`)
	if err == nil {
		defer noFactionRows.Close()
		for noFactionRows.Next() {
			var item Entities.CharacterListItem
			if err := noFactionRows.Scan(&item.Id, &item.Name, &item.WantedCharacterId); err == nil {
				item.IsClaim = true
				noFaction.Characters = append(noFaction.Characters, item)
			}
		}
	}

	if len(noFaction.Characters) > 0 {
		result = append(result, noFaction)
	}

	c.JSON(http.StatusOK, result)
}

func GetWantedCharacter(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid wanted character ID"})
		c.Abort()
		return
	}

	entity, err := Services.GetEntity(int64(id), "wanted_character", db)
	if err != nil {
		if err == sql.ErrNoRows {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Wanted character not found"})
		} else {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get wanted character: " + err.Error()})
		}
		c.Abort()
		return
	}

	if wc, ok := entity.(*Entities.WantedCharacter); ok {
		if wc.CharacterClaimId != nil {
			wc.Factions, _ = Services.GetFactionTreeByCharacterClaim(*wc.CharacterClaimId, db)
			wc.ClaimRecord = fetchActiveClaimRecord(*wc.CharacterClaimId, db)
		} else {
			wc.Factions = []Entities.Faction{}
		}
	}

	c.JSON(http.StatusOK, entity)
}

func CreateWantedCharacter(c *gin.Context, db *sql.DB) {
	var req CreateWantedCharacterRequest
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

	res, err := tx.Exec(
		"INSERT INTO topics (subforum_id, name, author_user_id, date_created, date_last_post, status, type, post_number, last_post_author_user_id) VALUES (?, ?, ?, NOW(), NOW(), 0, ?, 0, ?)",
		req.SubforumID, req.Name, userID, Entities.WantedCharacterTopic, userID,
	)
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

	cfMap := make(map[string]Entities.CustomFieldValue)
	for k, v := range req.CustomFields {
		cfMap[k] = Entities.CustomFieldValue{Content: v}
	}

	wantedCharacter := Entities.WantedCharacter{
		Name:             req.Name,
		IsClaimed:        false,
		AuthorUserId:     userID,
		DateCreated:      time.Now(),
		CharacterClaimId: req.CharacterClaimId,
		TopicId:          int(topicID),
		CustomFields: Entities.CustomFieldEntity{
			CustomFields: cfMap,
		},
	}

	_, wantedCharacterID, err := Services.CreateEntity("wanted_character", &wantedCharacter, tx)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create wanted character entity: " + err.Error()})
		c.Abort()
		return
	}

	var claimID int64
	if req.CharacterClaimId != nil {
		claimID = int64(*req.CharacterClaimId)
	} else {
		claimRes, err := tx.Exec(
			"INSERT INTO character_claim (name, description, is_claimed, can_change_name) VALUES (?, NULL, false, false)",
			req.Name,
		)
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create character claim: " + err.Error()})
			c.Abort()
			return
		}
		claimID, err = claimRes.LastInsertId()
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get character claim ID"})
			c.Abort()
			return
		}

		if _, err := tx.Exec("UPDATE wanted_character_base SET character_claim_id = ? WHERE id = ?", claimID, wantedCharacterID); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to link character claim to wanted character: " + err.Error()})
			c.Abort()
			return
		}
	}

	for _, faction := range req.Factions {
		if _, err := tx.Exec("INSERT INTO character_claim_faction (character_claim_id, faction_id) VALUES (?, ?)", claimID, faction.Id); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: fmt.Sprintf("Failed to insert faction %d into character claim: %s", faction.Id, err.Error())})
			c.Abort()
			return
		}
	}

	if err := tx.Commit(); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to commit transaction"})
		c.Abort()
		return
	}

	Events.Publish(db, Events.WantedCharacterCreated, Events.WantedCharacterCreatedEvent{
		WantedCharacterID: wantedCharacterID,
		SubforumID:        req.SubforumID,
		TopicID:           topicID,
		TopicName:         req.Name,
		AuthorUserID:      userID,
	})

	c.JSON(http.StatusCreated, gin.H{"wanted_character_id": wantedCharacterID, "topic_id": topicID})
}

func UpdateWantedCharacter(c *gin.Context, db *sql.DB) {
	wantedCharacterID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid wanted character ID"})
		c.Abort()
		return
	}

	var req UpdateWantedCharacterRequest
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

	var topicID int
	var authorUserID int
	var subforumID int
	err = db.QueryRow(`
		SELECT w.topic_id, t.author_user_id, t.subforum_id
		FROM wanted_character_base w
		JOIN topics t ON w.topic_id = t.id
		WHERE w.id = ?
	`, wantedCharacterID).Scan(&topicID, &authorUserID, &subforumID)
	if err != nil {
		if err == sql.ErrNoRows {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Wanted character not found"})
		} else {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch wanted character details: " + err.Error()})
		}
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
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "You do not have permission to edit this wanted character"})
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

	_, err = tx.Exec("UPDATE topics SET name = ? WHERE id = ?", req.Name, topicID)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update topic name: " + err.Error()})
		c.Abort()
		return
	}

	_, err = tx.Exec("UPDATE character_claim SET name = ? WHERE id = (SELECT character_claim_id FROM wanted_character_base WHERE id = ?)", req.Name, wantedCharacterID)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update character claim name: " + err.Error()})
		c.Abort()
		return
	}

	updates := map[string]interface{}{
		"name":          req.Name,
		"custom_fields": req.CustomFields,
	}
	_, err = Services.PatchEntity(int64(wantedCharacterID), "wanted_character", updates, tx)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update wanted character entity: " + err.Error()})
		c.Abort()
		return
	}

	if req.Factions != nil {
		var claimID int
		err = tx.QueryRow("SELECT character_claim_id FROM wanted_character_base WHERE id = ?", wantedCharacterID).Scan(&claimID)
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get character claim ID: " + err.Error()})
			c.Abort()
			return
		}

		if _, err = tx.Exec("DELETE FROM character_claim_faction WHERE character_claim_id = ?", claimID); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to clear factions: " + err.Error()})
			c.Abort()
			return
		}

		for _, faction := range req.Factions {
			if _, err = tx.Exec("INSERT INTO character_claim_faction (character_claim_id, faction_id) VALUES (?, ?)", claimID, faction.Id); err != nil {
				_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: fmt.Sprintf("Failed to insert faction %d: %s", faction.Id, err.Error())})
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

	updatedEntity, err := Services.GetEntity(int64(wantedCharacterID), "wanted_character", db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch updated wanted character: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, updatedEntity)
}

func DeactivateWantedCharacter(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid wanted character ID"})
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

	result, err := tx.Exec("UPDATE wanted_character_base SET wanted_character_status = ? WHERE id = ?", Entities.InactiveWantedCharacter, id)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to deactivate wanted character: " + err.Error()})
		c.Abort()
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Wanted character not found"})
		c.Abort()
		return
	}

	_, err = tx.Exec("UPDATE topics SET status = ? WHERE id = (SELECT topic_id FROM wanted_character_base WHERE id = ?)", Entities.InactiveTopic, id)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to deactivate wanted character topic: " + err.Error()})
		c.Abort()
		return
	}

	if err := tx.Commit(); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to commit transaction"})
		c.Abort()
		return
	}

	var topicStatus Entities.TopicStatus
	_ = db.QueryRow("SELECT status FROM topics WHERE id = (SELECT topic_id FROM wanted_character_base WHERE id = ?)", id).Scan(&topicStatus)

	c.JSON(http.StatusOK, gin.H{
		"wanted_character_status": Entities.InactiveWantedCharacter,
		"topic_status":            topicStatus,
	})
}

func ActivateWantedCharacter(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid wanted character ID"})
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

	result, err := tx.Exec("UPDATE wanted_character_base SET wanted_character_status = ? WHERE id = ?", Entities.ActiveWantedCharacter, id)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to activate wanted character: " + err.Error()})
		c.Abort()
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Wanted character not found"})
		c.Abort()
		return
	}

	_, err = tx.Exec(
		"UPDATE topics SET status = ? WHERE id = (SELECT topic_id FROM wanted_character_base WHERE id = ?) AND status = ?",
		Entities.ActiveTopic, id, Entities.InactiveTopic,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to activate wanted character topic: " + err.Error()})
		c.Abort()
		return
	}

	if err := tx.Commit(); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to commit transaction"})
		c.Abort()
		return
	}

	var topicStatus Entities.TopicStatus
	_ = db.QueryRow("SELECT status FROM topics WHERE id = (SELECT topic_id FROM wanted_character_base WHERE id = ?)", id).Scan(&topicStatus)

	c.JSON(http.StatusOK, gin.H{
		"wanted_character_status": Entities.ActiveWantedCharacter,
		"topic_status":            topicStatus,
	})
}

type ClaimAutocompleteItem struct {
	Id                  int        `json:"id"`
	Name                string     `json:"name"`
	IsClaimed           *bool      `json:"is_claimed"`
	ClaimExpirationDate *time.Time `json:"claim_expiration_date"`
	UserId              *int       `json:"user_id"`
	GuestHash           *string    `json:"guest_hash"`
}

func scanClaimAutocompleteRows(c *gin.Context, rows *sql.Rows, errMsg string) []ClaimAutocompleteItem {
	var results []ClaimAutocompleteItem
	for rows.Next() {
		var guestHash *string
		var claimDate *time.Time
		item := ClaimAutocompleteItem{}
		if err := rows.Scan(&item.Id, &item.Name, &item.UserId, &guestHash, &claimDate); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: errMsg + err.Error()})
			continue
		}
		if claimDate != nil {
			isClaimed := true
			item.IsClaimed = &isClaimed
			item.ClaimExpirationDate = claimDate
			item.GuestHash = guestHash
		}
		results = append(results, item)
	}
	return results
}

func GetWantedCharacterAutocomplete(c *gin.Context, db *sql.DB) {
	query := `
		SELECT wcb.id, wcb.name, cr.user_id, cr.guest_hash, cr.claim_expiration_date
		FROM wanted_character_base wcb
		LEFT JOIN character_claim cc ON cc.id = wcb.character_claim_id
		LEFT JOIN claim_record cr ON cr.claim_id = cc.id
			AND cr.claim_expiration_date > NOW()
			AND cr.id = (SELECT MAX(id) FROM claim_record WHERE claim_id = cc.id AND claim_expiration_date > NOW())
		WHERE wcb.name LIKE ? AND (wcb.is_deleted IS NULL OR wcb.is_deleted = false) AND wcb.wanted_character_status = 0
		ORDER BY wcb.name ASC LIMIT 10
	`
	rows, err := db.Query(query, "%"+c.Param("term")+"%")
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get wanted characters: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()
	c.JSON(http.StatusOK, scanClaimAutocompleteRows(c, rows, "Failed to scan wanted character: "))
}

func GetClaimAutocomplete(c *gin.Context, db *sql.DB) {
	query := `
		SELECT cc.id, cc.name, cr.user_id, cr.guest_hash, cr.claim_expiration_date
		FROM character_claim cc
		LEFT JOIN wanted_character_base wcb ON wcb.character_claim_id = cc.id
		LEFT JOIN claim_record cr ON cr.claim_id = cc.id
			AND cr.claim_expiration_date > NOW()
			AND cr.id = (SELECT MAX(id) FROM claim_record WHERE claim_id = cc.id AND claim_expiration_date > NOW())
		WHERE cc.name LIKE ? AND wcb.id IS NULL AND cc.is_claimed IS NOT TRUE AND cr.id IS NULL
		ORDER BY cc.name ASC LIMIT 10
	`
	rows, err := db.Query(query, "%"+c.Param("term")+"%")
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get claims: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()
	c.JSON(http.StatusOK, scanClaimAutocompleteRows(c, rows, "Failed to scan claim: "))
}

func fetchActiveClaimRecord(characterClaimId int, db *sql.DB) *Entities.ClaimRecord {
	var cr Entities.ClaimRecord
	err := db.QueryRow(`
		SELECT cr.id, cr.claim_id, cr.user_id, cr.guest_hash, cr.is_guest, cr.claim_date, cr.claim_expiration_date, cr.character_id, cr.claim_created_with_character_sheet, u.id, u.username
		FROM claim_record cr
		LEFT JOIN users u ON u.id = cr.user_id
		WHERE cr.claim_id = ? AND cr.claim_expiration_date > NOW()
		ORDER BY cr.claim_date DESC
		LIMIT 1
	`, characterClaimId).Scan(&cr.Id, &cr.ClaimId, &cr.UserId, &cr.GuestHash, &cr.IsGuest, &cr.ClaimDate, &cr.ClaimExpirationDate, &cr.CharacterId, &cr.ClaimCreatedWithCharacterSheet, &cr.ClaimAuthorId, &cr.ClaimAuthorUsername)
	if err != nil {
		return nil
	}
	return &cr
}

type RevokeClaimRequest struct {
	ClaimRecordId int `json:"claim_record_id" binding:"required"`
}

func RevokeClaim(c *gin.Context, db *sql.DB) {
	var req RevokeClaimRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	var recordUserID *int
	var guestHash *string
	if err := db.QueryRow("SELECT user_id, guest_hash FROM claim_record WHERE id = ?", req.ClaimRecordId).Scan(&recordUserID, &guestHash); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Claim record not found"})
		c.Abort()
		return
	}

	userID := Services.GetUserIdFromContext(c)
	authorized := false

	if userID != 0 {
		authorized = recordUserID != nil && *recordUserID == userID
	} else if guestHash != nil {
		for i := 1; i <= 3; i++ {
			if hash, err := c.Cookie(fmt.Sprintf("claim_hash_%d", i)); err == nil && hash == *guestHash {
				authorized = true
				break
			}
		}
	}

	if !authorized {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "You are not authorized to revoke this claim"})
		c.Abort()
		return
	}

	_, err := db.Exec("UPDATE claim_record SET claim_expiration_date = NOW() - INTERVAL 1 SECOND WHERE id = ?", req.ClaimRecordId)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to revoke claim: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Claim revoked"})
}

type CreateClaimRecordRequest struct {
	ClaimType string `json:"claim_type" binding:"required"`
	ClaimId   int    `json:"claim_id" binding:"required"`
}

func generateGuestHash() string {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	result := make([]byte, 6)
	for i := range result {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		result[i] = chars[n.Int64()]
	}
	return string(result)
}

func CreateClaimRecord(c *gin.Context, db *sql.DB) {
	var req CreateClaimRecordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	userID := Services.GetUserIdFromContext(c)

	claimId := req.ClaimId
	if req.ClaimType == "wanted_character" {
		var characterClaimId *int
		if err := db.QueryRow("SELECT character_claim_id FROM wanted_character_base WHERE id = ?", claimId).Scan(&characterClaimId); err != nil || characterClaimId == nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Failed to resolve wanted character claim"})
			c.Abort()
			return
		}
		claimId = *characterClaimId
	}

	isGuest := userID == 0
	expirationDays := 5
	if isGuest {
		expirationDays = 3
	}
	expirationDate := time.Now().AddDate(0, 0, expirationDays)

	var guestHash *string
	if isGuest {
		h := generateGuestHash()
		guestHash = &h
	}

	var userIdParam *int
	if !isGuest {
		userIdParam = &userID
	}

	_, err := db.Exec(
		"INSERT INTO claim_record (claim_id, user_id, guest_hash, is_guest, claim_date, claim_expiration_date) VALUES (?, ?, ?, ?, NOW(), ?)",
		claimId, userIdParam, guestHash, isGuest, expirationDate,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create claim record: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"guest_hash":            guestHash,
		"claim_expiration_date": expirationDate,
	})
}

type CreateNewRoleClaimRequest struct {
	CharacterName string `json:"character_name" binding:"required"`
	FactionId     int    `json:"faction_id" binding:"required"`
}

func CreateNewRoleClaim(c *gin.Context, db *sql.DB) {
	var req CreateNewRoleClaimRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	var factionExists int
	if err := db.QueryRow("SELECT id FROM factions WHERE id = ?", req.FactionId).Scan(&factionExists); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Faction not found"})
		c.Abort()
		return
	}

	userID := Services.GetUserIdFromContext(c)
	isGuest := userID == 0
	expirationDays := 5
	if isGuest {
		expirationDays = 3
	}
	expirationDate := time.Now().AddDate(0, 0, expirationDays)

	var guestHash *string
	var cookieSlot string
	if isGuest {
		for i := 1; i <= 3; i++ {
			slotName := fmt.Sprintf("claim_hash_%d", i)
			if _, err := c.Cookie(slotName); err != nil {
				cookieSlot = slotName
				break
			}
		}
		if cookieSlot == "" {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Maximum number of guest claims reached"})
			c.Abort()
			return
		}
		h := generateGuestHash()
		guestHash = &h
	}

	claimRes, err := db.Exec(
		"INSERT INTO character_claim (name, show_only_with_active_claim) VALUES (?, true)",
		req.CharacterName,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create claim: " + err.Error()})
		c.Abort()
		return
	}
	claimId, _ := claimRes.LastInsertId()

	_, err = db.Exec(
		"INSERT INTO character_claim_faction (character_claim_id, faction_id) VALUES (?, ?)",
		claimId, req.FactionId,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to link faction to claim: " + err.Error()})
		c.Abort()
		return
	}

	var userIdParam *int
	if !isGuest {
		userIdParam = &userID
	}

	recordRes, err := db.Exec(
		"INSERT INTO claim_record (claim_id, user_id, guest_hash, is_guest, claim_date, claim_expiration_date) VALUES (?, ?, ?, ?, NOW(), ?)",
		claimId, userIdParam, guestHash, isGuest, expirationDate,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create claim record: " + err.Error()})
		c.Abort()
		return
	}
	recordId, _ := recordRes.LastInsertId()

	_, err = db.Exec("UPDATE character_claim SET claim_record_id = ? WHERE id = ?", recordId, claimId)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update claim record reference: " + err.Error()})
		c.Abort()
		return
	}

	if isGuest && guestHash != nil {
		c.SetCookie(cookieSlot, *guestHash, expirationDays*24*60*60, "/", "", false, true)
	}

	c.JSON(http.StatusOK, gin.H{
		"claim_id":              claimId,
		"claim_record_id":       recordId,
		"guest_hash":            guestHash,
		"claim_expiration_date": expirationDate,
	})
}
