package Controllers

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

type FactionUpdateRequest struct {
	Name          *string                 `json:"name"`
	ParentId      *int                    `json:"parent_id"`
	Level         *int                    `json:"level"`
	Description   *string                 `json:"description"`
	Icon          *string                 `json:"icon"`
	ShowOnProfile *bool                   `json:"show_on_profile"`
	FactionStatus *Entities.FactionStatus `json:"faction_status"`
}

func GetFactionChildren(c *gin.Context, db *sql.DB) {
	parentIDStr := c.Param("parent_id")
	includePending := c.Query("include_pending") == "true"

	statuses := []interface{}{Entities.FactionActive}
	if includePending {
		statuses = append(statuses, Entities.FactionPending)
	}
	placeholders := strings.Repeat("?,", len(statuses)-1) + "?"

	var rows *sql.Rows
	var err error

	if parentIDStr == "" || parentIDStr == "0" {
		args := append([]interface{}(nil), statuses...)
		rows, err = db.Query("SELECT id, name, parent_id, level, description, icon, show_on_profile, faction_status FROM factions WHERE parent_id IS NULL AND faction_status IN ("+placeholders+") ORDER BY name", args...)
	} else {
		parentID, convErr := strconv.Atoi(parentIDStr)
		if convErr != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid parent_id"})
			c.Abort()
			return
		}
		args := append([]interface{}{parentID}, statuses...)
		rows, err = db.Query("SELECT id, name, parent_id, level, description, icon, show_on_profile, faction_status FROM factions WHERE parent_id = ? AND faction_status IN ("+placeholders+") ORDER BY name", args...)
	}

	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get factions: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	var factions []Entities.Faction
	for rows.Next() {
		var f Entities.Faction
		if err := rows.Scan(&f.Id, &f.Name, &f.ParentId, &f.Level, &f.Description, &f.Icon, &f.ShowOnProfile, &f.FactionStatus); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan faction: " + err.Error()})
			c.Abort()
			return
		}
		factions = append(factions, f)
	}

	if factions == nil {
		factions = []Entities.Faction{}
	}

	c.JSON(http.StatusOK, factions)
}

func GetFactionTree(c *gin.Context, db *sql.DB) {
	factions, err := Services.GetFactionTree(db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get faction tree: " + err.Error()})
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, factions)
}

func GetActiveFactionTree(c *gin.Context, db *sql.DB) {
	factions, err := Services.GetActiveFactionTree(db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get faction tree: " + err.Error()})
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, factions)
}

func GetWantedFactionTree(c *gin.Context, db *sql.DB) {
	factions, err := Services.GetWantedFactionTree(db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get wanted faction tree: " + err.Error()})
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, factions)
}

func UpdateFactionById(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid id"})
		c.Abort()
		return
	}

	var req FactionUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	setClauses := []string{}
	args := []interface{}{}

	if req.Name != nil {
		setClauses = append(setClauses, "name = ?")
		args = append(args, *req.Name)
	}
	if req.ParentId != nil {
		setClauses = append(setClauses, "parent_id = ?")
		args = append(args, *req.ParentId)
	}
	if req.Level != nil {
		setClauses = append(setClauses, "level = ?")
		args = append(args, *req.Level)
	}
	if req.Description != nil {
		setClauses = append(setClauses, "description = ?")
		args = append(args, *req.Description)
	}
	if req.Icon != nil {
		setClauses = append(setClauses, "icon = ?")
		args = append(args, *req.Icon)
	}
	if req.ShowOnProfile != nil {
		setClauses = append(setClauses, "show_on_profile = ?")
		args = append(args, *req.ShowOnProfile)
	}
	if req.FactionStatus != nil {
		setClauses = append(setClauses, "faction_status = ?")
		args = append(args, *req.FactionStatus)
	}

	if len(setClauses) == 0 {
		c.JSON(http.StatusOK, gin.H{"message": "Faction updated"})
		return
	}

	args = append(args, id)
	query := "UPDATE factions SET " + strings.Join(setClauses, ", ") + " WHERE id = ?"
	res, err := db.Exec(query, args...)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update faction: " + err.Error()})
		c.Abort()
		return
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Faction not found"})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Faction updated"})
}

func CreateFaction(c *gin.Context, db *sql.DB) {
	var req Entities.Faction
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	id, err := Services.CreateFaction(req, db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create faction: " + err.Error()})
		c.Abort()
		return
	}

	req.Id = int(id)
	c.JSON(http.StatusOK, req)
}

type CreatePendingFactionRequest struct {
	Name string `json:"name" binding:"required"`
}

func CreatePendingFaction(c *gin.Context, db *sql.DB) {
	var req CreatePendingFactionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	var existingId int
	err := db.QueryRow("SELECT id FROM factions WHERE LOWER(name) = LOWER(?)", req.Name).Scan(&existingId)
	if err == nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusConflict, Message: "A faction with this name already exists"})
		c.Abort()
		return
	}
	if err != sql.ErrNoRows {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to check faction name: " + err.Error()})
		c.Abort()
		return
	}

	res, err := db.Exec(
		"INSERT INTO factions (name, faction_status, level, show_on_profile) VALUES (?, ?, 0, false)",
		req.Name, Entities.FactionPending,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create faction: " + err.Error()})
		c.Abort()
		return
	}

	id, _ := res.LastInsertId()
	c.JSON(http.StatusOK, gin.H{"id": id, "name": req.Name, "faction_status": Entities.FactionPending})
}

func DeleteFaction(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid id"})
		c.Abort()
		return
	}

	var exists bool
	if err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM factions WHERE id = ?)", id).Scan(&exists); err != nil || !exists {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Faction not found"})
		c.Abort()
		return
	}

	var childCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM factions WHERE parent_id = ?", id).Scan(&childCount); err != nil || childCount > 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusConflict, Message: "Cannot delete a faction that has children"})
		c.Abort()
		return
	}

	var characterCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM character_faction WHERE faction_id = ?", id).Scan(&characterCount); err != nil || characterCount > 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusConflict, Message: "Cannot delete a faction that has characters"})
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

	for _, table := range []string{"character_faction", "character_claim_faction", "wanted_character_faction"} {
		if _, err := tx.Exec("DELETE FROM "+table+" WHERE faction_id = ?", id); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to remove faction references: " + err.Error()})
			c.Abort()
			return
		}
	}

	if _, err := tx.Exec("DELETE FROM factions WHERE id = ?", id); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to delete faction: " + err.Error()})
		c.Abort()
		return
	}

	if err := tx.Commit(); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to commit transaction"})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Faction deleted"})
}

func GetPendingFactions(c *gin.Context, db *sql.DB) {
	rows, err := db.Query("SELECT id, name, parent_id, level, description, icon, show_on_profile, faction_status FROM factions WHERE faction_status = ? ORDER BY name", Entities.FactionPending)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get pending factions: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	var factions []Entities.Faction
	for rows.Next() {
		var f Entities.Faction
		if err := rows.Scan(&f.Id, &f.Name, &f.ParentId, &f.Level, &f.Description, &f.Icon, &f.ShowOnProfile, &f.FactionStatus); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan faction: " + err.Error()})
			c.Abort()
			return
		}
		factions = append(factions, f)
	}

	if factions == nil {
		factions = []Entities.Faction{}
	}

	c.JSON(http.StatusOK, factions)
}
