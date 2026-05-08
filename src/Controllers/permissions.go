package Controllers

import (
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Define a struct to match the incoming JSON object
type UpdatePermissionsRequest struct {
	Permissions []string `json:"permissions"`
}

func GetPermissionMatrix(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	lang := Services.GetUserLanguage(userID, db)

	endpointMatrix, err := Services.GetEndpointPermissionMatrix(db, lang)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get endpoint permissions: " + err.Error()})
		c.Abort()
		return
	}

	subforumMatrix, err := Services.GetSubforumPermissionMatrix(db, lang)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get subforum permissions: " + err.Error()})
		c.Abort()
		return
	}

	frontendMatrix, err := Services.GetFrontendPermissionMatrix(db, lang)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get frontend permissions: " + err.Error()})
		c.Abort()
		return
	}

	// Use the numeric PermissionType as the key
	response := map[Services.PermissionType]interface{}{
		Services.EndpointPermission: endpointMatrix,
		Services.SubforumPermission: subforumMatrix,
		Services.FrontendPermission: frontendMatrix,
	}

	c.JSON(http.StatusOK, response)
}

func UpdatePermissionMatrix(c *gin.Context, db *sql.DB) {
	var req UpdatePermissionsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	// Safety check: do not allow wiping all permissions with an empty set
	if req.Permissions == nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Permissions key is required."})
		c.Abort()
		return
	}
	if len(req.Permissions) == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Cannot update with an empty permission set."})
		c.Abort()
		return
	}

	if err := Services.UpdatePermissionMatrix(req.Permissions, db); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update permissions: " + err.Error()})
		c.Abort()
		return
	}

	// Notify all users who have any role assigned — their frontend permissions may have changed.
	rows, err := db.Query("SELECT DISTINCT user_id FROM user_role")
	if err == nil {
		defer rows.Close()
		var userIDs []int
		for rows.Next() {
			var id int
			if rows.Scan(&id) == nil {
				userIDs = append(userIDs, id)
			}
		}
		Services.NotifyUsersRefresh(userIDs, db)
	}

	c.JSON(http.StatusOK, gin.H{"message": "Permissions updated successfully"})
}
