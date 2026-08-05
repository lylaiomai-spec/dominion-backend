package Controllers

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/MCP"
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
)

func GetGlobalSettings(c *gin.Context, db *sql.DB) {
	rows, err := db.Query("SELECT setting_name, setting_value, needs_superuser FROM global_settings")
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get settings: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	isSuperuser := Services.IsSuperuser(c)
	var settings []Entities.Setting
	for rows.Next() {
		var s Entities.Setting
		var value sql.NullString
		var needsSuperuser bool
		if err := rows.Scan(&s.SettingName, &value, &needsSuperuser); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan setting: " + err.Error()})
			c.Abort()
			return
		}
		if needsSuperuser && !isSuperuser {
			continue
		}
		s.SettingValue = value.String
		settings = append(settings, s)
	}

	if settings == nil {
		settings = []Entities.Setting{}
	}

	c.JSON(http.StatusOK, settings)
}

func UpdateGlobalSettings(c *gin.Context, db *sql.DB) {
	var req []Entities.Setting
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	isSuperuser := Services.IsSuperuser(c)
	if !isSuperuser {
		// Check whether any requested setting requires superuser
		names := make([]interface{}, len(req))
		for i, s := range req {
			names[i] = s.SettingName
		}
		placeholders := ""
		for i := range names {
			if i > 0 {
				placeholders += ","
			}
			placeholders += "?"
		}
		var restricted string
		err := db.QueryRow(
			"SELECT setting_name FROM global_settings WHERE needs_superuser = 1 AND setting_name IN ("+placeholders+") LIMIT 1",
			names...,
		).Scan(&restricted)
		if err == nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "You do not have permission to change setting: " + restricted})
			c.Abort()
			return
		}
	}

	tx, err := db.Begin()
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to start transaction"})
		c.Abort()
		return
	}
	defer tx.Rollback()

	for _, s := range req {
		_, err := tx.Exec("UPDATE global_settings SET setting_value = ? WHERE setting_name = ?", s.SettingValue, s.SettingName)
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update setting: " + err.Error()})
			c.Abort()
			return
		}
	}

	if err := tx.Commit(); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to commit transaction"})
		c.Abort()
		return
	}

	aiSettings := map[string]bool{"ai_api_key": true, "ai_name": true, "ai_model": true}
	for _, s := range req {
		if aiSettings[s.SettingName] {
			MCP.ReinitializeAgent(db)
			break
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "Settings updated"})
}
