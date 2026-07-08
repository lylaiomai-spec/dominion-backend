package Controllers

import (
	"crypto/rand"
	"cuento-backend/src/Entities"
	"cuento-backend/src/Middlewares"
	"database/sql"
	"math/big"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

const apiKeyCharset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func generateExternalAppApiKey() (string, error) {
	b := make([]byte, 25)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(apiKeyCharset))))
		if err != nil {
			return "", err
		}
		b[i] = apiKeyCharset[n.Int64()]
	}
	return string(b), nil
}

func GetExternalAppList(c *gin.Context, db *sql.DB) {
	rows, err := db.Query("SELECT id, name, api_key, status, user_id FROM external_apps ORDER BY id")
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get external apps: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	var apps []Entities.ExternalApp
	for rows.Next() {
		var a Entities.ExternalApp
		var userId sql.NullInt64
		if err := rows.Scan(&a.Id, &a.Name, &a.ApiKey, &a.Status, &userId); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan external app: " + err.Error()})
			c.Abort()
			return
		}
		if userId.Valid {
			v := int(userId.Int64)
			a.UserId = &v
		}
		apps = append(apps, a)
	}

	if apps == nil {
		apps = []Entities.ExternalApp{}
	}

	c.JSON(http.StatusOK, apps)
}

func CreateExternalApp(c *gin.Context, db *sql.DB) {
	var req struct {
		Name   string `json:"name" binding:"required"`
		Status bool   `json:"status"`
		UserId *int   `json:"user_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	apiKey, err := generateExternalAppApiKey()
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to generate API key"})
		c.Abort()
		return
	}

	res, err := db.Exec("INSERT INTO external_apps (name, api_key, status, user_id) VALUES (?, ?, ?, ?)", req.Name, apiKey, req.Status, req.UserId)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create external app: " + err.Error()})
		c.Abort()
		return
	}

	id, _ := res.LastInsertId()
	c.JSON(http.StatusOK, Entities.ExternalApp{
		Id:     int(id),
		Name:   req.Name,
		ApiKey: apiKey,
		Status: req.Status,
		UserId: req.UserId,
	})
}

type ExternalAppUpdateRequest struct {
	Name   *string `json:"name"`
	Status *bool   `json:"status"`
	UserId *int    `json:"user_id"`
}

func UpdateExternalApp(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid id"})
		c.Abort()
		return
	}

	var req ExternalAppUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	setClauses := []string{}
	args := []interface{}{}

	if req.Name != nil {
		setClauses = append(setClauses, "name = ?")
		args = append(args, *req.Name)
	}
	if req.Status != nil {
		setClauses = append(setClauses, "status = ?")
		args = append(args, *req.Status)
	}
	if req.UserId != nil {
		setClauses = append(setClauses, "user_id = ?")
		args = append(args, *req.UserId)
	}

	if len(setClauses) == 0 {
		c.JSON(http.StatusOK, gin.H{"message": "External app updated"})
		return
	}

	args = append(args, id)
	res, err := db.Exec("UPDATE external_apps SET "+strings.Join(setClauses, ", ")+" WHERE id = ?", args...)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update external app: " + err.Error()})
		c.Abort()
		return
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "External app not found"})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "External app updated"})
}

func DeleteExternalApp(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid id"})
		c.Abort()
		return
	}

	var exists bool
	if err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM external_apps WHERE id = ?)", id).Scan(&exists); err != nil || !exists {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "External app not found"})
		c.Abort()
		return
	}

	if _, err := db.Exec("DELETE FROM external_apps WHERE id = ?", id); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to delete external app: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "External app deleted"})
}

type ExternalAppPermissionEntry struct {
	ExternalAppId int                            `json:"external_app_id"`
	SubforumId    int                            `json:"subforum_id"`
	Permission    Entities.ExternalAppPermission `json:"permission"`
}

func GetExternalAppPermissions(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid id"})
		c.Abort()
		return
	}

	var exists bool
	if err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM external_apps WHERE id = ?)", id).Scan(&exists); err != nil || !exists {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "External app not found"})
		c.Abort()
		return
	}

	rows, err := db.Query("SELECT external_app_id, subforum_id, permission FROM external_app_permissions WHERE external_app_id = ?", id)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get permissions: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	var permissions []ExternalAppPermissionEntry
	for rows.Next() {
		var p ExternalAppPermissionEntry
		if err := rows.Scan(&p.ExternalAppId, &p.SubforumId, &p.Permission); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan permission: " + err.Error()})
			c.Abort()
			return
		}
		permissions = append(permissions, p)
	}

	if permissions == nil {
		permissions = []ExternalAppPermissionEntry{}
	}

	c.JSON(http.StatusOK, permissions)
}

func AddExternalAppPermission(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid id"})
		c.Abort()
		return
	}

	var req struct {
		SubforumId int                            `json:"subforum_id" binding:"required"`
		Permission Entities.ExternalAppPermission `json:"permission" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	if req.Permission != Entities.ExternalAppPermissionPost && req.Permission != Entities.ExternalAppPermissionGetActiveTopics && req.Permission != Entities.ExternalAppPermissionGetFirstPost {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid permission value"})
		c.Abort()
		return
	}

	var appExists bool
	if err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM external_apps WHERE id = ?)", id).Scan(&appExists); err != nil || !appExists {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "External app not found"})
		c.Abort()
		return
	}

	var subforumExists bool
	if err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM subforums WHERE id = ?)", req.SubforumId).Scan(&subforumExists); err != nil || !subforumExists {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Subforum not found"})
		c.Abort()
		return
	}

	_, err = db.Exec(
		"INSERT IGNORE INTO external_app_permissions (external_app_id, subforum_id, permission) VALUES (?, ?, ?)",
		id, req.SubforumId, string(req.Permission),
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to add permission: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Permission added"})
}

func DeleteExternalAppPermission(c *gin.Context, db *sql.DB) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid id"})
		c.Abort()
		return
	}

	var req struct {
		SubforumId int                            `json:"subforum_id" binding:"required"`
		Permission Entities.ExternalAppPermission `json:"permission" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	res, err := db.Exec(
		"DELETE FROM external_app_permissions WHERE external_app_id = ? AND subforum_id = ? AND permission = ?",
		id, req.SubforumId, string(req.Permission),
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to delete permission: " + err.Error()})
		c.Abort()
		return
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Permission not found"})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Permission deleted"})
}
