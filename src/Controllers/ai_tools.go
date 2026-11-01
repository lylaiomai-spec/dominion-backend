package Controllers

import (
	"cuento-backend/src/MCP"
	"cuento-backend/src/Middlewares"
	"database/sql"
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
