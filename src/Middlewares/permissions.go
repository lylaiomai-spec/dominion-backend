package Middlewares

import (
	"cuento-backend/src/Router"
	"cuento-backend/src/Services"
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
)

func PermissionsMiddleware(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get user ID from context, default to 0 (guest) if not present
		userID := Services.GetUserIdFromContext(c)

		// Superuser bypasses all endpoint permission checks
		if Services.IsSuperuser(c) {
			c.Next()
			return
		}

		// Get the matched route pattern from gin context
		endpointPath := c.FullPath()

		// Check if user has permission to access this endpoint
		query := `
			SELECT COUNT(*)
			FROM user_role ur
			INNER JOIN role_permission rp ON ur.role_id = rp.role_id
			WHERE type = 0 AND ur.user_id = ? AND rp.permission = ?`

		var count int
		err := db.QueryRow(query, userID, endpointPath).Scan(&count)
		if err != nil {
			_ = c.Error(&AppError{Code: http.StatusInternalServerError, Message: "Failed to check permissions"})
			c.Abort()
			return
		}

		if count == 0 {
			// Find the human-readable description for this endpoint
			description := endpointPath
			for _, route := range Router.AllRoutes {
				if route.Path == endpointPath {
					description = route.Definition
					break
				}
			}

			_ = c.Error(&AppError{Code: http.StatusForbidden, Message: "User does not have access to " + description})
			c.Abort()
			return
		}

		c.Next()
	}
}

type PermissionType int

const (
	EndpointPermission PermissionType = 0
	SubforumPermission PermissionType = 1
)
