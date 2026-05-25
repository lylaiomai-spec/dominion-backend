package Middlewares

import (
	"cuento-backend/src/Services"

	"github.com/gin-gonic/gin"
)

func GuestTrackingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetHeader("Authorization") == "" {
			fingerprint := Services.GuestFingerprint(c.Request)
			Services.GuestActivity.Track(fingerprint)
		}
		c.Next()
	}
}
