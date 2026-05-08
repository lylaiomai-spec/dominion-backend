package Services

import (
	"database/sql"
	"log"
)

var (
	userRefreshSender    func(userID int)
	userConnectedChecker func(userID int) bool
)

func SetUserRefreshSender(fn func(userID int)) {
	userRefreshSender = fn
}

func SetUserConnectedChecker(fn func(userID int) bool) {
	userConnectedChecker = fn
}

// NotifyUserRefresh sends a user_refresh_required WS message immediately if the user
// is connected, otherwise persists the request so it is delivered on next connect.
func NotifyUserRefresh(userID int, db *sql.DB) {
	if userConnectedChecker != nil && userConnectedChecker(userID) {
		if userRefreshSender != nil {
			userRefreshSender(userID)
		}
		return
	}
	if _, err := db.Exec(
		"INSERT INTO pending_user_refresh (user_id) VALUES (?) ON DUPLICATE KEY UPDATE queued_at = CURRENT_TIMESTAMP",
		userID,
	); err != nil {
		log.Printf("user_refresh_queue: failed to queue refresh for user %d: %v", userID, err)
	}
}

// NotifyUsersRefresh notifies multiple users, each queued individually if offline.
func NotifyUsersRefresh(userIDs []int, db *sql.DB) {
	for _, id := range userIDs {
		NotifyUserRefresh(id, db)
	}
}

// DrainUserRefreshQueue checks for a pending refresh for the given user, delivers it,
// and removes the record. Should be called when a user establishes a WS connection.
func DrainUserRefreshQueue(userID int, db *sql.DB) {
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM pending_user_refresh WHERE user_id = ?", userID).Scan(&count); err != nil || count == 0 {
		return
	}
	if _, err := db.Exec("DELETE FROM pending_user_refresh WHERE user_id = ?", userID); err != nil {
		log.Printf("user_refresh_queue: failed to clear refresh for user %d: %v", userID, err)
		return
	}
	if userRefreshSender != nil {
		userRefreshSender(userID)
	}
}
