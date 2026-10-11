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

type CreateNPCRequest struct {
	ArcID        int     `json:"arc_id" binding:"required"`
	Name         string  `json:"name" binding:"required"`
	Avatar       *string `json:"avatar"`
	Description  *string `json:"description"`
	DisplayOrder int     `json:"display_order"`
}

type UpdateNPCRequest struct {
	Name         *string `json:"name"`
	Avatar       *string `json:"avatar"`
	Description  *string `json:"description"`
	DisplayOrder *int    `json:"display_order"`
}

func GetArcNPCs(c *gin.Context, db *sql.DB) {
	arcID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid arc ID"})
		c.Abort()
		return
	}

	query := `
		SELECT n.id, n.name, n.avatar, n.description, n.display_order
		FROM npc n
		JOIN npc_arc na ON na.npc_id = n.id
		WHERE na.arc_id = ?
		ORDER BY n.display_order ASC`

	args := []interface{}{arcID}

	if numberStr := c.Query("number"); numberStr != "" {
		if number, err := strconv.Atoi(numberStr); err == nil && number > 0 {
			query += " LIMIT ?"
			args = append(args, number)
		}
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch NPCs: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	list := []Entities.NPC{}
	for rows.Next() {
		var n Entities.NPC
		var avatar, description sql.NullString
		if err := rows.Scan(&n.ID, &n.Name, &avatar, &description, &n.DisplayOrder); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan NPC: " + err.Error()})
			c.Abort()
			return
		}
		if avatar.Valid {
			n.Avatar = &avatar.String
		}
		if description.Valid {
			n.Description = &description.String
		}
		list = append(list, n)
	}

	c.JSON(http.StatusOK, list)
}

func GetNPC(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid NPC ID"})
		c.Abort()
		return
	}

	var n Entities.NPC
	var avatar, description sql.NullString
	err = db.QueryRow(
		"SELECT id, name, avatar, description, display_order FROM npc WHERE id = ?", id,
	).Scan(&n.ID, &n.Name, &avatar, &description, &n.DisplayOrder)
	if err == sql.ErrNoRows {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "NPC not found"})
		c.Abort()
		return
	}
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch NPC: " + err.Error()})
		c.Abort()
		return
	}
	if avatar.Valid {
		n.Avatar = &avatar.String
	}
	if description.Valid {
		n.Description = &description.String
	}

	var arcID int
	var arcTitle string
	if db.QueryRow("SELECT na.arc_id, a.title FROM npc_arc na JOIN arcs a ON a.id = na.arc_id WHERE na.npc_id = ?", id).Scan(&arcID, &arcTitle) == nil {
		n.Arc = &Entities.NPCArc{ID: arcID, Title: arcTitle}
	}

	userID := Services.GetUserIdFromContext(c)
	if userID != 0 && arcID != 0 {
		canEdit := canEditArc(userID, arcID, db)
		n.CanEdit = &canEdit
	}

	c.JSON(http.StatusOK, n)
}

func SearchNPCs(c *gin.Context, db *sql.DB) {
	arcID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid arc ID"})
		c.Abort()
		return
	}

	name := c.Query("name")

	rows, err := db.Query(`
		SELECT n.id, n.name, n.avatar, n.description, n.display_order
		FROM npc n
		JOIN npc_arc na ON na.npc_id = n.id
		WHERE na.arc_id = ? AND n.name LIKE ?
		ORDER BY n.display_order ASC`,
		arcID, "%"+name+"%",
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to search NPCs: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	list := []Entities.NPC{}
	for rows.Next() {
		var n Entities.NPC
		var avatar, description sql.NullString
		if err := rows.Scan(&n.ID, &n.Name, &avatar, &description, &n.DisplayOrder); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan NPC: " + err.Error()})
			c.Abort()
			return
		}
		if avatar.Valid {
			n.Avatar = &avatar.String
		}
		if description.Valid {
			n.Description = &description.String
		}
		list = append(list, n)
	}

	c.JSON(http.StatusOK, list)
}

func CreateNPC(c *gin.Context, db *sql.DB) {
	var req CreateNPCRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	userID := Services.GetUserIdFromContext(c)
	if !canEditArc(userID, req.ArcID, db) {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "You do not have permission to edit this arc"})
		c.Abort()
		return
	}

	res, err := db.Exec(
		"INSERT INTO npc (name, avatar, description, display_order) VALUES (?, ?, ?, ?)",
		req.Name, req.Avatar, req.Description, req.DisplayOrder,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create NPC: " + err.Error()})
		c.Abort()
		return
	}

	npcID, _ := res.LastInsertId()
	db.Exec("INSERT INTO npc_arc (npc_id, arc_id) VALUES (?, ?)", npcID, req.ArcID)

	c.JSON(http.StatusCreated, gin.H{"id": npcID})
}

func UpdateNPC(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid NPC ID"})
		c.Abort()
		return
	}

	var req UpdateNPCRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	var arcID int
	if err := db.QueryRow("SELECT arc_id FROM npc_arc WHERE npc_id = ?", id).Scan(&arcID); err != nil {
		if err == sql.ErrNoRows {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "NPC not found"})
		} else {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch NPC: " + err.Error()})
		}
		c.Abort()
		return
	}

	userID := Services.GetUserIdFromContext(c)
	if !canEditArc(userID, arcID, db) {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "You do not have permission to edit this arc"})
		c.Abort()
		return
	}

	if req.Name == nil && req.Avatar == nil && req.Description == nil && req.DisplayOrder == nil {
		c.JSON(http.StatusOK, gin.H{"message": "NPC updated successfully"})
		return
	}

	setClauses := []string{}
	args := []interface{}{}
	if req.Name != nil {
		setClauses = append(setClauses, "name = ?")
		args = append(args, *req.Name)
	}
	if req.Avatar != nil {
		setClauses = append(setClauses, "avatar = ?")
		args = append(args, *req.Avatar)
	}
	if req.Description != nil {
		setClauses = append(setClauses, "description = ?")
		args = append(args, *req.Description)
	}
	if req.DisplayOrder != nil {
		setClauses = append(setClauses, "display_order = ?")
		args = append(args, *req.DisplayOrder)
	}

	args = append(args, id)
	if _, err := db.Exec("UPDATE npc SET "+strings.Join(setClauses, ", ")+" WHERE id = ?", args...); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update NPC: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "NPC updated successfully"})
}

func DeleteNPC(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid NPC ID"})
		c.Abort()
		return
	}

	var arcID int
	if err := db.QueryRow("SELECT arc_id FROM npc_arc WHERE npc_id = ?", id).Scan(&arcID); err != nil {
		if err == sql.ErrNoRows {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "NPC not found"})
		} else {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch NPC: " + err.Error()})
		}
		c.Abort()
		return
	}

	userID := Services.GetUserIdFromContext(c)
	if !canEditArc(userID, arcID, db) {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "You do not have permission to edit this arc"})
		c.Abort()
		return
	}

	result, err := db.Exec("DELETE FROM npc WHERE id = ?", id)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to delete NPC: " + err.Error()})
		c.Abort()
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "NPC not found"})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "NPC deleted successfully"})
}

type NpcTopicItem struct {
	TopicID   int    `json:"topic_id"`
	TopicName string `json:"topic_name"`
	PostIDs   []int  `json:"post_ids"`
}

func GetNPCTopics(c *gin.Context, db *sql.DB) {
	npcID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid NPC ID"})
		c.Abort()
		return
	}

	rows, err := db.Query(`
		SELECT t.id, t.name, p.id
		FROM npc_post np
		JOIN posts p ON p.id = np.post_id
		JOIN topics t ON t.id = p.topic_id
		WHERE np.npc_id = ?
		ORDER BY t.id, p.id`, npcID)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch NPC topics: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	topicOrder := []int{}
	topicMap := map[int]*NpcTopicItem{}
	for rows.Next() {
		var topicID, postID int
		var topicName string
		if err := rows.Scan(&topicID, &topicName, &postID); err != nil {
			continue
		}
		if _, exists := topicMap[topicID]; !exists {
			topicOrder = append(topicOrder, topicID)
			topicMap[topicID] = &NpcTopicItem{TopicID: topicID, TopicName: topicName, PostIDs: []int{}}
		}
		topicMap[topicID].PostIDs = append(topicMap[topicID].PostIDs, postID)
	}

	result := make([]NpcTopicItem, 0, len(topicOrder))
	for _, id := range topicOrder {
		result = append(result, *topicMap[id])
	}

	c.JSON(http.StatusOK, result)
}
