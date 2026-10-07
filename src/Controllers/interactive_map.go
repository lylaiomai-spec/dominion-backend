package Controllers

import (
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

type InteractiveMap struct {
	ID        int             `json:"id"`
	Title     string          `json:"title"`
	Config    json.RawMessage `json:"config"`
	IsPublic  bool            `json:"is_public"`
	CreatorID *int            `json:"creator_id"`
	CanEdit   bool            `json:"can_edit"`
}

type CreateInteractiveMapRequest struct {
	Title    string          `json:"title" binding:"required"`
	Config   json.RawMessage `json:"config" binding:"required"`
	IsPublic bool            `json:"is_public"`
}

type UpdateInteractiveMapRequest struct {
	Title    *string          `json:"title"`
	Config   *json.RawMessage `json:"config"`
	IsPublic *bool            `json:"is_public"`
}

func GetInteractiveMapList(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)

	var rows *sql.Rows
	var err error
	if userID == 0 {
		rows, err = db.Query("SELECT id, title, config, is_public, creator_id FROM interactive_maps WHERE is_public = 1")
	} else {
		rows, err = db.Query("SELECT id, title, config, is_public, creator_id FROM interactive_maps")
	}
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch maps: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	canEditOthers, _ := Services.HasPermission(userID, "edit_others_maps", db)

	list := []InteractiveMap{}
	for rows.Next() {
		var m InteractiveMap
		var creatorID sql.NullInt64
		if err := rows.Scan(&m.ID, &m.Title, &m.Config, &m.IsPublic, &creatorID); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan map: " + err.Error()})
			c.Abort()
			return
		}
		if creatorID.Valid {
			id := int(creatorID.Int64)
			m.CreatorID = &id
		}
		m.CanEdit = userID != 0 && (canEditOthers || (m.CreatorID != nil && *m.CreatorID == userID))
		list = append(list, m)
	}

	c.JSON(http.StatusOK, list)
}

func GetInteractiveMap(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid map ID"})
		c.Abort()
		return
	}

	var m InteractiveMap
	var creatorID sql.NullInt64
	err = db.QueryRow(
		"SELECT id, title, config, is_public, creator_id FROM interactive_maps WHERE id = ?", id,
	).Scan(&m.ID, &m.Title, &m.Config, &m.IsPublic, &creatorID)
	if err == sql.ErrNoRows {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Map not found"})
		c.Abort()
		return
	}
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch map: " + err.Error()})
		c.Abort()
		return
	}

	userID := Services.GetUserIdFromContext(c)
	if !m.IsPublic && userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "This map is not public"})
		c.Abort()
		return
	}

	if creatorID.Valid {
		id := int(creatorID.Int64)
		m.CreatorID = &id
	}

	canEditOthers, _ := Services.HasPermission(userID, "edit_others_maps", db)
	m.CanEdit = userID != 0 && (canEditOthers || (m.CreatorID != nil && *m.CreatorID == userID))

	c.JSON(http.StatusOK, m)
}

func CreateInteractiveMap(c *gin.Context, db *sql.DB) {
	var req CreateInteractiveMapRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	userID := Services.GetUserIdFromContext(c)

	res, err := db.Exec(
		"INSERT INTO interactive_maps (title, config, is_public, creator_id) VALUES (?, ?, ?, ?)",
		req.Title, req.Config, req.IsPublic, userID,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create map: " + err.Error()})
		c.Abort()
		return
	}

	newID, _ := res.LastInsertId()
	c.JSON(http.StatusCreated, gin.H{"id": newID})
}

func UpdateInteractiveMap(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid map ID"})
		c.Abort()
		return
	}

	var req UpdateInteractiveMapRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	if req.Title == nil && req.Config == nil && req.IsPublic == nil {
		c.JSON(http.StatusOK, gin.H{"message": "Map updated successfully"})
		return
	}

	setClauses := []string{}
	args := []interface{}{}

	if req.Title != nil {
		setClauses = append(setClauses, "title = ?")
		args = append(args, *req.Title)
	}
	if req.Config != nil {
		setClauses = append(setClauses, "config = ?")
		args = append(args, *req.Config)
	}
	if req.IsPublic != nil {
		setClauses = append(setClauses, "is_public = ?")
		args = append(args, *req.IsPublic)
	}

	query := "UPDATE interactive_maps SET "
	for i, clause := range setClauses {
		if i > 0 {
			query += ", "
		}
		query += clause
	}
	query += " WHERE id = ?"
	args = append(args, id)

	var mapCreatorID sql.NullInt64
	if err := db.QueryRow("SELECT creator_id FROM interactive_maps WHERE id = ?", id).Scan(&mapCreatorID); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Map not found"})
		c.Abort()
		return
	}

	userID := Services.GetUserIdFromContext(c)
	isCreator := mapCreatorID.Valid && int(mapCreatorID.Int64) == userID
	canEditOthers, _ := Services.HasPermission(userID, "edit_others_maps", db)
	if !isCreator && !canEditOthers {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "You do not have permission to edit this map"})
		c.Abort()
		return
	}

	if _, err := db.Exec(query, args...); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update map: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Map updated successfully"})
}

func DeleteInteractiveMap(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid map ID"})
		c.Abort()
		return
	}

	result, err := db.Exec("DELETE FROM interactive_maps WHERE id = ?", id)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to delete map: " + err.Error()})
		c.Abort()
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Map not found"})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Map deleted successfully"})
}
