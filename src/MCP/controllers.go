package MCP

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func SendMessage(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		c.Abort()
		return
	}

	var req struct {
		Content string `json:"content" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	if activeAgent == nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusServiceUnavailable, Message: "AI is not configured"})
		c.Abort()
		return
	}

	// Save user message
	_, err := db.Exec(
		"INSERT INTO ai_chat_messages (user_id, role, content, date_created) VALUES (?, 'user', ?, NOW())",
		userID, req.Content,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to save message"})
		c.Abort()
		return
	}

	// Enqueue AI task
	res, err := db.Exec(
		"INSERT INTO ai_task_queue (user_id, status, date_created) VALUES (?, 'pending', NOW())",
		userID,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to queue task"})
		c.Abort()
		return
	}
	taskID, _ := res.LastInsertId()

	// Count tasks ahead of this one.
	var position int
	_ = db.QueryRow(
		`SELECT COUNT(*) FROM ai_task_queue WHERE status IN ('pending', 'processing') AND id < ?`,
		taskID,
	).Scan(&position)

	if position > 0 {
		AddAISubscriber(userID, int(taskID))
	}

	notifyWorker()

	c.JSON(http.StatusOK, gin.H{"message": "ok", "queue_position": position})
}

func GetAvailableModels(c *gin.Context, db *sql.DB) {
	models, err := ListAvailableModels(db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch models: " + err.Error()})
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, models)
}

func GetAIChatHistory(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		c.Abort()
		return
	}

	limit := 50
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	rows, err := db.Query(
		"SELECT id, user_id, role, content, sources, date_created FROM ai_chat_messages WHERE user_id = ? ORDER BY date_created ASC LIMIT ?",
		userID, limit,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch history"})
		c.Abort()
		return
	}
	defer rows.Close()

	messages := make([]Entities.AIChatMessage, 0)
	for rows.Next() {
		var msg Entities.AIChatMessage
		var sourcesRaw []byte
		if err := rows.Scan(&msg.Id, &msg.UserId, &msg.Role, &msg.Content, &sourcesRaw, &msg.DateCreated); err != nil {
			continue
		}
		if len(sourcesRaw) > 0 {
			_ = json.Unmarshal(sourcesRaw, &msg.Sources)
		}
		messages = append(messages, msg)
	}

	c.JSON(http.StatusOK, messages)
}

func ClearAIContext(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		c.Abort()
		return
	}

	_, err := db.Exec(
		"INSERT INTO ai_chat_messages (user_id, role, content, date_created) VALUES (?, 'clear', '', NOW())",
		userID,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to clear context"})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}
