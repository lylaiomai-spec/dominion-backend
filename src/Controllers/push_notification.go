package Controllers

import (
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
)

type pushSubscribeRequest struct {
	Endpoint string `json:"endpoint" binding:"required"`
	P256dh   string `json:"p256dh"   binding:"required"`
	Auth     string `json:"auth"     binding:"required"`
}

type pushUnsubscribeRequest struct {
	Endpoint string `json:"endpoint" binding:"required"`
}

func GetVAPIDPublicKey(c *gin.Context, db *sql.DB) {
	public, _, err := Services.GetOrCreateVAPIDKeys(db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get VAPID key: " + err.Error()})
		c.Abort()
		return
	}
	c.JSON(http.StatusOK, gin.H{"public_key": public})
}

func SubscribePushNotifications(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		c.Abort()
		return
	}

	var req pushSubscribeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	// Update keys if endpoint already registered, otherwise insert.
	res, err := db.Exec(
		"UPDATE user_push_subscriptions SET p256dh = ?, auth = ? WHERE user_id = ? AND endpoint = ?",
		req.P256dh, req.Auth, userID, req.Endpoint,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to save subscription: " + err.Error()})
		c.Abort()
		return
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		if _, err := db.Exec(
			"INSERT INTO user_push_subscriptions (user_id, endpoint, p256dh, auth) VALUES (?, ?, ?, ?)",
			userID, req.Endpoint, req.P256dh, req.Auth,
		); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to save subscription: " + err.Error()})
			c.Abort()
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"subscribed": true})
}

func UnsubscribePushNotifications(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		c.Abort()
		return
	}

	var req pushUnsubscribeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	_, _ = db.Exec(
		"DELETE FROM user_push_subscriptions WHERE user_id = ? AND endpoint = ?",
		userID, req.Endpoint,
	)
	c.JSON(http.StatusOK, gin.H{"unsubscribed": true})
}
