package Services

import "database/sql"

type dbExecer interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
}

// AddTopicActivityLog records a topic or episode status change in topic_activity_log.
// Pass userID=0 to store a NULL user_id (e.g. admin or system actions).
func AddTopicActivityLog(db dbExecer, userID int, topicID int64, event string, oldState, newState int) {
	var uid interface{}
	if userID != 0 {
		uid = userID
	}
	_, _ = db.Exec(
		"INSERT INTO topic_activity_log (user_id, topic_id, event, old_state, new_state) VALUES (?, ?, ?, ?, ?)",
		uid, topicID, event, oldState, newState,
	)
}
