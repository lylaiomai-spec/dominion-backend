package Controllers

import (
	"cuento-backend/src/MCP"
	"database/sql"
	"encoding/json"
)

// GameDigest enqueues a game digest generation task.
// The instructions and execution logic live in src/MCP/game_digest.go.
func GameDigest(implementationID int, db *sql.DB) (interface{}, error) {
	payload, _ := json.Marshal(map[string]int{"implementation_id": implementationID})

	res, err := db.Exec(
		`INSERT INTO ai_task_queue (type, payload, status, date_created) VALUES ('GameDigest', ?, 'pending', NOW())`,
		string(payload),
	)
	if err != nil {
		return nil, err
	}

	taskID, _ := res.LastInsertId()

	var position int
	_ = db.QueryRow(
		`SELECT COUNT(*) FROM ai_task_queue WHERE status IN ('pending', 'processing') AND id < ?`,
		taskID,
	).Scan(&position)

	MCP.NotifyWorker()

	return map[string]interface{}{
		"task_id":        taskID,
		"queue_position": position,
	}, nil
}
