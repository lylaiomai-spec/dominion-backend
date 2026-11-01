package MCP

import (
	"bytes"
	"cuento-backend/src/Middlewares"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

// InjectionCheckMiddleware reads the named JSON field from the request body,
// checks it for prompt injection, and aborts with 422 if one is detected.
// The body is restored so downstream handlers can still read it.
func InjectionCheckMiddleware(db *sql.DB, fieldName string) gin.HandlerFunc {
	return func(c *gin.Context) {
		bodyBytes, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.Next()
			return
		}
		// Restore body for the actual handler.
		c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

		var payload map[string]any
		if err := json.Unmarshal(bodyBytes, &payload); err != nil {
			c.Next()
			return
		}

		text, _ := payload[fieldName].(string)
		if text == "" {
			c.Next()
			return
		}

		result, err := CheckForPromptInjection(text, db)
		if err != nil || !result.IsInjection {
			c.Next()
			return
		}

		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnprocessableEntity, Message: "suspicious input detected: " + result.Reason})
		c.Abort()
	}
}
