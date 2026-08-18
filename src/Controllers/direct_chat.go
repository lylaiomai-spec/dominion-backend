package Controllers

import (
	"cuento-backend/src/Events"
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

func GetLastMessages(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		c.Abort()
		return
	}

	chatID, err := strconv.Atoi(c.Param("chatID"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid chat ID"})
		c.Abort()
		return
	}

	// Verify participation and fetch block date
	var count int
	var blockedSince *time.Time
	err = db.QueryRow(
		"SELECT COUNT(*), MAX(chat_blocked_since_date) FROM direct_chat_users WHERE direct_chat_id = ? AND user_id = ?",
		chatID, userID,
	).Scan(&count, &blockedSince)
	if err != nil || count == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "You are not a participant in this chat"})
		c.Abort()
		return
	}

	n := 10
	if nStr := c.Query("n"); nStr != "" {
		if parsed, err := strconv.Atoi(nStr); err == nil && parsed > 0 {
			n = parsed
		}
	}

	messageIDStr := c.Query("message_id")

	type Message struct {
		Id                    int        `json:"id"`
		UserID                int        `json:"user_id"`
		Username              string     `json:"username"`
		Avatar                *string    `json:"avatar"`
		DateSend              time.Time  `json:"date_send"`
		DateSendLocalized     string     `json:"date_send_localized"`
		DateReceived          *time.Time `json:"date_received"`
		DateReceivedLocalized *string    `json:"date_received_localized,omitempty"`
		Ciphertext            string     `json:"ciphertext"`
		IV                    string     `json:"iv"`
		Key                   string     `json:"key"`
	}

	userTimezone := Services.GetUserTimezone(userID, db)

	scanRows := func(rows *sql.Rows) ([]Message, error) {
		var msgs []Message
		for rows.Next() {
			var msg Message
			var keyAuthor, keyReceiver string
			if err := rows.Scan(&msg.Id, &msg.UserID, &msg.Username, &msg.Avatar, &msg.DateSend, &msg.DateReceived, &msg.Ciphertext, &msg.IV, &keyAuthor, &keyReceiver); err != nil {
				return nil, err
			}
			if msg.UserID == userID {
				msg.Key = keyAuthor
			} else {
				msg.Key = keyReceiver
			}
			msg.DateSendLocalized = Services.LocalizeTime(msg.DateSend, userTimezone)
			if msg.DateReceived != nil {
				s := Services.LocalizeTime(*msg.DateReceived, userTimezone)
				msg.DateReceivedLocalized = &s
			}
			msgs = append(msgs, msg)
		}
		return msgs, nil
	}

	var messages []Message

	if messageIDStr == "" {
		var rows *sql.Rows
		if blockedSince != nil {
			rows, err = db.Query(`
				SELECT m.id, m.user_id, u.username, u.avatar, m.date_send, m.date_received, m.ciphertext, m.iv, m.key_author, m.key_receiver
				FROM direct_chat_messages m
				JOIN users u ON u.id = m.user_id
				WHERE m.chat_id = ? AND m.date_send <= ?
				ORDER BY m.date_send DESC
				LIMIT ?
			`, chatID, blockedSince, n)
		} else {
			rows, err = db.Query(`
				SELECT m.id, m.user_id, u.username, u.avatar, m.date_send, m.date_received, m.ciphertext, m.iv, m.key_author, m.key_receiver
				FROM direct_chat_messages m
				JOIN users u ON u.id = m.user_id
				WHERE m.chat_id = ?
				ORDER BY m.date_send DESC
				LIMIT ?
			`, chatID, n)
		}
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get messages: " + err.Error()})
			c.Abort()
			return
		}
		defer rows.Close()
		messages, err = scanRows(rows)
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan messages: " + err.Error()})
			c.Abort()
			return
		}
		for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
			messages[i], messages[j] = messages[j], messages[i]
		}
	} else {
		messageID, err := strconv.Atoi(messageIDStr)
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid message ID"})
			c.Abort()
			return
		}

		// N messages before
		var beforeRows *sql.Rows
		if blockedSince != nil {
			beforeRows, err = db.Query(`
				SELECT m.id, m.user_id, u.username, u.avatar, m.date_send, m.date_received, m.ciphertext, m.iv, m.key_author, m.key_receiver
				FROM direct_chat_messages m
				JOIN users u ON u.id = m.user_id
				WHERE m.chat_id = ? AND m.id < ? AND m.date_send <= ?
				ORDER BY m.date_send DESC
				LIMIT ?
			`, chatID, messageID, blockedSince, n)
		} else {
			beforeRows, err = db.Query(`
				SELECT m.id, m.user_id, u.username, u.avatar, m.date_send, m.date_received, m.ciphertext, m.iv, m.key_author, m.key_receiver
				FROM direct_chat_messages m
				JOIN users u ON u.id = m.user_id
				WHERE m.chat_id = ? AND m.id < ?
				ORDER BY m.date_send DESC
				LIMIT ?
			`, chatID, messageID, n)
		}
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get messages before: " + err.Error()})
			c.Abort()
			return
		}
		defer beforeRows.Close()
		before, err := scanRows(beforeRows)
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan messages: " + err.Error()})
			c.Abort()
			return
		}
		// Reverse to chronological order
		for i, j := 0, len(before)-1; i < j; i, j = i+1, j-1 {
			before[i], before[j] = before[j], before[i]
		}

		// N messages after
		var afterRows *sql.Rows
		if blockedSince != nil {
			afterRows, err = db.Query(`
				SELECT m.id, m.user_id, u.username, u.avatar, m.date_send, m.date_received, m.ciphertext, m.iv, m.key_author, m.key_receiver
				FROM direct_chat_messages m
				JOIN users u ON u.id = m.user_id
				WHERE m.chat_id = ? AND m.id > ? AND m.date_send <= ?
				ORDER BY m.date_send ASC
				LIMIT ?
			`, chatID, messageID, blockedSince, n)
		} else {
			afterRows, err = db.Query(`
				SELECT m.id, m.user_id, u.username, u.avatar, m.date_send, m.date_received, m.ciphertext, m.iv, m.key_author, m.key_receiver
				FROM direct_chat_messages m
				JOIN users u ON u.id = m.user_id
				WHERE m.chat_id = ? AND m.id > ?
				ORDER BY m.date_send ASC
				LIMIT ?
			`, chatID, messageID, n)
		}
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get messages after: " + err.Error()})
			c.Abort()
			return
		}
		defer afterRows.Close()
		after, err := scanRows(afterRows)
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan messages: " + err.Error()})
			c.Abort()
			return
		}

		messages = append(before, after...)
	}

	if messages == nil {
		messages = []Message{}
	}

	c.JSON(http.StatusOK, messages)
}

func GetMessagesBefore(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		c.Abort()
		return
	}

	chatID, err := strconv.Atoi(c.Param("chatID"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid chat ID"})
		c.Abort()
		return
	}

	messageID, err := strconv.Atoi(c.Param("messageID"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid message ID"})
		c.Abort()
		return
	}

	var count int
	var blockedSince *time.Time
	err = db.QueryRow(
		"SELECT COUNT(*), MAX(chat_blocked_since_date) FROM direct_chat_users WHERE direct_chat_id = ? AND user_id = ?",
		chatID, userID,
	).Scan(&count, &blockedSince)
	if err != nil || count == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "You are not a participant in this chat"})
		c.Abort()
		return
	}

	n := 10
	if nStr := c.Query("n"); nStr != "" {
		if parsed, err := strconv.Atoi(nStr); err == nil && parsed > 0 {
			n = parsed
		}
	}

	type Message struct {
		Id                    int        `json:"id"`
		UserID                int        `json:"user_id"`
		Username              string     `json:"username"`
		Avatar                *string    `json:"avatar"`
		DateSend              time.Time  `json:"date_send"`
		DateSendLocalized     string     `json:"date_send_localized"`
		DateReceived          *time.Time `json:"date_received"`
		DateReceivedLocalized *string    `json:"date_received_localized,omitempty"`
		Ciphertext            string     `json:"ciphertext"`
		IV                    string     `json:"iv"`
		Key                   string     `json:"key"`
	}

	userTimezone := Services.GetUserTimezone(userID, db)

	var rows *sql.Rows
	if blockedSince != nil {
		rows, err = db.Query(`
			SELECT m.id, m.user_id, u.username, u.avatar, m.date_send, m.date_received, m.ciphertext, m.iv, m.key_author, m.key_receiver
			FROM direct_chat_messages m
			JOIN users u ON u.id = m.user_id
			WHERE m.chat_id = ? AND m.id < ? AND m.date_send <= ?
			ORDER BY m.date_send DESC
			LIMIT ?
		`, chatID, messageID, blockedSince, n)
	} else {
		rows, err = db.Query(`
			SELECT m.id, m.user_id, u.username, u.avatar, m.date_send, m.date_received, m.ciphertext, m.iv, m.key_author, m.key_receiver
			FROM direct_chat_messages m
			JOIN users u ON u.id = m.user_id
			WHERE m.chat_id = ? AND m.id < ?
			ORDER BY m.date_send DESC
			LIMIT ?
		`, chatID, messageID, n)
	}
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get messages: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var msg Message
		var keyAuthor, keyReceiver string
		if err := rows.Scan(&msg.Id, &msg.UserID, &msg.Username, &msg.Avatar, &msg.DateSend, &msg.DateReceived, &msg.Ciphertext, &msg.IV, &keyAuthor, &keyReceiver); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan messages: " + err.Error()})
			c.Abort()
			return
		}
		if msg.UserID == userID {
			msg.Key = keyAuthor
		} else {
			msg.Key = keyReceiver
		}
		msg.DateSendLocalized = Services.LocalizeTime(msg.DateSend, userTimezone)
		if msg.DateReceived != nil {
			s := Services.LocalizeTime(*msg.DateReceived, userTimezone)
			msg.DateReceivedLocalized = &s
		}
		messages = append(messages, msg)
	}

	// Reverse to chronological order
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	if messages == nil {
		messages = []Message{}
	}

	c.JSON(http.StatusOK, messages)
}

func GetMessagesAfter(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		c.Abort()
		return
	}

	chatID, err := strconv.Atoi(c.Param("chatID"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid chat ID"})
		c.Abort()
		return
	}

	messageID, err := strconv.Atoi(c.Param("messageID"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid message ID"})
		c.Abort()
		return
	}

	var count int
	var blockedSince *time.Time
	err = db.QueryRow(
		"SELECT COUNT(*), MAX(chat_blocked_since_date) FROM direct_chat_users WHERE direct_chat_id = ? AND user_id = ?",
		chatID, userID,
	).Scan(&count, &blockedSince)
	if err != nil || count == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "You are not a participant in this chat"})
		c.Abort()
		return
	}

	n := 10
	if nStr := c.Query("n"); nStr != "" {
		if parsed, err := strconv.Atoi(nStr); err == nil && parsed > 0 {
			n = parsed
		}
	}

	type Message struct {
		Id                    int        `json:"id"`
		UserID                int        `json:"user_id"`
		Username              string     `json:"username"`
		Avatar                *string    `json:"avatar"`
		DateSend              time.Time  `json:"date_send"`
		DateSendLocalized     string     `json:"date_send_localized"`
		DateReceived          *time.Time `json:"date_received"`
		DateReceivedLocalized *string    `json:"date_received_localized,omitempty"`
		Ciphertext            string     `json:"ciphertext"`
		IV                    string     `json:"iv"`
		Key                   string     `json:"key"`
	}

	userTimezone := Services.GetUserTimezone(userID, db)

	var rows *sql.Rows
	if blockedSince != nil {
		rows, err = db.Query(`
			SELECT m.id, m.user_id, u.username, u.avatar, m.date_send, m.date_received, m.ciphertext, m.iv, m.key_author, m.key_receiver
			FROM direct_chat_messages m
			JOIN users u ON u.id = m.user_id
			WHERE m.chat_id = ? AND m.id > ? AND m.date_send <= ?
			ORDER BY m.date_send ASC
			LIMIT ?
		`, chatID, messageID, blockedSince, n)
	} else {
		rows, err = db.Query(`
			SELECT m.id, m.user_id, u.username, u.avatar, m.date_send, m.date_received, m.ciphertext, m.iv, m.key_author, m.key_receiver
			FROM direct_chat_messages m
			JOIN users u ON u.id = m.user_id
			WHERE m.chat_id = ? AND m.id > ?
			ORDER BY m.date_send ASC
			LIMIT ?
		`, chatID, messageID, n)
	}
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get messages: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var msg Message
		var keyAuthor, keyReceiver string
		if err := rows.Scan(&msg.Id, &msg.UserID, &msg.Username, &msg.Avatar, &msg.DateSend, &msg.DateReceived, &msg.Ciphertext, &msg.IV, &keyAuthor, &keyReceiver); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan messages: " + err.Error()})
			c.Abort()
			return
		}
		if msg.UserID == userID {
			msg.Key = keyAuthor
		} else {
			msg.Key = keyReceiver
		}
		msg.DateSendLocalized = Services.LocalizeTime(msg.DateSend, userTimezone)
		if msg.DateReceived != nil {
			s := Services.LocalizeTime(*msg.DateReceived, userTimezone)
			msg.DateReceivedLocalized = &s
		}
		messages = append(messages, msg)
	}

	if messages == nil {
		messages = []Message{}
	}

	c.JSON(http.StatusOK, messages)
}

type CreateDirectChatRequest struct {
	RecipientID int `json:"recipient_id" binding:"required"`
}

func CreateDirectChat(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		c.Abort()
		return
	}

	var req CreateDirectChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	// Find existing chat between the two users
	var chatID int
	err := db.QueryRow(`
		SELECT dcu1.direct_chat_id
		FROM direct_chat_users dcu1
		JOIN direct_chat_users dcu2 ON dcu1.direct_chat_id = dcu2.direct_chat_id
		WHERE dcu1.user_id = ? AND dcu2.user_id = ?
		LIMIT 1
	`, userID, req.RecipientID).Scan(&chatID)

	if err == nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusConflict, Message: "A chat with this user already exists"})
		c.Abort()
		return
	} else if err != sql.ErrNoRows {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to find chat: " + err.Error()})
		c.Abort()
		return
	}

	res, err := db.Exec(
		"INSERT INTO direct_chats (start_date, status) VALUES (?, 0)",
		time.Now(),
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create chat: " + err.Error()})
		c.Abort()
		return
	}
	id, err := res.LastInsertId()
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get chat ID: " + err.Error()})
		c.Abort()
		return
	}
	chatID = int(id)

	_, err = db.Exec(
		"INSERT INTO direct_chat_users (direct_chat_id, user_id) VALUES (?, ?), (?, ?)",
		chatID, userID, chatID, req.RecipientID,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to add chat participants: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusCreated, gin.H{"chat_id": chatID})
}

type DirectChatParticipant struct {
	Id                int     `json:"id"`
	Username          string  `json:"username"`
	Avatar            *string `json:"avatar"`
	PublicKey         *string `json:"public_key"`
	LastReadMessageId *int    `json:"last_read_message_id"`
}

type DirectChatLastReadMessage struct {
	Id           int        `json:"id"`
	UserID       int        `json:"user_id"`
	DateSend     time.Time  `json:"date_send"`
	DateReceived *time.Time `json:"date_received"`
	Ciphertext   string     `json:"ciphertext"`
	IV           string     `json:"iv"`
	KeyAuthor    string     `json:"key_author"`
	KeyReceiver  string     `json:"key_receiver"`
}

type DirectChatResponse struct {
	ChatID            int                     `json:"chat_id"`
	Participants      []DirectChatParticipant `json:"participants"`
	LastReadMessageId *int                    `json:"last_read_message_id"`
}

func GetDirectChat(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		c.Abort()
		return
	}

	chatID, err := strconv.Atoi(c.Param("chatID"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid chat ID"})
		c.Abort()
		return
	}

	// Verify current user is in the chat and get both participants
	rows, err := db.Query(`
		SELECT u.id, u.username, u.avatar, pk.public_key, dcu.last_read_message_id
		FROM direct_chat_users dcu
		JOIN direct_chat_users dcu_self ON dcu_self.direct_chat_id = dcu.direct_chat_id AND dcu_self.user_id = ?
		JOIN users u ON dcu.user_id = u.id
		LEFT JOIN public_keys pk ON pk.user_id = u.id
		WHERE dcu.direct_chat_id = ?
	`, userID, chatID)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get chat participants: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	var participants []DirectChatParticipant
	for rows.Next() {
		var p DirectChatParticipant
		if err := rows.Scan(&p.Id, &p.Username, &p.Avatar, &p.PublicKey, &p.LastReadMessageId); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan participant: " + err.Error()})
			c.Abort()
			return
		}
		participants = append(participants, p)
	}
	if len(participants) == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Chat not found or you are not a participant"})
		c.Abort()
		return
	}

	// Get current user's last read message
	var lastReadMessageID *int
	err = db.QueryRow(
		"SELECT last_read_message_id FROM direct_chat_users WHERE direct_chat_id = ? AND user_id = ?",
		chatID, userID,
	).Scan(&lastReadMessageID)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get last read message: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, DirectChatResponse{
		ChatID:            chatID,
		Participants:      participants,
		LastReadMessageId: lastReadMessageID,
	})
}

type CreateDirectChatMessageRequest struct {
	ChatID      int    `json:"chat_id" binding:"required"`
	Ciphertext  string `json:"ciphertext" binding:"required"`
	IV          string `json:"iv" binding:"required"`
	KeyAuthor   string `json:"key_author" binding:"required"`
	KeyReceiver string `json:"key_receiver" binding:"required"`
}

func CreateDirectChatMessage(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		c.Abort()
		return
	}

	var req CreateDirectChatMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	// Verify the user is a participant in this chat
	var count int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM direct_chat_users WHERE direct_chat_id = ? AND user_id = ?",
		req.ChatID, userID,
	).Scan(&count)
	if err != nil || count == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "You are not a participant in this chat"})
		c.Abort()
		return
	}

	dateSend := time.Now()
	res, err := db.Exec(
		"INSERT INTO direct_chat_messages (chat_id, user_id, date_send, ciphertext, iv, key_author, key_receiver) VALUES (?, ?, ?, ?, ?, ?, ?)",
		req.ChatID, userID, dateSend, req.Ciphertext, req.IV, req.KeyAuthor, req.KeyReceiver,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create message: " + err.Error()})
		c.Abort()
		return
	}

	messageID, err := res.LastInsertId()
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get message ID: " + err.Error()})
		c.Abort()
		return
	}

	Events.Publish(db, Events.DirectMessageCreated, Events.DirectMessageCreatedEvent{
		MessageID:   messageID,
		ChatID:      req.ChatID,
		SenderID:    userID,
		Ciphertext:  req.Ciphertext,
		IV:          req.IV,
		KeyAuthor:   req.KeyAuthor,
		KeyReceiver: req.KeyReceiver,
		DateSend:    dateSend,
	})

	c.JSON(http.StatusCreated, gin.H{"message_id": messageID})
}

type DirectChatListItem struct {
	ChatID          int        `json:"chat_id"`
	UserId          int        `json:"user_id"`
	Username        string     `json:"username"`
	UnreadCount     *int       `json:"unread_count"`
	BlockedSinceDate *time.Time `json:"chat_blocked_since_date"`
}

func GetDirectChatList(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		c.Abort()
		return
	}

	rows, err := db.Query(`
		SELECT dcu.direct_chat_id, u.id, u.username, dcu.unread_count, dcu.chat_blocked_since_date
		FROM direct_chat_users dcu
		JOIN direct_chat_users dcu_other ON dcu_other.direct_chat_id = dcu.direct_chat_id AND dcu_other.user_id != dcu.user_id
		JOIN users u ON u.id = dcu_other.user_id
		WHERE dcu.user_id = ?
	`, userID)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get chat list: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	chats := []DirectChatListItem{}
	for rows.Next() {
		var item DirectChatListItem
		if err := rows.Scan(&item.ChatID, &item.UserId, &item.Username, &item.UnreadCount, &item.BlockedSinceDate); err != nil {
			continue
		}
		chats = append(chats, item)
	}

	c.JSON(http.StatusOK, chats)
}

func BlockDirectChat(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		c.Abort()
		return
	}

	chatID, err := strconv.Atoi(c.Param("chatID"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid chat ID"})
		c.Abort()
		return
	}

	var count int
	err = db.QueryRow(
		"SELECT COUNT(*) FROM direct_chat_users WHERE direct_chat_id = ? AND user_id = ?",
		chatID, userID,
	).Scan(&count)
	if err != nil || count == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "You are not a participant in this chat"})
		c.Abort()
		return
	}

	_, err = db.Exec(
		"UPDATE direct_chat_users SET chat_blocked_since_date = NOW() WHERE direct_chat_id = ? AND user_id = ? AND chat_blocked_since_date IS NULL",
		chatID, userID,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to block chat: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Chat blocked"})
}

func UnblockDirectChat(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		c.Abort()
		return
	}

	chatID, err := strconv.Atoi(c.Param("chatID"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid chat ID"})
		c.Abort()
		return
	}

	var count int
	err = db.QueryRow(
		"SELECT COUNT(*) FROM direct_chat_users WHERE direct_chat_id = ? AND user_id = ?",
		chatID, userID,
	).Scan(&count)
	if err != nil || count == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "You are not a participant in this chat"})
		c.Abort()
		return
	}

	_, err = db.Exec(
		"UPDATE direct_chat_users SET chat_blocked_since_date = NULL WHERE direct_chat_id = ? AND user_id = ?",
		chatID, userID,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to unblock chat: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Chat unblocked"})
}
