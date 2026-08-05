package Services

import (
	"database/sql"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// superuserID is read once from the environment at startup.
var superuserID = func() int {
	if v := os.Getenv("SUPERUSER_ID"); v != "" {
		if id, err := strconv.Atoi(v); err == nil {
			return id
		}
	}
	return 0
}()

// IsSuperuser returns true if the currently authenticated user is the superuser.
func IsSuperuser(c *gin.Context) bool {
	return superuserID != 0 && GetUserIdFromContext(c) == superuserID
}

// LocalizeTime converts t into the user's IANA timezone and returns a formatted string.
// Falls back to UTC if the timezone is nil, empty, or unrecognised.
func LocalizeTime(t time.Time, timezone *string) string {
	loc := time.UTC
	if timezone != nil && *timezone != "" {
		if l, err := time.LoadLocation(*timezone); err == nil {
			loc = l
		}
	}
	return t.In(loc).Format("2006-01-02 15:04:05")
}

// GetUserTimezone fetches the interface_timezone for a given user from the DB.
// Returns nil if the user is a guest (userID == 0) or the value is not set.
func GetUserTimezone(userID int, db *sql.DB) *string {
	if userID == 0 {
		return nil
	}
	var tz sql.NullString
	if err := db.QueryRow("SELECT interface_timezone FROM users WHERE id = ?", userID).Scan(&tz); err != nil {
		return nil
	}
	if !tz.Valid {
		return nil
	}
	return &tz.String
}

// IsFeatureEnabled checks whether a named feature flag is active in the Gin context,
// as populated by FeatureFlagsMiddleware.
func IsFeatureEnabled(flagName string, c *gin.Context) bool {
	flagMap, _ := c.Get("features")
	flags, _ := flagMap.(map[string]bool)
	return flags[flagName]
}

// GetUserIdFromContext safely retrieves the user ID from the Gin context.
// It returns the user ID if it exists and is a valid integer.
// It returns 0 if the user ID does not exist or is not a valid type,
// indicating a guest user or an unauthenticated session.
func GetUserIdFromContext(c *gin.Context) int {
	if id, exists := c.Get("user_id"); exists {
		if userID, ok := id.(int); ok {
			return userID
		}
	}
	return 0
}
