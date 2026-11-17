package Middlewares

import (
	"cuento-backend/src/Services"
	"database/sql"

	"github.com/gin-gonic/gin"
)

const userLocaleKey = "userLocale"

func UserLocaleMiddleware(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := Services.GetUserIdFromContext(c)
		c.Set(userLocaleKey, Services.GetUserLocale(userID, db))
		c.Next()
	}
}
