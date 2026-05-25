package Controllers

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Middlewares"
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

func GetFactionSettings(c *gin.Context, db *sql.DB) {
	rows, err := db.Query(`
		SELECT fs.id, fs.level, fs.human_name, fs.parent_faction_id,
		       f.id, f.name, f.level
		FROM faction_settings fs
		LEFT JOIN factions f ON fs.parent_faction_id = f.id
		ORDER BY fs.level, fs.id
	`)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get faction settings: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	var settings []Entities.FactionSetting
	for rows.Next() {
		var s Entities.FactionSetting
		var parentId sql.NullInt64
		var parentObjId sql.NullInt64
		var parentName sql.NullString
		var parentLevel sql.NullInt64

		if err := rows.Scan(&s.Id, &s.Level, &s.HumanName, &parentId,
			&parentObjId, &parentName, &parentLevel); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan faction setting: " + err.Error()})
			c.Abort()
			return
		}

		if parentId.Valid {
			v := int(parentId.Int64)
			s.ParentFactionId = &v
		}
		if parentObjId.Valid {
			s.Parent = &Entities.FactionInfo{
				Id:    int(parentObjId.Int64),
				Name:  parentName.String,
				Level: int(parentLevel.Int64),
			}
		}

		settings = append(settings, s)
	}

	if settings == nil {
		settings = []Entities.FactionSetting{}
	}

	c.JSON(http.StatusOK, settings)
}

func factionSettingExists(db *sql.DB, level int, parentFactionId *int, excludeId *int) (bool, error) {
	var query string
	var args []interface{}
	if parentFactionId == nil {
		query = "SELECT EXISTS(SELECT 1 FROM faction_settings WHERE level = ? AND parent_faction_id IS NULL"
		args = []interface{}{level}
	} else {
		query = "SELECT EXISTS(SELECT 1 FROM faction_settings WHERE level = ? AND parent_faction_id = ?"
		args = []interface{}{level, *parentFactionId}
	}
	if excludeId != nil {
		query += " AND id != ?"
		args = append(args, *excludeId)
	}
	query += ")"
	var exists bool
	err := db.QueryRow(query, args...).Scan(&exists)
	return exists, err
}

func CreateFactionSetting(c *gin.Context, db *sql.DB) {
	var req Entities.FactionSetting
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	exists, err := factionSettingExists(db, req.Level, req.ParentFactionId, nil)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to check uniqueness: " + err.Error()})
		c.Abort()
		return
	}
	if exists {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusConflict, Message: "A faction setting with this level and parent already exists"})
		c.Abort()
		return
	}

	res, err := db.Exec(
		"INSERT INTO faction_settings (level, human_name, parent_faction_id) VALUES (?, ?, ?)",
		req.Level, req.HumanName, req.ParentFactionId,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create faction setting: " + err.Error()})
		c.Abort()
		return
	}

	id, _ := res.LastInsertId()
	req.Id = int(id)
	c.JSON(http.StatusOK, req)
}

type FactionSettingUpdateRequest struct {
	Level           *int    `json:"level"`
	HumanName       *string `json:"human_name"`
	ParentFactionId *int    `json:"parent_faction_id"`
}

func UpdateFactionSetting(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid id"})
		c.Abort()
		return
	}

	var req FactionSettingUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	// Fetch current values to resolve effective level/parent after partial update
	var currentLevel int
	var currentParentId sql.NullInt64
	if err := db.QueryRow("SELECT level, parent_faction_id FROM faction_settings WHERE id = ?", id).
		Scan(&currentLevel, &currentParentId); err != nil {
		if err == sql.ErrNoRows {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Faction setting not found"})
		} else {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch faction setting: " + err.Error()})
		}
		c.Abort()
		return
	}

	effectiveLevel := currentLevel
	if req.Level != nil {
		effectiveLevel = *req.Level
	}

	var effectiveParentId *int
	if currentParentId.Valid {
		v := int(currentParentId.Int64)
		effectiveParentId = &v
	}
	if req.ParentFactionId != nil {
		effectiveParentId = req.ParentFactionId
	}

	if req.Level != nil || req.ParentFactionId != nil {
		exists, err := factionSettingExists(db, effectiveLevel, effectiveParentId, &id)
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to check uniqueness: " + err.Error()})
			c.Abort()
			return
		}
		if exists {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusConflict, Message: "A faction setting with this level and parent already exists"})
			c.Abort()
			return
		}
	}

	setClauses := []string{}
	args := []interface{}{}

	if req.Level != nil {
		setClauses = append(setClauses, "level = ?")
		args = append(args, *req.Level)
	}
	if req.HumanName != nil {
		setClauses = append(setClauses, "human_name = ?")
		args = append(args, *req.HumanName)
	}
	if req.ParentFactionId != nil {
		setClauses = append(setClauses, "parent_faction_id = ?")
		args = append(args, *req.ParentFactionId)
	}

	if len(setClauses) == 0 {
		c.JSON(http.StatusOK, gin.H{"message": "Faction setting updated"})
		return
	}

	args = append(args, id)
	res, err := db.Exec("UPDATE faction_settings SET "+strings.Join(setClauses, ", ")+" WHERE id = ?", args...)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update faction setting: " + err.Error()})
		c.Abort()
		return
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Faction setting not found"})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Faction setting updated"})
}

func DeleteFactionSetting(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid id"})
		c.Abort()
		return
	}

	var exists bool
	if err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM faction_settings WHERE id = ?)", id).Scan(&exists); err != nil || !exists {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Faction setting not found"})
		c.Abort()
		return
	}

	var childCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM faction_settings WHERE parent_faction_id = ?", id).Scan(&childCount); err != nil || childCount > 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusConflict, Message: "Cannot delete a faction setting that has children"})
		c.Abort()
		return
	}

	if _, err := db.Exec("DELETE FROM faction_settings WHERE id = ?", id); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to delete faction setting: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Faction setting deleted"})
}
