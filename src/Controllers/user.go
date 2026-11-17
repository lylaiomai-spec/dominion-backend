package Controllers

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Events"
	"cuento-backend/src/Features"
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"cuento-backend/src/Websockets"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// Credentials holds the username/password pair for login.
type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// RefreshTokenRequest is the payload for token refresh.
type RefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

// UserProfileResponse is the public profile payload returned by GetUserProfile.
type UserProfileResponse struct {
	UserId                    int                        `json:"user_id"`
	Username                  string                     `json:"username"`
	Avatar                    *string                    `json:"avatar"`
	RegistrationDate          time.Time                  `json:"registration_date"`
	RegistrationDateLocalized string                     `json:"registration_date_localized"`
	TotalPosts                int                        `json:"total_posts"`
	TotalGeneralPosts         int                        `json:"total_general_posts"`
	Characters                []CharacterProfileListItem `json:"characters"`
	Masks                     []MaskProfileListItem      `json:"masks"`
	CurrencyAmount            *int                       `json:"currency_amount"`
	UserStatus                Entities.UserStatus        `json:"user_status"`
	ArchiveDate               *time.Time                 `json:"archive_date"`
	ArchiveReason             *string                    `json:"archive_reason"`
}

// MaskProfileListItem is a summary of a mask shown on the user profile page.
type MaskProfileListItem struct {
	Id                   int        `json:"id"`
	Name                 *string    `json:"name"`
	Avatar               *string    `json:"avatar"`
	TotalEpisodes        int        `json:"total_episodes"`
	TotalPosts           int        `json:"total_posts"`
	DateLastPost         *time.Time `json:"date_last_post"`
	DateLastPostLocalized *string   `json:"date_last_post_localized,omitempty"`
}

// CharacterProfileListItem is a summary of a character shown on the user profile page.
type CharacterProfileListItem struct {
	Id                    int                      `json:"id"`
	Name                  string                   `json:"name"`
	TotalEpisodes         int                      `json:"total_episodes"`
	TotalPosts            int                      `json:"total_posts"`
	LastPostDate          *time.Time               `json:"last_post_date"`
	LastPostDateLocalized *string                  `json:"last_post_date_localized,omitempty"`
	CharacterStatus       Entities.CharacterStatus `json:"character_status"`
	Factions              []Entities.Faction       `json:"factions"`
}

// UpdateSettingsRequest is the payload for the user settings update endpoint.
type UpdateSettingsRequest struct {
	Avatar          *string        `json:"avatar"`
	Timezone        *string        `json:"interface_timezone"`
	Language        *string        `json:"interface_language"`
	FontSize        *float64       `json:"interface_font_size"`
	Password        *string        `json:"password"`
	InterfaceDesign NullableString `json:"interface_design"`
	Signature       NullableString `json:"signature"`
	EditorType      *int           `json:"editor_type"`
	DoNotBlur       *bool          `json:"do_not_blur"`
}

// CreateUserRequest is the payload for admin-created user accounts.
type CreateUserRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	Language string `json:"interface_language,omitempty"`
}

// UserListItem is a user entry in the user-facing list, including active characters.
type UserListItem struct {
	Id            int                       `json:"id"`
	Username      string                    `json:"username"`
	Characters    []Entities.ShortCharacter `json:"characters"`
	UserStatus    Entities.UserStatus       `json:"user_status"`
	ArchiveDate   *time.Time                `json:"archive_date"`
	ArchiveReason *string                   `json:"archive_reason"`
}

// Register creates a new user account via the self-registration form.
// Hashes the password, inserts the user, and assigns the default "user" role.
func Register(c *gin.Context, db *sql.DB) {
	var user Entities.User
	if err := c.ShouldBindJSON(&user); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	if err := user.HashPassword(user.Password); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to hash password: " + err.Error()})
		c.Abort()
		return
	}

	defaultLang := "en-US"
	if user.InterfaceLanguage != nil && *user.InterfaceLanguage != "" {
		defaultLang = *user.InterfaceLanguage
	}
	defaultTZ := "Europe/London"
	if user.InterfaceTimezone != nil && *user.InterfaceTimezone != "" {
		defaultTZ = *user.InterfaceTimezone
	}

	query := "INSERT INTO users (username, password, date_registered, interface_language, interface_timezone) VALUES (?, ?, ?, ?, ?)"
	res, err := db.Exec(query, user.Username, user.Password, time.Now(), defaultLang, defaultTZ)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create user: " + err.Error()})
		c.Abort()
		return
	}

	id, err := res.LastInsertId()
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get user Id: " + err.Error()})
		c.Abort()
		return
	}
	user.Id = int(id)
	user.InterfaceLanguage = &defaultLang
	user.InterfaceTimezone = &defaultTZ

	var defaultRoleID int
	err = db.QueryRow("SELECT id FROM roles WHERE name = ?", "user").Scan(&defaultRoleID)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get default role: " + err.Error()})
		c.Abort()
		return
	}

	_, err = db.Exec("INSERT INTO user_role (user_id, role_id) VALUES (?, ?)", user.Id, defaultRoleID)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to assign role to user: " + err.Error()})
		c.Abort()
		return
	}

	user.Password = ""
	user.Roles = []Entities.Role{{Id: defaultRoleID, Name: "user"}}

	Events.Publish(db, Events.UserRegistered, Events.UserRegisteredEvent{
		UserID:   user.Id,
		Username: user.Username,
	})

	c.JSON(http.StatusCreated, gin.H{"user": user})
}

// CreateUser creates a user account on behalf of an admin, skipping the self-registration flow.
func CreateUser(c *gin.Context, db *sql.DB) {
	var req CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	var user Entities.User
	user.Username = req.Username
	user.Password = req.Password
	if err := user.HashPassword(user.Password); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to hash password: " + err.Error()})
		c.Abort()
		return
	}

	defaultLang := "en-US"
	if req.Language != "" {
		defaultLang = req.Language
	}
	defaultTZ := "Europe/London"

	res, err := db.Exec("INSERT INTO users (username, password, date_registered, interface_language, interface_timezone) VALUES (?, ?, ?, ?, ?)",
		user.Username, user.Password, time.Now(), defaultLang, defaultTZ)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to create user: " + err.Error()})
		c.Abort()
		return
	}

	id, err := res.LastInsertId()
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get user ID: " + err.Error()})
		c.Abort()
		return
	}

	var defaultRoleID int
	err = db.QueryRow("SELECT id FROM roles WHERE name = ?", "user").Scan(&defaultRoleID)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get default role: " + err.Error()})
		c.Abort()
		return
	}

	_, err = db.Exec("INSERT INTO user_role (user_id, role_id) VALUES (?, ?)", id, defaultRoleID)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to assign role to user: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":       id,
		"username": user.Username,
	})
}

// Login authenticates a username/password pair and returns JWT access and refresh tokens.
// Returns 401 with error "password_reset_required" if the account is pending an admin reset.
func Login(c *gin.Context, db *sql.DB) {
	var creds Credentials
	if err := c.ShouldBindJSON(&creds); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	var user Entities.User
	query := "SELECT id, username, avatar, password, interface_language, interface_timezone, interface_font_size, user_status, interface_design, archive_reason, signature, editor_type, do_not_blur FROM users WHERE username = ?"
	err := db.QueryRow(query, creds.Username).Scan(&user.Id, &user.Username, &user.Avatar, &user.Password, &user.InterfaceLanguage, &user.InterfaceTimezone, &user.InterfaceFontSize, &user.UserStatus, &user.InterfaceDesign, &user.ArchiveReason, &user.Signature, &user.EditorType, &user.DoNotBlur)
	if err != nil {
		if err == sql.ErrNoRows {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Invalid credentials"})
		} else {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Database error: " + err.Error()})
		}
		c.Abort()
		return
	}

	if user.Password == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "password_reset_required"})
		c.Abort()
		return
	}

	if err := user.CheckPassword(creds.Password); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Invalid credentials"})
		c.Abort()
		return
	}

	if user.UserStatus == Entities.ArchivedUser {
		c.JSON(http.StatusForbidden, gin.H{"error": "User is archived", "archive_reason": user.ArchiveReason})
		c.Abort()
		return
	}

	rolesQuery := `
		SELECT r.id, r.name
		FROM roles r
		INNER JOIN user_role ur ON r.id = ur.role_id
		WHERE ur.user_id = ?`
	rows, err := db.Query(rolesQuery, user.Id)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch user roles: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	user.Roles = []Entities.Role{}
	for rows.Next() {
		var role Entities.Role
		if err := rows.Scan(&role.Id, &role.Name); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan role: " + err.Error()})
			c.Abort()
			return
		}
		role.Permissions = Services.GetRoleFrontendPermissions(role.Id, db)
		user.Roles = append(user.Roles, role)
	}
	if Services.IsSuperuserID(user.Id) {
		user.Roles = append(user.Roles, Entities.Role{Id: 0, Name: "superadmin", Permissions: []string{"show_admin_backup", "show_admin_ai_models"}})
		user.IsSuperuser = true
	}

	if err := rows.Err(); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Error iterating roles: " + err.Error()})
		c.Abort()
		return
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
	settingsRows, err := db.Query(
		"SELECT notification_type, disable_toast, disable_sound, disable_all FROM user_notification_setting WHERE user_id = ?",
		user.Id,
	)
	if err == nil {
		defer settingsRows.Close()
		for settingsRows.Next() {
			var s Entities.UserNotificationSetting
			if err := settingsRows.Scan(&s.NotificationType, &s.DisableToast, &s.DisableSound, &s.DisableAll); err == nil {
				user.NotificationSettings = append(user.NotificationSettings, s)
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"access_token":  tokenString,
		"refresh_token": refreshTokenString,
		"user":          user,
	})
}

// RefreshToken exchanges a valid refresh token for new access and refresh tokens,
// re-fetching the user's current roles and status.
func RefreshToken(c *gin.Context, db *sql.DB) {
	var req RefreshTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	claims := &Middlewares.Claims{}
	token, err := jwt.ParseWithClaims(req.RefreshToken, claims, func(token *jwt.Token) (interface{}, error) {
		return Middlewares.JwtKey, nil
	})

	if err != nil || !token.Valid {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Invalid refresh token"})
		c.Abort()
		return
	}

	var user Entities.User
	query := "SELECT id, username, avatar, interface_language, interface_timezone, interface_font_size, user_status, total_posts, total_general_posts, interface_design, signature, editor_type, do_not_blur FROM users WHERE id = ?"
	err = db.QueryRow(query, claims.UserID).Scan(&user.Id, &user.Username, &user.Avatar, &user.InterfaceLanguage, &user.InterfaceTimezone, &user.InterfaceFontSize, &user.UserStatus, &user.TotalPosts, &user.TotalGeneralPosts, &user.InterfaceDesign, &user.Signature, &user.EditorType, &user.DoNotBlur)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch user details"})
		c.Abort()
		return
	}

	if user.UserStatus == Entities.ArchivedUser {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusForbidden, Message: "User is archived"})
		c.Abort()
		return
	}

	rolesQuery := `
		SELECT r.id, r.name
		FROM roles r
		INNER JOIN user_role ur ON r.id = ur.role_id
		WHERE ur.user_id = ?`
	rows, err := db.Query(rolesQuery, user.Id)
	if err == nil {
		defer rows.Close()
		user.Roles = []Entities.Role{}
		for rows.Next() {
			var role Entities.Role
			if err := rows.Scan(&role.Id, &role.Name); err == nil {
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
	newClaims := &Middlewares.Claims{
		Username: user.Username,
		UserID:   user.Id,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expirationTime),
			Issuer:    "cuento-backend",
		},
	}

	newToken := jwt.NewWithClaims(jwt.SigningMethodHS256, newClaims)
	newTokenString, err := newToken.SignedString(Middlewares.JwtKey)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to generate new access token"})
		c.Abort()
		return
	}

	refreshExpirationTime := time.Now().Add(7 * 24 * time.Hour)
	newRefreshClaims := &Middlewares.Claims{
		Username: user.Username,
		UserID:   user.Id,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(refreshExpirationTime),
			Issuer:    "cuento-backend",
		},
	}
	newRefreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, newRefreshClaims)
	newRefreshTokenString, err := newRefreshToken.SignedString(Middlewares.JwtKey)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to generate new refresh token"})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"access_token":  newTokenString,
		"refresh_token": newRefreshTokenString,
		"user":          user,
	})
}

// GetUsersByPage returns the list of authenticated users currently browsing a given page.
func GetUsersByPage(c *gin.Context, db *sql.DB) {
	pageType := c.Param("page_type")
	pageId := c.Param("page_id")

	activeUsers := Services.ActivityStorage.GetUsersOnPage(pageType, pageId)

	var shortUsers []Entities.ShortUser
	for _, u := range activeUsers {
		shortUsers = append(shortUsers, Entities.ShortUser{
			Id:       u.UserID,
			Username: u.Username,
		})
	}

	if shortUsers == nil {
		shortUsers = []Entities.ShortUser{}
	}

	c.JSON(http.StatusOK, shortUsers)
}

// GetUserProfile returns public profile data for a user, including characters, masks,
// and optional currency balance. Registration date is localized to the viewer's timezone.
func GetUserProfile(c *gin.Context, db *sql.DB) {
	userIDStr := c.Param("userID")
	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid user ID"})
		c.Abort()
		return
	}

	var profile UserProfileResponse
	err = db.QueryRow("SELECT id, username, avatar, date_registered, total_posts, total_general_posts, user_status, archive_date, archive_reason FROM users WHERE id = ?", userID).Scan(&profile.UserId, &profile.Username, &profile.Avatar, &profile.RegistrationDate, &profile.TotalPosts, &profile.TotalGeneralPosts, &profile.UserStatus, &profile.ArchiveDate, &profile.ArchiveReason)
	if err != nil {
		if err == sql.ErrNoRows {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "User not found"})
		} else {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get user details: " + err.Error()})
		}
		c.Abort()
		return
	}

	profile.RegistrationDateLocalized = Services.LocalizeDateOnly(c, profile.RegistrationDate)

	charRows, err := db.Query("SELECT cb.id, cb.name, cb.total_episodes, cb.total_posts, cb.date_last_post, cb.character_status FROM character_base cb LEFT JOIN topics t ON cb.topic_id = t.id WHERE cb.user_id = ? AND (cb.topic_id IS NULL OR t.status != ?)", userID, Entities.DeletedTopic)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get user characters: " + err.Error()})
		c.Abort()
		return
	}
	defer charRows.Close()

	for charRows.Next() {
		var char CharacterProfileListItem
		if err := charRows.Scan(&char.Id, &char.Name, &char.TotalEpisodes, &char.TotalPosts, &char.LastPostDate, &char.CharacterStatus); err != nil {
			continue
		}
		if char.LastPostDate != nil {
			s := Services.LocalizeTime(c, *char.LastPostDate)
			char.LastPostDateLocalized = &s
		}

		factionRows, err := db.Query("SELECT f.id, f.name FROM factions f JOIN character_faction cf ON f.id = cf.faction_id WHERE cf.character_id = ? AND f.level = 0", char.Id)
		if err == nil {
			var factions []Entities.Faction
			for factionRows.Next() {
				var faction Entities.Faction
				if err := factionRows.Scan(&faction.Id, &faction.Name); err == nil {
					factions = append(factions, faction)
				}
			}
			char.Factions = factions
			factionRows.Close()
		}

		profile.Characters = append(profile.Characters, char)
	}

	if profile.Characters == nil {
		profile.Characters = []CharacterProfileListItem{}
	}

	// mask_stats is keyed per user (not per mask), so stats are computed from source tables.
	maskRows, err := db.Query(`
		SELECT
			cpb.id,
			cpb.mask_name,
			cpb.avatar,
			(SELECT COUNT(*) FROM episode_mask em WHERE em.mask_id = cpb.id) AS total_episodes,
			(SELECT COUNT(*) FROM posts p
				WHERE p.character_profile_id = cpb.id
				  AND (p.is_deleted IS NULL OR p.is_deleted != 1)) AS total_posts,
			(SELECT MAX(p.date_created) FROM posts p
				WHERE p.character_profile_id = cpb.id
				  AND (p.is_deleted IS NULL OR p.is_deleted != 1)) AS date_last_post
		FROM character_profile_base cpb
		WHERE cpb.user_id = ? AND cpb.is_mask = 1
		  AND (cpb.is_archived IS NULL OR cpb.is_archived = 0)
	`, userID)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get user masks: " + err.Error()})
		c.Abort()
		return
	}
	defer maskRows.Close()

	for maskRows.Next() {
		var mask MaskProfileListItem
		if err := maskRows.Scan(&mask.Id, &mask.Name, &mask.Avatar, &mask.TotalEpisodes, &mask.TotalPosts, &mask.DateLastPost); err != nil {
			continue
		}
		if mask.DateLastPost != nil {
			s := Services.LocalizeTime(c, *mask.DateLastPost)
			mask.DateLastPostLocalized = &s
		}
		profile.Masks = append(profile.Masks, mask)
	}

	if profile.Masks == nil {
		profile.Masks = []MaskProfileListItem{}
	}

	if Features.IsCurrencyActive(db) {
		var amount int
		err := db.QueryRow("SELECT amount FROM currency_user_account WHERE user_id = ?", userID).Scan(&amount)
		if err == nil {
			profile.CurrencyAmount = &amount
		} else if err == sql.ErrNoRows {
			zero := 0
			profile.CurrencyAmount = &zero
		}
	}

	c.JSON(http.StatusOK, profile)
}

// UploadUserAvatar saves a new avatar image for the authenticated user.
func UploadUserAvatar(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		c.Abort()
		return
	}

	file, _, err := c.Request.FormFile("avatar")
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "avatar file is required"})
		c.Abort()
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to read uploaded file"})
		c.Abort()
		return
	}

	avatarURL, err := Services.SaveUserAvatar(data, userID, db)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: err.Error()})
		c.Abort()
		return
	}

	if _, err := db.Exec("UPDATE users SET avatar = ? WHERE id = ?", avatarURL, userID); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to save avatar"})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"avatar": avatarURL})
}

// UpdateSettings applies partial updates to the authenticated user's profile and preferences.
// Returns the full updated user object including roles.
func UpdateSettings(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		c.Abort()
		return
	}

	var req UpdateSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	var updates []string
	var args []interface{}

	if req.Avatar != nil {
		updates = append(updates, "avatar = ?")
		args = append(args, *req.Avatar)
	}
	if req.Timezone != nil {
		updates = append(updates, "interface_timezone = ?")
		args = append(args, *req.Timezone)
	}
	if req.Language != nil {
		updates = append(updates, "interface_language = ?")
		args = append(args, *req.Language)
	}
	if req.FontSize != nil {
		updates = append(updates, "interface_font_size = ?")
		args = append(args, *req.FontSize)
	}
	if req.InterfaceDesign.IsSet {
		updates = append(updates, "interface_design = ?")
		args = append(args, req.InterfaceDesign.Value)
	}
	if req.Signature.IsSet {
		updates = append(updates, "signature = ?")
		args = append(args, req.Signature.Value)
	}
	if req.EditorType != nil {
		updates = append(updates, "editor_type = ?")
		args = append(args, *req.EditorType)
	}
	if req.DoNotBlur != nil {
		updates = append(updates, "do_not_blur = ?")
		args = append(args, *req.DoNotBlur)
	}
	if req.Password != nil {
		dummyUser := Entities.User{}
		if err := dummyUser.HashPassword(*req.Password); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to hash password"})
			c.Abort()
			return
		}
		updates = append(updates, "password = ?")
		args = append(args, dummyUser.Password)
	}

	if len(updates) == 0 {
		c.JSON(http.StatusOK, gin.H{"message": "No changes to update"})
		return
	}

	query := fmt.Sprintf("UPDATE users SET %s WHERE id = ?", strings.Join(updates, ", "))
	args = append(args, userID)

	_, err := db.Exec(query, args...)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update settings: " + err.Error()})
		c.Abort()
		return
	}

	var user Entities.User
	err = db.QueryRow("SELECT id, username, avatar, interface_language, interface_timezone, interface_font_size, user_status, total_posts, total_general_posts, interface_design, signature, editor_type, do_not_blur FROM users WHERE id = ?", userID).Scan(&user.Id, &user.Username, &user.Avatar, &user.InterfaceLanguage, &user.InterfaceTimezone, &user.InterfaceFontSize, &user.UserStatus, &user.TotalPosts, &user.TotalGeneralPosts, &user.InterfaceDesign, &user.Signature, &user.EditorType, &user.DoNotBlur)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch updated user details"})
		c.Abort()
		return
	}

	rolesQuery := `
		SELECT r.id, r.name
		FROM roles r
		INNER JOIN user_role ur ON r.id = ur.role_id
		WHERE ur.user_id = ?`
	rows, err := db.Query(rolesQuery, user.Id)
	if err == nil {
		defer rows.Close()
		user.Roles = []Entities.Role{}
		for rows.Next() {
			var role Entities.Role
			if err := rows.Scan(&role.Id, &role.Name); err == nil {
				role.Permissions = Services.GetRoleFrontendPermissions(role.Id, db)
				user.Roles = append(user.Roles, role)
			}
		}
		if Services.IsSuperuserID(user.Id) {
			user.Roles = append(user.Roles, Entities.Role{Id: 0, Name: "superadmin", Permissions: []string{"show_admin_backup", "show_admin_ai_models"}})
			user.IsSuperuser = true
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Settings updated successfully",
		"user":    user,
	})
}

// SetDoNotBlurRequest is the payload for toggling the NSFW blur preference.
type SetDoNotBlurRequest struct {
	DoNotBlur bool `json:"do_not_blur"`
}

// SetDoNotBlur updates the NSFW image blur preference for the authenticated user.
func SetDoNotBlur(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)
	if userID == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusUnauthorized, Message: "Unauthorized"})
		c.Abort()
		return
	}

	var req SetDoNotBlurRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	if _, err := db.Exec("UPDATE users SET do_not_blur = ? WHERE id = ?", req.DoNotBlur, userID); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update setting: " + err.Error()})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"do_not_blur": req.DoNotBlur})
}

// AdminUserListItem is a user entry in the admin user list, including activity stats.
type AdminUserListItem struct {
	Id                      int        `json:"id"`
	Username                string     `json:"username"`
	UserStatus              int        `json:"user_status"`
	DateRegistered          *time.Time `json:"date_registered"`
	DateLastVisit           *time.Time `json:"date_last_visit"`
	CharacterCount          int        `json:"character_count"`
	LastGamePostDate        *time.Time `json:"last_game_post_date"`
	ArchiveDate             *time.Time `json:"archive_date"`
	ArchiveReason           *string    `json:"archive_reason"`
	DateRegisteredLocalized   *string  `json:"date_registered_localized,omitempty"`
	DateLastVisitLocalized    *string  `json:"date_last_visit_localized,omitempty"`
	LastGamePostDateLocalized *string  `json:"last_game_post_date_localized,omitempty"`
}

// GetAdminUserList returns all users with registration date, last visit, character count,
// and archive status. Excludes the system user (id = 1).
func GetAdminUserList(c *gin.Context, db *sql.DB) {
	rows, err := db.Query(`
		SELECT
			u.id,
			u.username,
			u.user_status,
			u.date_registered,
			u.date_last_visit,
			COUNT(c.id) AS character_count,
			MAX(c.date_last_post) AS last_game_post_date,
			u.archive_date,
			u.archive_reason
		FROM users u
		LEFT JOIN character_base c ON c.user_id = u.id
		WHERE u.id > 1
		GROUP BY u.id, u.username, u.user_status, u.date_registered, u.date_last_visit, u.archive_date, u.archive_reason
		ORDER BY u.username ASC
	`)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get users: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()


	users := []AdminUserListItem{}
	for rows.Next() {
		var u AdminUserListItem
		if err := rows.Scan(&u.Id, &u.Username, &u.UserStatus, &u.DateRegistered, &u.DateLastVisit, &u.CharacterCount, &u.LastGamePostDate, &u.ArchiveDate, &u.ArchiveReason); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan user: " + err.Error()})
			c.Abort()
			return
		}
		if u.DateRegistered != nil {
			s := Services.LocalizeTime(c, *u.DateRegistered)
			u.DateRegisteredLocalized = &s
		}
		if u.DateLastVisit != nil {
			s := Services.LocalizeTime(c, *u.DateLastVisit)
			u.DateLastVisitLocalized = &s
		}
		if u.LastGamePostDate != nil {
			s := Services.LocalizeTime(c, *u.LastGamePostDate)
			u.LastGamePostDateLocalized = &s
		}
		users = append(users, u)
	}

	c.JSON(http.StatusOK, users)
}

// GetUserList returns all active users ordered alphabetically, each with their active characters.
func GetUserList(c *gin.Context, db *sql.DB) {
	query := "SELECT id, username, user_status, archive_date, archive_reason FROM users WHERE user_status = 0 AND id > 1 ORDER BY username ASC"
	rows, err := db.Query(query)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch users: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	var users []UserListItem
	for rows.Next() {
		var user UserListItem
		if err := rows.Scan(&user.Id, &user.Username, &user.UserStatus, &user.ArchiveDate, &user.ArchiveReason); err != nil {
			continue
		}

		charQuery := "SELECT id, name FROM character_base WHERE user_id = ? AND character_status = 0 ORDER BY name ASC"
		charRows, err := db.Query(charQuery, user.Id)
		if err == nil {
			user.Characters = []Entities.ShortCharacter{}
			for charRows.Next() {
				var char Entities.ShortCharacter
				if err := charRows.Scan(&char.Id, &char.Name); err == nil {
					user.Characters = append(user.Characters, char)
				}
			}
			charRows.Close()
		}

		users = append(users, user)
	}

	if users == nil {
		users = []UserListItem{}
	}

	c.JSON(http.StatusOK, users)
}

// GetRecentActiveUsers returns users who visited within the last 24 hours, ordered alphabetically.
func GetRecentActiveUsers(c *gin.Context, db *sql.DB) {
	rows, err := db.Query(`
		SELECT id, username FROM users
		WHERE user_status = 0 AND id > 1
		AND date_last_visit >= NOW() - INTERVAL 24 HOUR
		ORDER BY username ASC
	`)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch recent users: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	users := []Entities.ShortUser{}
	for rows.Next() {
		var u Entities.ShortUser
		if err := rows.Scan(&u.Id, &u.Username); err != nil {
			continue
		}
		users = append(users, u)
	}

	c.JSON(http.StatusOK, users)
}

// GetRecentActiveCharacters returns characters whose owners visited within the last 24 hours.
func GetRecentActiveCharacters(c *gin.Context, db *sql.DB) {
	rows, err := db.Query(`
		SELECT c.id, c.name FROM character_base c
		JOIN users u ON u.id = c.user_id
		WHERE c.character_status = 0 AND u.user_status = 0 AND u.id > 1
		AND u.date_last_visit >= NOW() - INTERVAL 24 HOUR
		ORDER BY c.name ASC
	`)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch recent characters: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	characters := []Entities.ShortCharacter{}
	for rows.Next() {
		var ch Entities.ShortCharacter
		if err := rows.Scan(&ch.Id, &ch.Name); err != nil {
			continue
		}
		characters = append(characters, ch)
	}

	c.JSON(http.StatusOK, characters)
}

// ActiveUserInfo is one entry in the active-users activity list.
type ActiveUserInfo struct {
	UserID              int     `json:"user_id"`
	Username            string  `json:"username"`
	IsGuest             bool    `json:"is_guest,omitempty"`
	CurrentPageType     string  `json:"current_page_type"`
	CurrentPageId       *string `json:"current_page_id"`
	CurrentPageName     *string `json:"current_page_name"`
	LastActiveLocalized string  `json:"last_active"`
}

// buildActiveUserActivity assembles the active-user list for forUserID, applying subforum
// visibility rules so users cannot see who is reading subforums they cannot access.
// Topic names are only included when the viewer has read access to that subforum.
func buildActiveUserActivity(forUserID int, db *sql.DB) []ActiveUserInfo {
	locale := Services.GetUserLocale(forUserID, db)
	activeUsers := Services.ActivityStorage.GetActiveUsers()
	result := make([]ActiveUserInfo, 0, len(activeUsers))

	visibleSubforums, _ := Services.GetVisibleSubforums(forUserID, "subforum_read", db)
	visibleSubforumSet := make(map[int]bool, len(visibleSubforums))
	for _, id := range visibleSubforums {
		visibleSubforumSet[id] = true
	}

	for _, u := range activeUsers {
		info := ActiveUserInfo{
			UserID:              u.UserID,
			Username:            u.Username,
			CurrentPageType:     u.CurrentPageType,
			LastActiveLocalized: Services.LocalizeTimeForUser(u.LastActive, locale),
		}

		switch u.CurrentPageType {
		case "direct-chat":
			// never reveal chat id
		case "topic":
			var subforumID int
			var topicName string
			err := db.QueryRow("SELECT subforum_id, name FROM topics WHERE id = ?", u.CurrentPageId).Scan(&subforumID, &topicName)
			if err == nil && visibleSubforumSet[subforumID] {
				info.CurrentPageId = &u.CurrentPageId
				info.CurrentPageName = &topicName
			}
		default:
			info.CurrentPageId = &u.CurrentPageId
		}

		result = append(result, info)
	}

	for _, g := range Services.GuestActivity.GetActiveGuests() {
		info := ActiveUserInfo{
			IsGuest:             true,
			Username:            "Guest#" + g.ShortID,
			CurrentPageType:     g.CurrentPageType,
			LastActiveLocalized: Services.LocalizeTimeForUser(g.LastActive, locale),
		}
		if g.CurrentPageType == "topic" && g.CurrentPageId != "" {
			var subforumID int
			var topicName string
			err := db.QueryRow("SELECT subforum_id, name FROM topics WHERE id = ?", g.CurrentPageId).Scan(&subforumID, &topicName)
			if err == nil && visibleSubforumSet[subforumID] {
				info.CurrentPageId = &g.CurrentPageId
				info.CurrentPageName = &topicName
			}
		} else if g.CurrentPageType != "" && g.CurrentPageId != "" {
			info.CurrentPageId = &g.CurrentPageId
		}
		result = append(result, info)
	}

	return result
}

// BroadcastActiveUserActivity pushes updated activity data via WebSocket to every user
// currently viewing the active-users page.
func BroadcastActiveUserActivity(db *sql.DB) {
	viewers := Services.ActivityStorage.GetUsersOnPageType("active-users")
	if len(viewers) == 0 {
		return
	}
	for _, v := range viewers {
		Websockets.MainHub.SendNotification(v.UserID, map[string]interface{}{
			"type": "active_users_activity_update",
			"data": buildActiveUserActivity(v.UserID, db),
		})
	}
}

// GetActiveUserActivity returns the current active-user activity list for the caller.
func GetActiveUserActivity(c *gin.Context, db *sql.DB) {
	currentUserID := Services.GetUserIdFromContext(c)
	c.JSON(http.StatusOK, buildActiveUserActivity(currentUserID, db))
}

// UpdateGuestLocation records a guest's current page for the active-users display
// and broadcasts the updated list to all viewers.
func UpdateGuestLocation(c *gin.Context, db *sql.DB) {
	var req struct {
		PageType string `json:"page_type" binding:"required"`
		PageId   string `json:"page_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	fingerprint := Services.GuestFingerprint(c.Request)
	Services.GuestActivity.UpdateLocation(fingerprint, req.PageType, req.PageId)
	go BroadcastActiveUserActivity(db)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// GetUserRoles returns all roles assigned to a given user.
func GetUserRoles(c *gin.Context, db *sql.DB) {
	userID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid user ID"})
		c.Abort()
		return
	}

	rows, err := db.Query(`SELECT r.id, r.name FROM roles r INNER JOIN user_role ur ON r.id = ur.role_id WHERE ur.user_id = ?`, userID)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get user roles: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	roles := []Entities.Role{}
	for rows.Next() {
		var r Entities.Role
		if err := rows.Scan(&r.Id, &r.Name); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan role: " + err.Error()})
			c.Abort()
			return
		}
		roles = append(roles, r)
	}

	c.JSON(http.StatusOK, roles)
}

// UpdateUserRoles replaces all role assignments for a user in a single transaction
// and forces a token refresh so the new permissions take effect immediately.
func UpdateUserRoles(c *gin.Context, db *sql.DB) {
	var req struct {
		UserID  int   `json:"user_id" binding:"required"`
		RoleIDs []int `json:"role_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	tx, err := db.Begin()
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to start transaction: " + err.Error()})
		c.Abort()
		return
	}

	if _, err := tx.Exec("DELETE FROM user_role WHERE user_id = ?", req.UserID); err != nil {
		tx.Rollback()
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to clear user roles: " + err.Error()})
		c.Abort()
		return
	}

	for _, roleID := range req.RoleIDs {
		if _, err := tx.Exec("INSERT INTO user_role (user_id, role_id) VALUES (?, ?)", req.UserID, roleID); err != nil {
			tx.Rollback()
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to assign role: " + err.Error()})
			c.Abort()
			return
		}
	}

	if err := tx.Commit(); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to commit transaction: " + err.Error()})
		c.Abort()
		return
	}

	Services.NotifyUserRefresh(req.UserID, db)
	c.JSON(http.StatusOK, gin.H{"message": "User roles updated"})
}

// ArchiveAccount archives the authenticated user's own account and deactivates all characters.
func ArchiveAccount(c *gin.Context, db *sql.DB) {
	userID := Services.GetUserIdFromContext(c)

	tx, err := db.Begin()
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to start transaction"})
		c.Abort()
		return
	}
	defer tx.Rollback()

	result, err := tx.Exec(
		"UPDATE users SET user_status = ?, archive_date = ?, archive_reason = ? WHERE id = ?",
		Entities.ArchivedUser, time.Now(), "User's decision", userID,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to archive account: " + err.Error()})
		c.Abort()
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "User not found"})
		c.Abort()
		return
	}

	var archiveActiveCharIDs []int
	if archiveRows, err := db.Query(
		"SELECT id FROM character_base WHERE user_id = ? AND character_status = ?",
		userID, Entities.ActiveCharacter,
	); err == nil {
		for archiveRows.Next() {
			var cid int
			if archiveRows.Scan(&cid) == nil {
				archiveActiveCharIDs = append(archiveActiveCharIDs, cid)
			}
		}
		archiveRows.Close()
	}

	_, err = tx.Exec("UPDATE character_base SET character_status = ? WHERE user_id = ?", Entities.InactiveCharacter, userID)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to deactivate characters: " + err.Error()})
		c.Abort()
		return
	}

	if err := tx.Commit(); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to commit transaction"})
		c.Abort()
		return
	}

	for _, cid := range archiveActiveCharIDs {
		Events.Publish(db, Events.CharacterStatusChanged, Events.CharacterStatusChangedEvent{
			CharacterID: cid,
			UserID:      userID,
			OldStatus:   Entities.ActiveCharacter,
			NewStatus:   Entities.InactiveCharacter,
		})
	}

	c.JSON(http.StatusOK, gin.H{"user_status": Entities.ArchivedUser})
}

// ReactivateUser clears archive status and reactivates a user. Admin only.
func ReactivateUser(c *gin.Context, db *sql.DB) {
	userID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid user ID"})
		c.Abort()
		return
	}

	result, err := db.Exec(
		"UPDATE users SET user_status = ?, archive_date = NULL, archive_reason = NULL WHERE id = ?",
		Entities.ActiveUser, userID,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to reactivate user: " + err.Error()})
		c.Abort()
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "User not found"})
		c.Abort()
		return
	}

	c.JSON(http.StatusOK, gin.H{"user_status": Entities.ActiveUser})
}

// BanUser archives a user with an optional reason, deactivates their characters,
// and forces a token refresh. Admin only.
func BanUser(c *gin.Context, db *sql.DB) {
	userID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid user ID"})
		c.Abort()
		return
	}

	var body struct {
		Reason *string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&body)

	tx, err := db.Begin()
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to start transaction"})
		c.Abort()
		return
	}
	defer tx.Rollback()

	result, err := tx.Exec(
		"UPDATE users SET user_status = ?, archive_date = ?, archive_reason = ? WHERE id = ?",
		Entities.ArchivedUser, time.Now(), body.Reason, userID,
	)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to ban user: " + err.Error()})
		c.Abort()
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "User not found"})
		c.Abort()
		return
	}

	var banActiveCharIDs []int
	if banRows, err := db.Query(
		"SELECT id FROM character_base WHERE user_id = ? AND character_status = ?",
		userID, Entities.ActiveCharacter,
	); err == nil {
		for banRows.Next() {
			var cid int
			if banRows.Scan(&cid) == nil {
				banActiveCharIDs = append(banActiveCharIDs, cid)
			}
		}
		banRows.Close()
	}

	_, err = tx.Exec("UPDATE character_base SET character_status = ? WHERE user_id = ?", Entities.InactiveCharacter, userID)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to deactivate characters: " + err.Error()})
		c.Abort()
		return
	}

	if err := tx.Commit(); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to commit transaction"})
		c.Abort()
		return
	}

	for _, cid := range banActiveCharIDs {
		Events.Publish(db, Events.CharacterStatusChanged, Events.CharacterStatusChangedEvent{
			CharacterID: cid,
			UserID:      userID,
			OldStatus:   Entities.ActiveCharacter,
			NewStatus:   Entities.InactiveCharacter,
		})
	}

	Services.NotifyUserRefresh(userID, db)
	c.JSON(http.StatusOK, gin.H{"user_status": Entities.ArchivedUser})
}

// AdminUpdateUserRequest is the payload for admin edits to a user's account.
type AdminUpdateUserRequest struct {
	Username *string `json:"username"`
	Avatar   *string `json:"avatar"`
}

// AdminUpdateUser allows an admin to change a user's username or avatar.
// Syncs the in-memory activity store and forces a token refresh on username change.
func AdminUpdateUser(c *gin.Context, db *sql.DB) {
	userID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid user ID"})
		c.Abort()
		return
	}

	var req AdminUpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid request body: " + err.Error()})
		c.Abort()
		return
	}

	var updates []string
	var args []interface{}

	if req.Username != nil {
		updates = append(updates, "username = ?")
		args = append(args, *req.Username)
	}
	if req.Avatar != nil {
		updates = append(updates, "avatar = ?")
		args = append(args, *req.Avatar)
	}

	if len(updates) == 0 {
		c.JSON(http.StatusOK, gin.H{"message": "No changes to update"})
		return
	}

	query := fmt.Sprintf("UPDATE users SET %s WHERE id = ?", strings.Join(updates, ", "))
	args = append(args, userID)

	result, err := db.Exec(query, args...)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update user: " + err.Error()})
		c.Abort()
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusNotFound, Message: "User not found"})
		c.Abort()
		return
	}

	if req.Username != nil {
		Services.ActivityStorage.UpdateUsername(userID, *req.Username)
	}
	Services.NotifyUserRefresh(userID, db)
	c.JSON(http.StatusOK, gin.H{"message": "User updated successfully"})
}

// UserAutocomplete returns up to 10 users matching a username search term.
// If episode_id is provided, results are limited to participants of that episode.
func UserAutocomplete(c *gin.Context, db *sql.DB) {
	term := c.Param("term")

	var rows *sql.Rows
	var err error

	if episodeIDStr := c.Query("episode_id"); episodeIDStr != "" {
		episodeID, convErr := strconv.Atoi(episodeIDStr)
		if convErr != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid episode_id"})
			c.Abort()
			return
		}
		rows, err = db.Query(`
			SELECT DISTINCT u.id, u.username
			FROM users u
			JOIN character_base cb ON cb.user_id = u.id
			JOIN episode_character ec ON ec.character_id = cb.id
			WHERE ec.episode_id = ? AND u.username LIKE ? AND u.user_status = 0
			LIMIT 10`,
			episodeID, "%"+term+"%",
		)
	} else {
		rows, err = db.Query(
			"SELECT id, username FROM users WHERE username LIKE ? AND user_status = 0 LIMIT 10",
			"%"+term+"%",
		)
	}

	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get users: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	users := []Entities.ShortUser{}
	for rows.Next() {
		var u Entities.ShortUser
		if err := rows.Scan(&u.Id, &u.Username); err != nil {
			continue
		}
		users = append(users, u)
	}

	c.JSON(http.StatusOK, users)
}
