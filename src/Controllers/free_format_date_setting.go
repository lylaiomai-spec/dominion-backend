package Controllers

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Middlewares"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func ListFreeFormatDateSettingOptions(c *gin.Context, db *sql.DB) {
	rows, err := db.Query("SELECT id, name FROM free_format_date_settings ORDER BY name ASC")
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to list options: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	type Option struct {
		Id   int    `json:"id"`
		Name string `json:"name"`
	}
	result := []Option{}
	for rows.Next() {
		var o Option
		if err := rows.Scan(&o.Id, &o.Name); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan row: " + err.Error()})
			c.Abort()
			return
		}
		result = append(result, o)
	}
	c.JSON(http.StatusOK, result)
}

func ListFreeFormatDateSettings(c *gin.Context, db *sql.DB) {
	rows, err := db.Query("SELECT id, name, free_format_date FROM free_format_date_settings ORDER BY name ASC")
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to list free_format_date_settings: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	result := []Entities.FreeFormatDateSetting{}
	for rows.Next() {
		var s Entities.FreeFormatDateSetting
		var ffdJSON string
		if err := rows.Scan(&s.Id, &s.Name, &ffdJSON); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan row: " + err.Error()})
			c.Abort()
			return
		}
		if err := json.Unmarshal([]byte(ffdJSON), &s.FreeFormatDate); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to parse free_format_date: " + err.Error()})
			c.Abort()
			return
		}
		result = append(result, s)
	}
	c.JSON(http.StatusOK, result)
}

func CreateFreeFormatDateSetting(c *gin.Context, db *sql.DB) {
	var req Entities.FreeFormatDateSetting
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	ffdJSON, err := json.Marshal(req.FreeFormatDate)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to encode free_format_date"})
		c.Abort()
		return
	}

	res, err := db.Exec("INSERT INTO free_format_date_settings (name, free_format_date) VALUES (?, ?)", req.Name, string(ffdJSON))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create setting: " + err.Error()})
		c.Abort()
		return
	}

	id, _ := res.LastInsertId()
	c.JSON(http.StatusCreated, gin.H{"id": id})
}

func GetFreeFormatDateSetting(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid id"})
		c.Abort()
		return
	}

	var s Entities.FreeFormatDateSetting
	var ffdJSON string
	err = db.QueryRow("SELECT id, name, free_format_date FROM free_format_date_settings WHERE id = ?", id).Scan(&s.Id, &s.Name, &ffdJSON)
	if err == sql.ErrNoRows {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Setting not found"})
		c.Abort()
		return
	}
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get setting: " + err.Error()})
		c.Abort()
		return
	}
	if err := json.Unmarshal([]byte(ffdJSON), &s.FreeFormatDate); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to parse free_format_date"})
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, s)
}

func UpdateFreeFormatDateSetting(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid id"})
		c.Abort()
		return
	}

	var req Entities.FreeFormatDateSetting
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	ffdJSON, err := json.Marshal(req.FreeFormatDate)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to encode free_format_date"})
		c.Abort()
		return
	}

	res, err := db.Exec("UPDATE free_format_date_settings SET name = ?, free_format_date = ? WHERE id = ?", req.Name, string(ffdJSON), id)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update setting: " + err.Error()})
		c.Abort()
		return
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Setting not found"})
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Updated"})
}

func DeleteFreeFormatDateSetting(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid id"})
		c.Abort()
		return
	}

	res, err := db.Exec("DELETE FROM free_format_date_settings WHERE id = ?", id)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to delete setting: " + err.Error()})
		c.Abort()
		return
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Setting not found"})
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Deleted"})
}
