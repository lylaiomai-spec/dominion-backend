package Controllers

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"cuento-backend/src/Websockets"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins
	},
}

func HandleWebSocket(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		c.Abort()
		return
	}

	// Fetch username for activity tracking
	var username string
	err := db.QueryRow("SELECT username FROM users WHERE id = ?", userID).Scan(&username)
	if err != nil {
		// If user not found or db error, maybe abort?
		// For now, let's proceed but maybe log it?
		// Or just return error.
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get user details"})
		c.Abort()
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}

	client := &Websockets.Client{
		Hub:    Websockets.MainHub,
		Conn:   conn,
		Send:   make(chan interface{}, 256),
		UserID: userID,
	}

	Websockets.MainHub.Register(client)
	Services.ActivityStorage.AddUser(userID, username)

	// Replay missed messages if the client provides last_message_id as a query param.
	if lastMsgIDStr := c.Query("last_message_id"); lastMsgIDStr != "" {
		if lastMsgID, parseErr := strconv.ParseInt(lastMsgIDStr, 10, 64); parseErr == nil {
			for _, m := range Websockets.MainHub.GetMissedMessages(userID, lastMsgID) {
				select {
				case client.Send <- m:
				default:
				}
			}
		}
	}

	// Read loop to keep connection alive and detect disconnects
	go func() {
		defer func() {
			removed := Services.ActivityStorage.RemoveUser(userID)
			if removed {
				BroadcastActiveUsersToHome()
				go BroadcastActiveUserActivity(db)
			}
			Websockets.MainHub.Unregister(client)
			conn.Close()
		}()

		// Set up Ping/Pong handlers to keep connection alive
		conn.SetReadLimit(512)
		conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		conn.SetPongHandler(func(string) error { conn.SetReadDeadline(time.Now().Add(30 * time.Second)); return nil })

		for {
			_, p, err := conn.ReadMessage()
			if err != nil {
				break
			}

			var msg struct {
				Type                string      `json:"type"`
				PageType            string      `json:"page_type"`
				PageId              interface{} `json:"page_id"`
				TopicId             interface{} `json:"topic_id"`
				PostId              *int64      `json:"post_id"`
				ChatId              *int        `json:"chat_id"`
				LastViewedMessageId *int        `json:"last_viewed_message_id"`
				LastMessageId       *int64      `json:"last_message_id"`
				PanelName           string      `json:"panel_name"`
			}
			if err := json.Unmarshal(p, &msg); err == nil {
				if msg.Type == "page_change" {
					var pageIdStr string
					switch v := msg.PageId.(type) {
					case string:
						pageIdStr = v
					case float64:
						pageIdStr = fmt.Sprintf("%.0f", v)
					case int:
						pageIdStr = fmt.Sprintf("%d", v)
					}
					Services.ActivityStorage.UpdateUserLocation(db, userID, msg.PageType, pageIdStr)
					if msg.PageType == "home" {
						BroadcastActiveUsersToHome()
					}
					go BroadcastActiveUserActivity(db)
				} else if msg.Type == "topic_view" && msg.TopicId != nil && msg.PostId != nil {
					var topicID int64
					switch v := msg.TopicId.(type) {
					case string:
						topicID, _ = strconv.ParseInt(v, 10, 64)
					case float64:
						topicID = int64(v)
					case int64:
						topicID = v
					case int:
						topicID = int64(v)
					}
					if topicID > 0 {
						_ = Services.ActivityStorage.UpdateTopicView(db, userID, topicID, msg.PostId)
					}
				} else if msg.Type == "panel_reload" && msg.PanelName != "" {
					Websockets.MainHub.Broadcast(map[string]interface{}{
						"type":       "panel_reload",
						"panel_name": msg.PanelName,
					})
				} else if msg.Type == "direct_message_viewed" && msg.ChatId != nil && msg.LastViewedMessageId != nil {
					_, _ = db.Exec(
						"UPDATE direct_chat_users SET last_read_message_id = ? WHERE direct_chat_id = ? AND user_id = ?",
						msg.LastViewedMessageId, msg.ChatId, userID,
					)
				} else if msg.Type == "replay" && msg.LastMessageId != nil {
					for _, m := range Websockets.MainHub.GetMissedMessages(userID, *msg.LastMessageId) {
						select {
						case client.Send <- m:
						default:
						}
					}
				}
			}
		}
	}()
}

func GetActiveUsers(c *gin.Context) {
	activeUsers := Services.ActivityStorage.GetActiveUsers()
	shortUsers := make([]Entities.ShortUser, 0, len(activeUsers))
	for _, u := range activeUsers {
		shortUsers = append(shortUsers, Entities.ShortUser{Id: u.UserID, Username: u.Username})
	}
	c.JSON(http.StatusOK, shortUsers)
}

func BroadcastActiveUsersToHome() {
	activeUsers := Services.ActivityStorage.GetActiveUsers()
	shortUsers := make([]Entities.ShortUser, 0, len(activeUsers))
	for _, u := range activeUsers {
		shortUsers = append(shortUsers, Entities.ShortUser{Id: u.UserID, Username: u.Username})
	}
	msg := map[string]interface{}{
		"type": "active_users_update",
		"data": shortUsers,
	}
	for _, u := range Services.ActivityStorage.GetActiveUsers() {
		if u.CurrentPageType == "home" {
			Websockets.MainHub.SendNotification(u.UserID, msg)
		}
	}
}
