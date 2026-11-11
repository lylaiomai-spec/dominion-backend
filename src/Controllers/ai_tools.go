package Controllers

import (
	"cuento-backend/src/MCP"
	"cuento-backend/src/Middlewares"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

func ProofreadText(c *gin.Context, db *sql.DB) {
	var req struct {
		Text string `json:"text" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "text is required"})
		c.Abort()
		return
	}

	result, err := MCP.ProofreadText(req.Text, db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnprocessableEntity, Message: err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, result)
}

func ProofreadStream(c *gin.Context, db *sql.DB) {
	var req struct {
		Text string `json:"text" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "text is required"})
		c.Abort()
		return
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)

	sendJSON := func(data any) {
		b, _ := json.Marshal(data)
		fmt.Fprintf(c.Writer, "data: %s\n\n", b)
		c.Writer.Flush()
	}

	err := MCP.ProofreadTextStream(req.Text, db,
		func(token string) error {
			sendJSON(map[string]string{"t": token})
			return nil
		},
		func(text string, fixes []string) error {
			sendJSON(map[string]any{"done": map[string]any{"text": text, "fixes": fixes}})
			return nil
		},
	)
	if err != nil {
		sendJSON(map[string]string{"error": err.Error()})
	}
}
