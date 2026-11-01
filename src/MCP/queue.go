package MCP

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Services"
	"cuento-backend/src/Websockets"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"
)

// --- Queue worker ---

// workerNotify is signalled whenever a new task is enqueued.
// Buffered so the enqueuer never blocks.
var workerNotify = make(chan struct{}, 1)

// notifyWorker wakes the queue worker without blocking.
func notifyWorker() {
	select {
	case workerNotify <- struct{}{}:
	default:
	}
}

// NotifyWorker is the exported equivalent, used by Services to wake the worker
// when a non-chat task (e.g. embedding) is enqueued.
func NotifyWorker() { notifyWorker() }

// StartQueueWorker starts the background AI task processor.
// It resets any tasks stuck in 'processing' from a previous run, then
// enters a loop driven by notifyWorker() signals and a periodic fallback ticker.
func StartQueueWorker(db *sql.DB) {
	go func() {
		// Reset tasks that were left in 'processing' by a previous crash.
		if _, err := db.Exec(
			`UPDATE ai_task_queue SET status = 'pending', date_started = NULL WHERE status = 'processing'`,
		); err != nil {
			log.Printf("AI queue: failed to reset stuck tasks: %v", err)
		}

		// Process anything that was already pending.
		processAllPending(db)

		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-workerNotify:
				processAllPending(db)
			case <-ticker.C:
				processAllPending(db)
			}
		}
	}()
}

// processAllPending drains the pending queue one task at a time.
func processAllPending(db *sql.DB) {
	for {
		ok, err := processNextTask(db)
		if err != nil {
			log.Printf("AI queue worker error: %v", err)
			return
		}
		if !ok {
			return
		}
	}
}

// processNextTask claims and executes one pending task.
// Returns (true, nil) if a task was processed, (false, nil) if the queue is empty.
func processNextTask(db *sql.DB) (bool, error) {
	if getActivePool() == nil {
		return false, nil
	}

	// Atomically claim the oldest pending task.
	res, err := db.Exec(
		`UPDATE ai_task_queue
		 SET status = 'processing', date_started = NOW()
		 WHERE id = (
		     SELECT id FROM (
		         SELECT id FROM ai_task_queue WHERE status = 'pending' ORDER BY date_created ASC LIMIT 1
		     ) t
		 )`,
	)
	if err != nil {
		return false, fmt.Errorf("claiming task: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, nil // queue empty
	}

	var taskID int
	var userID sql.NullInt64
	var taskType string
	err = db.QueryRow(
		`SELECT id, user_id, COALESCE(type, 'chat') FROM ai_task_queue WHERE status = 'processing' ORDER BY date_started DESC LIMIT 1`,
	).Scan(&taskID, &userID, &taskType)
	if err != nil {
		return false, fmt.Errorf("fetching claimed task: %w", err)
	}

	switch taskType {
	case "embedding":
		executeEmbeddingQueueTask(db, taskID)
	case "GameDigest":
		executeGameDigestTask(db, taskID)
	default:
		executeChatTask(db, taskID, int(userID.Int64))
	}
	return true, nil
}

// markFailed marks a task as failed and logs the error.
func markFailed(db *sql.DB, taskID, userID int, errMsg string) {
	log.Printf("AI queue: task %d (user %d) failed: %s", taskID, userID, errMsg)
	_, _ = db.Exec(
		`UPDATE ai_task_queue SET status = 'failed', error = ?, date_completed = NOW() WHERE id = ?`,
		errMsg, taskID,
	)
}

// pushError sends an error notification to the user over WebSocket.
func pushError(userID int, errMsg string) {
	Websockets.MainHub.SendNotification(userID, map[string]interface{}{
		"type":  "ai_error",
		"error": errMsg,
	})
}

// --- Subscriber list ---

type aiSubscriber struct {
	userID int
	taskID int
}

var (
	aiSubscribersMu   sync.Mutex
	aiSubscribersList []aiSubscriber
)

// AddAISubscriber registers a user to receive queue position updates.
// Called when a task is enqueued with at least one task ahead of it.
func AddAISubscriber(userID, taskID int) {
	aiSubscribersMu.Lock()
	defer aiSubscribersMu.Unlock()
	aiSubscribersList = append(aiSubscribersList, aiSubscriber{userID: userID, taskID: taskID})
}

// notifySubscribersOnCompletion is called after a task finishes.
// It delivers ai_task_done to the user whose task just completed (if they were
// a subscriber) and sends updated queue positions to all other subscribers.
// Returns true if the completed user was a subscriber.
func notifySubscribersOnCompletion(db *sql.DB, completedTaskID, completedUserID int, msg Entities.AIChatMessage) bool {
	aiSubscribersMu.Lock()

	wasSubscriber := false
	remaining := make([]aiSubscriber, 0, len(aiSubscribersList))
	var toUpdate []aiSubscriber

	for _, sub := range aiSubscribersList {
		if sub.taskID == completedTaskID {
			wasSubscriber = true
			// Removed from list — do not add to remaining.
		} else {
			remaining = append(remaining, sub)
			toUpdate = append(toUpdate, sub)
		}
	}
	aiSubscribersList = remaining
	aiSubscribersMu.Unlock()

	// Deliver the answer to the subscriber whose task just finished.
	if wasSubscriber {
		Websockets.MainHub.SendNotification(completedUserID, map[string]interface{}{
			"type": "ai_task_done",
			"data": msg,
		})
	}

	// Send updated positions to the remaining subscribers.
	for _, sub := range toUpdate {
		sendQueuePosition(db, sub)
	}

	return wasSubscriber
}

// notifyQueuePositions sends position updates to all current subscribers.
// Called after a task fails so the queue still advances.
func notifyQueuePositions(db *sql.DB, completedTaskID int) {
	aiSubscribersMu.Lock()
	snapshot := make([]aiSubscriber, len(aiSubscribersList))
	copy(snapshot, aiSubscribersList)
	aiSubscribersMu.Unlock()

	for _, sub := range snapshot {
		sendQueuePosition(db, sub)
	}
}

// executeEmbeddingQueueTask processes one embedding task from ai_task_queue.
func executeEmbeddingQueueTask(db *sql.DB, taskID int) {
	var payloadStr string
	if err := db.QueryRow(`SELECT COALESCE(payload, '{}') FROM ai_task_queue WHERE id = ?`, taskID).Scan(&payloadStr); err != nil {
		markFailed(db, taskID, 0, "failed to read payload: "+err.Error())
		return
	}

	var payload struct {
		Bucket   string `json:"bucket"`
		EntityID int64  `json:"entity_id"`
	}
	if err := json.Unmarshal([]byte(payloadStr), &payload); err != nil || payload.Bucket == "" {
		markFailed(db, taskID, 0, "invalid embedding payload")
		return
	}

	if err := Services.ExecuteEmbeddingTask(payload.Bucket, payload.EntityID, db); err != nil {
		markFailed(db, taskID, 0, err.Error())
		return
	}

	_, _ = db.Exec(`UPDATE ai_task_queue SET status = 'done', date_completed = NOW() WHERE id = ?`, taskID)
}

// sendQueuePosition queries the current position of a subscriber's task and
// pushes an ai_queue_position WebSocket event.
func sendQueuePosition(db *sql.DB, sub aiSubscriber) {
	var pos int
	_ = db.QueryRow(
		`SELECT COUNT(*) FROM ai_task_queue WHERE id < ? AND status IN ('pending', 'processing')`,
		sub.taskID,
	).Scan(&pos)
	Websockets.MainHub.SendNotification(sub.userID, map[string]interface{}{
		"type": "ai_queue_position",
		"data": map[string]int{"position": pos},
	})
}
