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
	"time"
)

// chatSystemInstruction builds the system prompt for AI chat tasks.
func chatSystemInstruction(userID int) string {
	return fmt.Sprintf(
		"You are a helpful assistant for a forum community. "+
			"The current user's ID is %d. Use this when calling tools that require a user_id.\n\n"+
			"SEARCH STRATEGY — follow these steps in order, each at most once:\n"+
			"Step 1. READ THE MESSAGE FOR EXPLICIT FILTERS before doing anything else. "+
			"Check whether the user has directly named a faction, character, episode, or subforum area. "+
			"If they named a faction → call get_factions once and resolve it to a faction_id; "+
			"apply that faction_id to all subsequent searches for characters and episodes. "+
			"If they named a specific character → target only the 'characters' bucket. "+
			"If they named a specific episode → target only the 'episodes' and 'game_posts' buckets. "+
			"If they named a subforum or area → note it as a context hint for picking relevant buckets. "+
			"If nothing is explicitly named, proceed without pre-set filters.\n"+
			"Step 2. Identify the core intent of the question.\n"+
			"Step 3. Extract 2–4 focused keyword phrases. Use specific names or short terms, not the full message.\n"+
			"Step 4. Call search_content once per keyword phrase, applying any filters determined in Step 1.\n"+
			"Step 5. FACTION DETECTION — only if no faction was identified in Step 1, and the question involves characters or episodes: "+
			"call get_factions once. Read through your Step 4 results and decide — "+
			"without any additional tool calls — whether the content clearly belongs to one of those factions. "+
			"If yes, call search_content once more per relevant bucket with that faction_id set. "+
			"If no faction is obvious, skip this step entirely.\n"+
			"Step 6. STOP searching. Do not loop back. "+
			"From all results collected so far, keep only entries that directly answer the question.\n"+
			"Step 7. Call cite_sources once with the 'id' values (as integers) of the specific entries you actually drew facts from. "+
			"Do NOT include entries you retrieved but did not use in your answer. If nothing was relevant, pass an empty array.\n"+
			"Step 8. Write your answer based on those entries. "+
			"If nothing relevant was found, say so and suggest different search terms.\n\n"+
			"HARD RULES:\n"+
			"1. Never answer community questions from memory — always search first.\n"+
			"2. Never invent or extrapolate character names, faction names, lore, usernames, or post contents.\n"+
			"3. Never include post IDs, topic IDs, or any raw technical identifiers in your response.\n"+
			"4. Always respond in the same language the user wrote their message in.\n"+
			"5. Never include raw links, post IDs, topic IDs, or URL-style references in your text response. "+
			"Links are rendered by the UI from cite_sources calls. You may naturally mention where something was found "+
			"(e.g. 'according to their character sheet', 'as noted in the episode entry') but do not output any IDs or URLs.",
		userID,
	)
}

// executeChatTask runs the AI chat pipeline for one queued task and delivers the result.
func executeChatTask(db *sql.DB, taskID, userID int) {
	ctx := context.Background()

	client, err := getActivePool().ClientForMinSizeAndProtocol(Entities.AIModelSizeSmall, Entities.AIModelTypeText, Entities.AIModelProtocolOpenAI)
	if err != nil {
		markFailed(db, taskID, userID, "no AI model available: "+err.Error())
		return
	}

	// Read session_id and message_id from task payload.
	var payloadStr string
	_ = db.QueryRow(`SELECT COALESCE(payload, '{}') FROM ai_task_queue WHERE id = ?`, taskID).Scan(&payloadStr)
	var taskPayload struct {
		SessionID string `json:"session_id"`
		MessageID int64  `json:"message_id"`
	}
	_ = json.Unmarshal([]byte(payloadStr), &taskPayload)

	// Load the specific user message that triggered this task.
	userMsg, err := loadMessageByID(taskPayload.MessageID, db)
	if err != nil {
		markFailed(db, taskID, userID, "failed to load message: "+err.Error())
		return
	}

	// Append the new user message to the session and snapshot the history.
	sess := getOrCreateChatSession(taskPayload.SessionID)
	sess.mu.Lock()
	sess.messages = append(sess.messages, userMsg)
	history := make([]ChatMessage, len(sess.messages))
	copy(history, sess.messages)
	sess.mu.Unlock()

	var replyText string
	var sources []ChatSource

	err = withModelRetry(
		getActivePool(),
		Entities.AIModelSizeSmall, Entities.AIModelTypeText,
		client,
		func(c AIClient) error {
			var e error
			replyText, sources, e = NewAIAgent(c).Chat(ctx, history, chatSystemInstruction(userID))
			return e
		},
		func(modelID string) {
			_, _ = db.Exec(`UPDATE ai_task_queue SET retries = retries + 1 WHERE id = ?`, taskID)
			log.Printf("AI queue: task %d rate-limited on %s, trying next model", taskID, modelID)
		},
	)
	if err != nil {
		markFailed(db, taskID, userID, err.Error())
		pushError(userID, err.Error())
		notifyQueuePositions(db, taskID)
		return
	}

	replyText = Services.MarkdownToHTML(replyText)

	// Append assistant reply to the session.
	sess.mu.Lock()
	sess.messages = append(sess.messages, ChatMessage{Role: "assistant", Content: replyText})
	sess.mu.Unlock()

	// Deduplicate sources by PostID before saving.
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
		Websockets.MainHub.SendNotification(userID, map[string]interface{}{
			"type": "ai_message",
			"data": msg,
		})
	}
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
