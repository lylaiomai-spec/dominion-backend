package MCP

import (
	"context"
	"cuento-backend/src/Entities"
	"cuento-backend/src/Services"
	"cuento-backend/src/Websockets"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
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
	if activeAgent == nil {
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

	if taskType == "embedding" {
		executeEmbeddingQueueTask(db, taskID)
	} else {
		executeTask(db, taskID, int(userID.Int64))
	}
	return true, nil
}

// executeTask runs the AI chat for one queued task and delivers the result.
func executeTask(db *sql.DB, taskID, userID int) {
	ctx := context.Background()

	history, err := loadChatHistory(userID, db)
	if err != nil {
		markFailed(db, taskID, userID, "failed to load history: "+err.Error())
		return
	}

	systemInstruction := fmt.Sprintf(
		"You are a helpful assistant for a forum community. "+
			"The current user's ID is %d. Use this when calling tools that require a user_id.\n\n"+
			"CRITICAL RULES — follow these without exception:\n"+
			"1. Before answering ANY question about the community, its characters, factions, lore, topics, or posts, "+
			"you MUST call the appropriate search or lookup tool first. Never answer from memory alone.\n"+
			"2. Base your answer ONLY on what the tools return. Do not add, infer, or extrapolate facts "+
			"that are not explicitly present in the tool results.\n"+
			"3. If the tools return no relevant results, respond honestly: say you could not find any "+
			"information on that topic in the forum and suggest the user try different search terms.\n"+
			"4. Never invent character names, faction names, lore details, usernames, post contents, "+
			"or any other community-specific facts.\n"+
			"5. Never include post IDs, topic IDs, or any other raw technical identifiers in your response text.\n"+
			"6. Always respond in the same language the user wrote their message in.",
		userID,
	)

	const maxRetries = 4
	var replyText string
	var sources []ChatSource
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		replyText, sources, lastErr = activeAgent.Chat(ctx, history, systemInstruction)
		if lastErr == nil {
			break
		}

		is429 := strings.Contains(lastErr.Error(), "429") || strings.Contains(lastErr.Error(), "quota")
		if !is429 || attempt == maxRetries {
			markFailed(db, taskID, userID, lastErr.Error())
			pushError(userID, lastErr.Error())
			notifyQueuePositions(db, taskID)
			return
		}

		// Rate-limited: record the retry and pause before the next attempt.
		_, _ = db.Exec(`UPDATE ai_task_queue SET retries = retries + 1 WHERE id = ?`, taskID)
		log.Printf("AI queue: task %d rate-limited (attempt %d/%d), retrying in 30s", taskID, attempt+1, maxRetries)
		time.Sleep(30 * time.Second)
	}

	replyText = Services.MarkdownToHTML(replyText)

	// Deduplicate sources by (PostID, TopicID) before saving.
	sources = uniqueSources(sources)

	// Serialize sources.
	var sourcesJSON []byte
	if len(sources) > 0 {
		sourcesJSON, _ = json.Marshal(sources)
	}

	// Save assistant reply.
	var replyID int64
	r, err := db.Exec(
		"INSERT INTO ai_chat_messages (user_id, role, content, sources, date_created) VALUES (?, 'assistant', ?, ?, NOW())",
		userID, replyText, sourcesJSON,
	)
	if err == nil {
		replyID, _ = r.LastInsertId()
	}

	// Mark task done.
	_, _ = db.Exec(
		`UPDATE ai_task_queue SET status = 'done', date_completed = NOW() WHERE id = ?`,
		taskID,
	)

	// Build the reply entity.
	entitySources := make([]Entities.AISource, len(sources))
	for i, s := range sources {
		entitySources[i] = Entities.AISource{
			PostID:    s.PostID,
			TopicID:   s.TopicID,
			TopicName: s.TopicName,
			TopicType: s.TopicType,
		}
	}
	msg := Entities.AIChatMessage{
		Id:          int(replyID),
		UserId:      userID,
		Role:        "assistant",
		Content:     replyText,
		Sources:     entitySources,
		DateCreated: time.Now(),
	}

	// Notify subscribers and determine how to deliver the reply.
	wasSubscriber := notifySubscribersOnCompletion(db, taskID, userID, msg)

	if !wasSubscriber {
		// Immediate response: deliver as a regular chat message.
		Websockets.MainHub.SendNotification(userID, map[string]interface{}{
			"type": "ai_message",
			"data": msg,
		})
	}
}

// loadChatHistory fetches the last 20 user/assistant messages after the most
// recent 'clear' marker for the given user.
func loadChatHistory(userID int, db *sql.DB) ([]ChatMessage, error) {
	rows, err := db.Query(
		`SELECT role, content FROM (
		     SELECT role, content, date_created FROM ai_chat_messages
		     WHERE user_id = ?
		       AND role != 'clear'
		       AND date_created > COALESCE(
		             (SELECT MAX(date_created) FROM ai_chat_messages WHERE user_id = ? AND role = 'clear'),
		             '1970-01-01'
		           )
		     ORDER BY date_created DESC LIMIT 20
		 ) sub ORDER BY date_created ASC`,
		userID, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []ChatMessage
	for rows.Next() {
		var msg ChatMessage
		if err := rows.Scan(&msg.Role, &msg.Content); err != nil {
			continue
		}
		history = append(history, msg)
	}
	return history, nil
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

// uniqueSources removes duplicate ChatSource entries, keeping the first
// occurrence of each (PostID, TopicID) pair.
func uniqueSources(sources []ChatSource) []ChatSource {
	type key struct {
		postID  int64
		topicID int64
	}
	seen := make(map[key]bool, len(sources))
	out := sources[:0]
	for _, s := range sources {
		var k key
		if s.PostID != nil {
			k.postID = *s.PostID
		}
		if s.TopicID != nil {
			k.topicID = *s.TopicID
		}
		if !seen[k] {
			seen[k] = true
			out = append(out, s)
		}
	}
	return out
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
