package Controllers

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Events"
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"cuento-backend/src/Websockets"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// SaveKeysPrivateKeyItem holds an encrypted private key with its AES parameters.
type SaveKeysPrivateKeyItem struct {
	PrivateKey string `json:"private_key" binding:"required"`
	IV         string `json:"iv" binding:"required"`
	Salt       string `json:"salt" binding:"required"`
}

// SaveKeysRequest is the payload for the initial key-setup endpoint.
type SaveKeysRequest struct {
	Codes               []string                 `json:"codes" binding:"required"`
	PrivateKey          SaveKeysPrivateKeyItem   `json:"private_key" binding:"required"`
	RecoveryPrivateKeys []SaveKeysPrivateKeyItem `json:"recovery_private_keys" binding:"required"`
	PublicKey           Entities.PublicKey       `json:"public_key" binding:"required"`
}

// SaveKeys stores the user's initial RSA key pair, recovery codes, and public key.
// Called once after registration when the user sets up E2E encryption for the first time.
// Codes are double-hashed (frontend SHA-256 + backend SHA-256) before storage.
func SaveKeys(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		c.Abort()
		return
	}

	var req SaveKeysRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	if len(req.Codes) != len(req.RecoveryPrivateKeys) {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "codes and recovery_private_keys must have the same length"})
		c.Abort()
		return
	}

	_, err := db.Exec(
		"INSERT INTO private_keys (user_id, private_key, salt, iv, recovery_code_id, is_active) VALUES (?, ?, ?, ?, NULL, true)",
		userID, req.PrivateKey.PrivateKey, req.PrivateKey.Salt, req.PrivateKey.IV,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to save private key: " + err.Error()})
		c.Abort()
		return
	}

	for i, code := range req.Codes {
		hashBytes := sha256.Sum256([]byte(code))
		hashHex := fmt.Sprintf("%x", hashBytes)

		res, err := db.Exec(
			"INSERT INTO recovery_codes (user_id, recovery_code) VALUES (?, ?)",
			userID, hashHex,
		)
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to save recovery code: " + err.Error()})
			c.Abort()
			return
		}

		codeID, err := res.LastInsertId()
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get recovery code ID: " + err.Error()})
			c.Abort()
			return
		}

		pk := req.RecoveryPrivateKeys[i]
		_, err = db.Exec(
			"INSERT INTO private_keys (user_id, private_key, salt, iv, recovery_code_id, is_active) VALUES (?, ?, ?, ?, ?, false)",
			userID, pk.PrivateKey, pk.Salt, pk.IV, codeID,
		)
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to save recovery private key: " + err.Error()})
			c.Abort()
			return
		}
	}

	_, err = db.Exec(
		"INSERT INTO public_keys (user_id, public_key) VALUES (?, ?)",
		userID, req.PublicKey.PublicKey,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to save public key: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Keys saved successfully"})
}

// GetPrivateKey returns the active password-encrypted private key for the authenticated user,
// along with the AES IV, salt, and the ID of the linked active public key.
func GetPrivateKey(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		c.Abort()
		return
	}

	var privateKey, salt, iv string
	var publicKeyId int
	err := db.QueryRow(`
		SELECT pk.private_key, pk.salt, pk.iv, pub.id
		FROM private_keys pk
		JOIN public_keys pub ON pub.user_id = pk.user_id AND pub.is_active = true
		WHERE pk.user_id = ? AND pk.is_active = true
	`, userID).Scan(&privateKey, &salt, &iv, &publicKeyId)
	if err != nil {
		if err == sql.ErrNoRows {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "No active private key found"})
		} else {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get private key: " + err.Error()})
		}
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"private_key": privateKey, "salt": salt, "iv": iv, "public_key_id": publicKeyId})
}

// GetPublicKeyByUserId returns the active public key for a given user.
// Used by the DM system to encrypt messages for the recipient.
func GetPublicKeyByUserId(c *gin.Context, db *sql.DB) {
	userID, err := strconv.Atoi(c.Param("userID"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid user ID"})
		c.Abort()
		return
	}

	var pk Entities.PublicKey
	err = db.QueryRow(
		"SELECT id, user_id, public_key FROM public_keys WHERE user_id = ? AND is_active = true",
		userID,
	).Scan(&pk.Id, &pk.UserId, &pk.PublicKey)
	if err != nil {
		if err == sql.ErrNoRows {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "No public key found for user"})
		} else {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get public key: " + err.Error()})
		}
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, pk)
}

// RecoveryKeyItem holds an encrypted private key copy linked to a recovery code.
type RecoveryKeyItem struct {
	PrivateKey string `json:"private_key" binding:"required"`
	IV         string `json:"iv" binding:"required"`
	Salt       string `json:"salt" binding:"required"`
}

// SaveRecoveryKeysRequest is the payload for regenerating recovery codes from settings.
type SaveRecoveryKeysRequest struct {
	Codes       []string          `json:"codes" binding:"required"`
	PrivateKeys []RecoveryKeyItem `json:"recovery_private_keys" binding:"required"`
}

// SaveRecoveryKeys replaces the user's recovery codes and their linked encrypted private key copies.
// Called from settings when the user regenerates their recovery codes.
// Codes are double-hashed (frontend SHA-256 + backend SHA-256) before storage.
func SaveRecoveryKeys(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		c.Abort()
		return
	}

	var req SaveRecoveryKeysRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	if len(req.Codes) != len(req.PrivateKeys) {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "codes and private_keys must have the same length"})
		c.Abort()
		return
	}

	for i, code := range req.Codes {
		hashBytes := sha256.Sum256([]byte(code))
		hashHex := fmt.Sprintf("%x", hashBytes)

		res, err := db.Exec(
			"INSERT INTO recovery_codes (user_id, recovery_code) VALUES (?, ?)",
			userID, hashHex,
		)
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to save recovery code: " + err.Error()})
			c.Abort()
			return
		}

		codeID, err := res.LastInsertId()
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get recovery code ID: " + err.Error()})
			c.Abort()
			return
		}

		pk := req.PrivateKeys[i]
		_, err = db.Exec(
			"INSERT INTO private_keys (user_id, private_key, salt, iv, recovery_code_id, is_active) VALUES (?, ?, ?, ?, ?, false)",
			userID, pk.PrivateKey, pk.Salt, pk.IV, codeID,
		)
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to save recovery key: " + err.Error()})
			c.Abort()
			return
		}
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Recovery keys saved successfully"})
}

// RecoveryRequest is the payload for the first step of recovery-code login.
type RecoveryRequest struct {
	Code string `json:"code" binding:"required"`
}

// UpdatePasswordRequest is the payload for the second step of recovery-code login.
type UpdatePasswordRequest struct {
	Password     string `json:"password" binding:"required"`
	PrivateKey   string `json:"private_key" binding:"required"`
	IV           string `json:"iv" binding:"required"`
	Salt         string `json:"salt" binding:"required"`
	SecurityCode string `json:"security_code" binding:"required"`
}

// UpdatePassword completes the recovery-code login flow by setting the new password
// and re-encrypting the active private key. Marks the used recovery code as consumed.
func UpdatePassword(c *gin.Context, db *sql.DB) {
	var req UpdatePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	var userID int
	err := db.QueryRow(
		"SELECT user_id FROM recovery_codes WHERE security_code = ?",
		req.SecurityCode,
	).Scan(&userID)
	if err != nil {
		if err == sql.ErrNoRows {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Invalid security code"})
		} else {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to validate security code: " + err.Error()})
		}
		c.Abort()
		return
	}

	dummyUser := Entities.User{}
	if err := dummyUser.HashPassword(req.Password); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to hash password: " + err.Error()})
		c.Abort()
		return
	}

	_, err = db.Exec("UPDATE users SET password = ? WHERE id = ?", dummyUser.Password, userID)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update password: " + err.Error()})
		c.Abort()
		return
	}

	_, err = db.Exec(
		"UPDATE private_keys SET private_key = ?, iv = ?, salt = ? WHERE user_id = ? AND is_active = true",
		req.PrivateKey, req.IV, req.Salt, userID,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update private key: " + err.Error()})
		c.Abort()
		return
	}

	_, _ = db.Exec("UPDATE recovery_codes SET security_code = NULL, date_used = ? WHERE user_id = ?", time.Now(), userID)

	c.JSON(http.StatusOK, gin.H{"message": "Password updated successfully"})
}

// Recovery validates a recovery code and issues a one-time security code that is then
// used by UpdatePassword to authorize the password change. Returns the encrypted private key
// so the client can re-encrypt it under the new password before calling UpdatePassword.
func Recovery(c *gin.Context, db *sql.DB) {
	var req RecoveryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	hashBytes := sha256.Sum256([]byte(req.Code))
	hashHex := fmt.Sprintf("%x", hashBytes)

	var recoveryCodeID int
	err := db.QueryRow(
		"SELECT id FROM recovery_codes WHERE recovery_code = ? AND date_used IS NULL",
		hashHex,
	).Scan(&recoveryCodeID)
	if err != nil {
		if err == sql.ErrNoRows {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "Recovery code not found"})
		} else {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to look up recovery code: " + err.Error()})
		}
		c.Abort()
		return
	}

	var pk Entities.PrivateKey
	err = db.QueryRow(
		"SELECT user_id, private_key, salt, iv, recovery_code_id FROM private_keys WHERE recovery_code_id = ?",
		recoveryCodeID,
	).Scan(&pk.UserId, &pk.PrivateKey, &pk.Salt, &pk.IV, &pk.RecoveryKeyId)
	if err != nil {
		if err == sql.ErrNoRows {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "No private key found for this recovery code"})
		} else {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get private key: " + err.Error()})
		}
		c.Abort()
		return
	}

	securityCodeBytes := make([]byte, 32)
	if _, err := rand.Read(securityCodeBytes); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to generate security code: " + err.Error()})
		c.Abort()
		return
	}
	securityCode := hex.EncodeToString(securityCodeBytes)

	_, err = db.Exec(
		"UPDATE recovery_codes SET security_code = ? WHERE id = ?",
		securityCode, recoveryCodeID,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to save security code: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"private_key": pk, "security_code": securityCode})
}

// PrivateKeyCheck reports whether the user has any private keys or direct messages,
// which determines whether the key-setup flow should be shown on login.
func PrivateKeyCheck(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		c.Abort()
		return
	}

	var keyCount int
	_ = db.QueryRow("SELECT COUNT(*) FROM private_keys WHERE user_id = ?", userID).Scan(&keyCount)

	var messageCount int
	_ = db.QueryRow("SELECT COUNT(*) FROM direct_chat_messages WHERE user_id = ?", userID).Scan(&messageCount)

	c.JSON(http.StatusOK, gin.H{"result": keyCount > 0 || messageCount > 0})
}

// topicDeletion groups post IDs by topic for post-commit event firing after a wipe.
type topicDeletion struct {
	TopicID    int
	SubforumID int
	PostIDs    []int
}

// wipeUserInTx deletes direct messages, removes general posts, deactivates and reassigns
// characters/wanted characters/profiles to user 1, and deletes the user — all within the
// provided transaction. Returns per-topic post batches and deactivation results for
// post-commit event firing.
func wipeUserInTx(tx *sql.Tx, db *sql.DB, userID int) ([]topicDeletion, []Services.CharacterDeactivationResult, []wantedCharDeactivationResult, int, error) {
	if _, err := tx.Exec("DELETE FROM direct_chat_messages WHERE user_id = ?", userID); err != nil {
		return nil, nil, nil, 0, fmt.Errorf("failed to delete direct messages: %w", err)
	}

	rows, err := db.Query(`
		SELECT p.id, p.topic_id, t.subforum_id
		FROM posts p
		JOIN topics t ON t.id = p.topic_id
		WHERE p.author_user_id = ? AND t.type = ?
		ORDER BY p.topic_id`,
		userID, Entities.GeneralTopic,
	)
	var batches []topicDeletion
	topicIndex := map[int]int{}
	if err == nil {
		for rows.Next() {
			var pid, tid, sfid int
			if rows.Scan(&pid, &tid, &sfid) == nil {
				if idx, ok := topicIndex[tid]; ok {
					batches[idx].PostIDs = append(batches[idx].PostIDs, pid)
				} else {
					topicIndex[tid] = len(batches)
					batches = append(batches, topicDeletion{TopicID: tid, SubforumID: sfid, PostIDs: []int{pid}})
				}
			}
		}
		rows.Close()
	}

	if _, err := tx.Exec(
		"DELETE FROM posts WHERE author_user_id = ? AND topic_id IN (SELECT id FROM topics WHERE type = ?)",
		userID, Entities.GeneralTopic,
	); err != nil {
		return nil, nil, nil, 0, fmt.Errorf("failed to delete general posts: %w", err)
	}

	if _, err := tx.Exec("UPDATE posts SET author_user_id = 1 WHERE author_user_id = ?", userID); err != nil {
		return nil, nil, nil, 0, fmt.Errorf("failed to reassign posts: %w", err)
	}
	if _, err := tx.Exec("UPDATE topics SET author_user_id = 1 WHERE author_user_id = ?", userID); err != nil {
		return nil, nil, nil, 0, fmt.Errorf("failed to reassign topics: %w", err)
	}

	var charResults []Services.CharacterDeactivationResult
	charRows, err := db.Query(
		"SELECT id FROM character_base WHERE user_id = ? AND character_status = ?",
		userID, Entities.ActiveCharacter,
	)
	if err == nil {
		var charIDs []int
		for charRows.Next() {
			var cid int
			if charRows.Scan(&cid) == nil {
				charIDs = append(charIDs, cid)
			}
		}
		charRows.Close()
		for _, cid := range charIDs {
			res, _, err := Services.DeactivateCharacterInTx(tx, db, cid)
			if err != nil {
				return nil, nil, nil, 0, fmt.Errorf("failed to deactivate character %d: %w", cid, err)
			}
			charResults = append(charResults, res)
		}
	}

	var wipedCharCount int
	_ = db.QueryRow("SELECT COUNT(*) FROM character_base WHERE user_id = ?", userID).Scan(&wipedCharCount)

	if _, err := tx.Exec("UPDATE character_base SET user_id = 1 WHERE user_id = ?", userID); err != nil {
		return nil, nil, nil, 0, fmt.Errorf("failed to reassign characters: %w", err)
	}
	if _, err := tx.Exec("UPDATE character_profile_base SET user_id = 1 WHERE user_id = ?", userID); err != nil {
		return nil, nil, nil, 0, fmt.Errorf("failed to reassign character profiles: %w", err)
	}

	var wcResults []wantedCharDeactivationResult
	wcRows, err := db.Query(
		"SELECT id FROM wanted_character_base WHERE author_user_id = ? AND wanted_character_status = ?",
		userID, Entities.ActiveWantedCharacter,
	)
	if err == nil {
		var wcIDs []int
		for wcRows.Next() {
			var wcid int
			if wcRows.Scan(&wcid) == nil {
				wcIDs = append(wcIDs, wcid)
			}
		}
		wcRows.Close()
		for _, wcid := range wcIDs {
			res, _, err := deactivateWantedCharacterInTx(tx, db, wcid)
			if err != nil {
				return nil, nil, nil, 0, fmt.Errorf("failed to deactivate wanted character %d: %w", wcid, err)
			}
			wcResults = append(wcResults, res)
		}
	}

	if _, err := tx.Exec("UPDATE wanted_character_base SET author_user_id = 1 WHERE author_user_id = ?", userID); err != nil {
		return nil, nil, nil, 0, fmt.Errorf("failed to reassign wanted characters: %w", err)
	}

	if _, err := tx.Exec("DELETE FROM users WHERE id = ?", userID); err != nil {
		return nil, nil, nil, 0, fmt.Errorf("failed to delete user: %w", err)
	}

	return batches, charResults, wcResults, wipedCharCount, nil
}

// WipeOutMyUserRequest is the payload for self-service account deletion.
type WipeOutMyUserRequest struct {
	RecoveryCode string `json:"recovery_code" binding:"required"`
}

// WipeOutMyUser permanently deletes the caller's account after verifying a recovery code.
// The recovery code is double-hashed (frontend SHA-256 + backend SHA-256) before lookup,
// matching the storage scheme used by SaveKeys and SaveRecoveryKeys.
func WipeOutMyUser(c *gin.Context, db *sql.DB) {
	var req WipeOutMyUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	wipeHashBytes := sha256.Sum256([]byte(req.RecoveryCode))
	wipeHashHex := fmt.Sprintf("%x", wipeHashBytes)

	var codeID, userID int
	if err := db.QueryRow(
		"SELECT id, user_id FROM recovery_codes WHERE recovery_code = ? AND date_used IS NULL",
		wipeHashHex,
	).Scan(&codeID, &userID); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid or already used recovery code"})
		c.Abort()
		return
	}

	tx, err := db.Begin()
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to start transaction: " + err.Error()})
		c.Abort()
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec("UPDATE recovery_codes SET date_used = NOW() WHERE id = ?", codeID); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to mark recovery code as used: " + err.Error()})
		c.Abort()
		return
	}

	batches, charResults, wcResults, wipedCharCount, err := wipeUserInTx(tx, db, userID)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: err.Error()})
		c.Abort()
		return
	}

	if err := tx.Commit(); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to commit transaction: " + err.Error()})
		c.Abort()
		return
	}

	Websockets.MainHub.CloseUserConnections(userID)
	Services.ActivityStorage.RemoveUser(userID)

	c.JSON(http.StatusOK, gin.H{"message": "User account wiped"})

	Events.Publish(db, Events.UserWiped, Events.UserWipedEvent{CharacterCount: wipedCharCount})
	for _, b := range batches {
		Events.Publish(db, Events.GeneralPostsDeleted, Events.GeneralPostsDeletedEvent{
			TopicID:    b.TopicID,
			SubforumID: b.SubforumID,
			Count:      len(b.PostIDs),
			PostIDs:    b.PostIDs,
		})
	}
	for _, r := range charResults {
		Services.PublishCharacterDeactivatedEvents(db, r)
	}
	for _, r := range wcResults {
		publishWantedCharDeactivatedEvents(db, r)
	}
}

// AdminWipeUser performs an admin-triggered account wipe without requiring a recovery code.
func AdminWipeUser(c *gin.Context, db *sql.DB) {
	userID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid user ID"})
		c.Abort()
		return
	}

	var exists bool
	if err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE id = ?)", userID).Scan(&exists); err != nil || !exists {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "User not found"})
		c.Abort()
		return
	}

	tx, err := db.Begin()
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to start transaction: " + err.Error()})
		c.Abort()
		return
	}
	defer tx.Rollback()

	batches, charResults, wcResults, wipedCharCount, err := wipeUserInTx(tx, db, userID)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: err.Error()})
		c.Abort()
		return
	}

	if err := tx.Commit(); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to commit transaction: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "User account deleted"})

	Events.Publish(db, Events.UserWiped, Events.UserWipedEvent{CharacterCount: wipedCharCount})
	for _, b := range batches {
		Events.Publish(db, Events.GeneralPostsDeleted, Events.GeneralPostsDeletedEvent{
			TopicID:    b.TopicID,
			SubforumID: b.SubforumID,
			Count:      len(b.PostIDs),
			PostIDs:    b.PostIDs,
		})
	}
	for _, r := range charResults {
		Services.PublishCharacterDeactivatedEvents(db, r)
	}
	for _, r := range wcResults {
		publishWantedCharDeactivatedEvents(db, r)
	}
}

// VerifyAdminResetCodeRequest is the payload for validating the admin-issued PIN.
type VerifyAdminResetCodeRequest struct {
	Username string `json:"username" binding:"required"`
	Pin      string `json:"pin" binding:"required"`
}

// AdminResetPassword generates a one-time 6-digit PIN, clears the user's password,
// and deactivates all existing public/private keys. The PIN is returned to the admin
// in plaintext once and must be communicated out-of-band to the user.
func AdminResetPassword(c *gin.Context, db *sql.DB) {
	userID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid user ID"})
		c.Abort()
		return
	}

	pinBytes := make([]byte, 4)
	if _, err := rand.Read(pinBytes); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to generate PIN"})
		c.Abort()
		return
	}
	pinInt := int(pinBytes[0])<<24 | int(pinBytes[1])<<16 | int(pinBytes[2])<<8 | int(pinBytes[3])
	if pinInt < 0 {
		pinInt = -pinInt
	}
	pin := fmt.Sprintf("%06d", pinInt%1000000)

	hashBytes := sha256.Sum256([]byte(pin))
	pinHash := hex.EncodeToString(hashBytes[:])

	tx, err := db.Begin()
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to start transaction"})
		c.Abort()
		return
	}
	defer tx.Rollback()

	if _, err = tx.Exec("UPDATE users SET password = '' WHERE id = ?", userID); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to reset password"})
		c.Abort()
		return
	}
	if _, err = tx.Exec("UPDATE private_keys SET is_active = false WHERE user_id = ?", userID); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to deactivate private keys"})
		c.Abort()
		return
	}
	if _, err = tx.Exec("UPDATE public_keys SET is_active = false WHERE user_id = ?", userID); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to deactivate public keys"})
		c.Abort()
		return
	}
	if _, err = tx.Exec(
		"INSERT INTO admin_password_resets (user_id, pin_hash) VALUES (?, ?)",
		userID, pinHash,
	); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to store reset record"})
		c.Abort()
		return
	}

	if err = tx.Commit(); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to commit transaction"})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"pin": pin})
}

// VerifyAdminResetCode validates the PIN issued by AdminResetPassword and issues a
// one-time security code. The atomic UPDATE guards against replay: only one caller
// can win the race to set security_code from NULL.
func VerifyAdminResetCode(c *gin.Context, db *sql.DB) {
	var req VerifyAdminResetCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	var userID int
	if err := db.QueryRow("SELECT id FROM users WHERE username = ?", req.Username).Scan(&userID); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Invalid credentials"})
		c.Abort()
		return
	}

	hashBytes := sha256.Sum256([]byte(req.Pin))
	pinHash := hex.EncodeToString(hashBytes[:])

	var resetID int
	if err := db.QueryRow(
		"SELECT id FROM admin_password_resets WHERE user_id = ? AND pin_hash = ? AND used_at IS NULL",
		userID, pinHash,
	).Scan(&resetID); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Invalid or expired PIN"})
		c.Abort()
		return
	}

	securityCodeBytes := make([]byte, 32)
	if _, err := rand.Read(securityCodeBytes); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to generate security code"})
		c.Abort()
		return
	}
	securityCode := hex.EncodeToString(securityCodeBytes)

	res, err := db.Exec(
		"UPDATE admin_password_resets SET security_code = ? WHERE id = ? AND security_code IS NULL AND used_at IS NULL",
		securityCode, resetID,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to verify PIN"})
		c.Abort()
		return
	}
	affected, err := res.RowsAffected()
	if err != nil || affected != 1 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusConflict, Message: "PIN already used"})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"security_code": securityCode})
}

// CompletePasswordResetRequest is the payload for the final step of admin-initiated password reset.
type CompletePasswordResetRequest struct {
	SecurityCode        string                   `json:"security_code" binding:"required"`
	Password            string                   `json:"password" binding:"required"`
	PrivateKey          SaveKeysPrivateKeyItem   `json:"private_key" binding:"required"`
	RecoveryPrivateKeys []SaveKeysPrivateKeyItem `json:"recovery_private_keys" binding:"required"`
	PublicKey           Entities.PublicKey       `json:"public_key" binding:"required"`
	Codes               []string                 `json:"codes" binding:"required"`
}

// CompletePasswordReset finishes the admin-reset flow: sets the new password, stores fresh
// RSA keys and recovery codes, and auto-logs the user in by returning JWT tokens.
func CompletePasswordReset(c *gin.Context, db *sql.DB) {
	var req CompletePasswordResetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	if len(req.Codes) != len(req.RecoveryPrivateKeys) {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "codes and recovery_private_keys must have the same length"})
		c.Abort()
		return
	}

	var userID int
	if err := db.QueryRow(
		"SELECT user_id FROM admin_password_resets WHERE security_code = ? AND used_at IS NULL",
		req.SecurityCode,
	).Scan(&userID); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Invalid or expired security code"})
		c.Abort()
		return
	}

	var user Entities.User
	userQuery := "SELECT id, username, avatar, interface_language, interface_timezone, interface_font_size, user_status, interface_design, signature, editor_type, do_not_blur FROM users WHERE id = ?"
	if err := db.QueryRow(userQuery, userID).Scan(
		&user.Id, &user.Username, &user.Avatar, &user.InterfaceLanguage, &user.InterfaceTimezone,
		&user.InterfaceFontSize, &user.UserStatus, &user.InterfaceDesign, &user.Signature, &user.EditorType, &user.DoNotBlur,
	); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to load user"})
		c.Abort()
		return
	}

	var tempUser Entities.User
	tempUser.Password = req.Password
	if err := tempUser.HashPassword(req.Password); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to hash password"})
		c.Abort()
		return
	}

	tx, err := db.Begin()
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to start transaction"})
		c.Abort()
		return
	}
	defer tx.Rollback()

	if _, err = tx.Exec("UPDATE users SET password = ? WHERE id = ?", tempUser.Password, userID); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to set password"})
		c.Abort()
		return
	}

	if _, err = tx.Exec(
		"UPDATE admin_password_resets SET used_at = NOW() WHERE security_code = ?",
		req.SecurityCode,
	); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to mark reset as used"})
		c.Abort()
		return
	}

	if _, err = tx.Exec(
		"INSERT INTO private_keys (user_id, private_key, salt, iv, recovery_code_id, is_active) VALUES (?, ?, ?, ?, NULL, true)",
		userID, req.PrivateKey.PrivateKey, req.PrivateKey.Salt, req.PrivateKey.IV,
	); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to save private key"})
		c.Abort()
		return
	}

	for i, code := range req.Codes {
		codeHashBytes := sha256.Sum256([]byte(code))
		codeHashHex := fmt.Sprintf("%x", codeHashBytes)

		codeRes, err := tx.Exec("INSERT INTO recovery_codes (user_id, recovery_code) VALUES (?, ?)", userID, codeHashHex)
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to save recovery code"})
			c.Abort()
			return
		}
		codeID, err := codeRes.LastInsertId()
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get recovery code ID"})
			c.Abort()
			return
		}

		pk := req.RecoveryPrivateKeys[i]
		if _, err = tx.Exec(
			"INSERT INTO private_keys (user_id, private_key, salt, iv, recovery_code_id, is_active) VALUES (?, ?, ?, ?, ?, false)",
			userID, pk.PrivateKey, pk.Salt, pk.IV, codeID,
		); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to save recovery private key"})
			c.Abort()
			return
		}
	}

	if _, err = tx.Exec("INSERT INTO public_keys (user_id, public_key, is_active) VALUES (?, ?, true)", userID, req.PublicKey.PublicKey); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to save public key"})
		c.Abort()
		return
	}

	if err = tx.Commit(); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to commit transaction"})
		c.Abort()
		return
	}

	rolesQuery := `
		SELECT r.id, r.name
		FROM roles r
		INNER JOIN user_role ur ON r.id = ur.role_id
		WHERE ur.user_id = ?`
	roleRows, err := db.Query(rolesQuery, userID)
	if err == nil {
		defer roleRows.Close()
		user.Roles = []Entities.Role{}
		for roleRows.Next() {
			var role Entities.Role
			if err := roleRows.Scan(&role.Id, &role.Name); err == nil {
				role.Permissions = Services.GetRoleFrontendPermissions(role.Id, db)
				user.Roles = append(user.Roles, role)
			}
		}
		if Services.IsSuperuserID(user.Id) {
			user.Roles = append(user.Roles, Entities.Role{Id: 0, Name: "superadmin", Permissions: []string{"show_admin_backup", "show_admin_ai_models"}})
			user.IsSuperuser = true
		}
	}

	expirationTime := time.Now().Add(24 * time.Hour)
	claims := &Middlewares.Claims{
		Username: user.Username,
		UserID:   user.Id,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expirationTime),
			Issuer:    "cuento-backend",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(Middlewares.JwtKey)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to generate token"})
		c.Abort()
		return
	}

	refreshExpirationTime := time.Now().Add(7 * 24 * time.Hour)
	refreshClaims := &Middlewares.Claims{
		Username: user.Username,
		UserID:   user.Id,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(refreshExpirationTime),
			Issuer:    "cuento-backend",
		},
	}
	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	refreshTokenString, err := refreshToken.SignedString(Middlewares.JwtKey)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to generate refresh token"})
		c.Abort()
		return
	}

	user.Password = ""
	user.NotificationSettings = []Entities.UserNotificationSetting{}

	c.JSON(http.StatusOK, gin.H{
		"access_token":  tokenString,
		"refresh_token": refreshTokenString,
		"user":          user,
	})
}
