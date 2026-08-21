package MCP

import (
	"context"
	"cuento-backend/src/Entities"
	"cuento-backend/src/Events"
	"cuento-backend/src/Services"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

const gameDigestInstructions = `You are the Game Digest agent for a roleplay forum. Your task is to read all the posts of one episode and write a single cohesive summary for that episode, formatted in BB tags.

You will receive:
- In-world header fields (date, time, location, and other metadata set by the players)
- A previous summary describing what happened in this episode before the current period
- All new posts written during the current period, each with the character name and content

Your task:
1. Read all the new posts together. Ignore any out-of-character content marked with [OOC], (OOC), or similar tags.
2. Synthesize the posts into one unified picture of what happened in this episode during the period.
3. Use the previous summary as backstory context — do not repeat events already covered there.
4. Write in the voice of an in-world chronicler: immersive, present-tense, as if reporting from within the game world.
5. Format the output with BB tags:
   [b]Location — In-world date[/b] as the header (use the header fields if available, otherwise the episode name)
   [list][*] one bullet per meaningful story beat — not per post [/list]
   A "story beat" is a turning point, decision, conflict, discovery, or encounter that has the potential to affect the global plot — a shift in power, an alliance formed or broken, a secret revealed, a threat emerging, a critical choice made. Skip small personal moments, idle conversation, or routine actions that have no broader consequences.
   You may also merge events from different posts into a single bullet if — and only if — they share the same in-world date, time, and faction. Do not merge events that differ in any of these three dimensions.
   Keep each bullet to one–three sentences: vivid but concise.
6. If there are no new meaningful events, return only: [i]No new events recorded for this episode.[/i]

Rules:
- Synthesize across posts — do not write one bullet per post.
- Do not mention usernames, post counts, or any out-of-character details.
- Do not invent events — only summarize what the posts actually describe.
- Do not call any tools. All the data you need is provided in this message.

Return only the BB-formatted section text, nothing else.`

type gameDigestConfig struct {
	Period        string `json:"period"`
	SubforumIDs   []int  `json:"subforum_ids"`
	FactionIDs    []int  `json:"faction_ids"`
	TargetTopicID *int   `json:"target_topic_id"`
	Language      string `json:"language"`
}

type digestEpisode struct {
	EpisodeID    int64
	TopicID      int64
	TopicName    string
	HeaderFields []digestHeaderField
	Posts        []digestPost
	PrevContext  string
}

type digestHeaderField struct {
	MachineName string
	Value       string
}

type digestPost struct {
	AuthorName  string
	Content     string
	DateCreated time.Time
}

func executeGameDigestTask(db *sql.DB, taskID int) {
	ctx := context.Background()

	if activeAgent == nil {
		markFailed(db, taskID, 0, "AI agent not configured")
		return
	}

	// Load payload
	var payloadStr string
	if err := db.QueryRow(`SELECT COALESCE(payload, '{}') FROM ai_task_queue WHERE id = ?`, taskID).Scan(&payloadStr); err != nil {
		markFailed(db, taskID, 0, "failed to read payload: "+err.Error())
		return
	}
	var payload struct {
		ImplementationID int `json:"implementation_id"`
	}
	if err := json.Unmarshal([]byte(payloadStr), &payload); err != nil || payload.ImplementationID == 0 {
		markFailed(db, taskID, 0, "invalid payload")
		return
	}

	// Load config
	var configStr sql.NullString
	if err := db.QueryRow(`SELECT config FROM ai_agent_implementation WHERE id = ?`, payload.ImplementationID).Scan(&configStr); err != nil {
		markFailed(db, taskID, 0, "implementation not found")
		return
	}
	var config gameDigestConfig
	if configStr.Valid {
		_ = json.Unmarshal([]byte(configStr.String), &config)
	}
	if config.Period == "" {
		config.Period = "weekly"
	}

	since := periodToTime(config.Period)

	// Find matching episodes with new posts
	episodes, err := findDigestEpisodes(db, config, since, payload.ImplementationID)
	if err != nil {
		markFailed(db, taskID, 0, "failed to find episodes: "+err.Error())
		return
	}
	if len(episodes) == 0 {
		_, _ = db.Exec(`UPDATE ai_task_queue SET status = 'done', date_completed = NOW() WHERE id = ?`, taskID)
		return
	}

	// Generate one digest section per episode and update context
	var sections []string
	for i := range episodes {
		ep := &episodes[i]
		section, err := generateEpisodeDigestSection(ctx, ep, config.Language)
		if err != nil {
			log.Printf("game_digest: episode %d: %v", ep.EpisodeID, err)
			continue
		}
		sections = append(sections, section)
		updateDigestContext(db, payload.ImplementationID, ep.TopicID, section)
	}

	if len(sections) == 0 {
		_, _ = db.Exec(`UPDATE ai_task_queue SET status = 'done', date_completed = NOW() WHERE id = ?`, taskID)
		return
	}

	// Post combined digest to target topic
	if config.TargetTopicID != nil {
		fullDigest := strings.Join(sections, "\n\n")
		res, err := db.Exec(
			`INSERT INTO posts (topic_id, author_user_id, content, date_created, use_character_profile) VALUES (?, 1, ?, NOW(), 0)`,
			*config.TargetTopicID, fullDigest,
		)
		if err == nil {
			postID, _ := res.LastInsertId()
			var subforumID int
			_ = db.QueryRow(`SELECT subforum_id FROM topics WHERE id = ?`, *config.TargetTopicID).Scan(&subforumID)
			if fullPost, err := Services.GetPostById(int(postID), db, false); err == nil {
				Events.Publish(db, Events.PostCreated, Events.PostCreatedEvent{
					Type:       "post_created",
					TopicID:    int64(*config.TargetTopicID),
					SubforumID: subforumID,
					Post:       *fullPost,
				})
			}
		}
	}

	_, _ = db.Exec(`UPDATE ai_task_queue SET status = 'done', date_completed = NOW() WHERE id = ?`, taskID)
}

// findDigestEpisodes returns all episode topics that have new posts since `since`,
// filtered by the subforum and faction lists in the config.
func findDigestEpisodes(db *sql.DB, config gameDigestConfig, since time.Time, implementationID int) ([]digestEpisode, error) {
	query := `
		SELECT eb.id, t.id, t.name
		FROM episode_base eb
		JOIN topics t ON t.id = eb.topic_id
		WHERE t.status != ?
		  AND t.type = ?
		  AND EXISTS (
		      SELECT 1 FROM posts p
		      WHERE p.topic_id = t.id
		        AND p.date_created >= ?
		        AND COALESCE(p.is_deleted, 0) != 1
		  )`

	args := []interface{}{Entities.DeletedTopic, Entities.EpisodeTopic, since}

	if len(config.SubforumIDs) > 0 {
		ph := make([]string, len(config.SubforumIDs))
		for i, id := range config.SubforumIDs {
			ph[i] = "?"
			args = append(args, id)
		}
		query += " AND t.subforum_id IN (" + strings.Join(ph, ",") + ")"
	}

	if len(config.FactionIDs) > 0 {
		ph := make([]string, len(config.FactionIDs))
		for i, id := range config.FactionIDs {
			ph[i] = "?"
			args = append(args, id)
		}
		query += " AND EXISTS (" +
			"SELECT 1 FROM episode_character ec " +
			"JOIN character_faction cf ON cf.character_id = ec.character_id " +
			"WHERE ec.episode_id = eb.id AND cf.faction_id IN (" + strings.Join(ph, ",") + "))"
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var episodes []digestEpisode
	for rows.Next() {
		var ep digestEpisode
		if err := rows.Scan(&ep.EpisodeID, &ep.TopicID, &ep.TopicName); err != nil {
			continue
		}
		episodes = append(episodes, ep)
	}

	for i := range episodes {
		ep := &episodes[i]
		ep.HeaderFields = loadEpisodeHeaderFields(db, ep.EpisodeID)
		ep.Posts = loadNewPosts(db, ep.TopicID, since)
		ep.PrevContext = loadDigestContext(db, implementationID, ep.TopicID)
	}

	return episodes, nil
}

func loadEpisodeHeaderFields(db *sql.DB, episodeID int64) []digestHeaderField {
	rows, err := db.Query(`
		SELECT field_machine_name,
		       COALESCE(value_string, value_text, CAST(value_int AS CHAR), value_date, '') as value
		FROM episode_main
		WHERE entity_id = ? AND field_machine_name IS NOT NULL AND (
		      value_string IS NOT NULL OR value_text IS NOT NULL OR
		      value_int IS NOT NULL OR value_date IS NOT NULL
		)`, episodeID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var fields []digestHeaderField
	for rows.Next() {
		var f digestHeaderField
		if err := rows.Scan(&f.MachineName, &f.Value); err == nil && f.Value != "" {
			fields = append(fields, f)
		}
	}
	return fields
}

func loadNewPosts(db *sql.DB, topicID int64, since time.Time) []digestPost {
	rows, err := db.Query(`
		SELECT p.content, p.date_created,
		       COALESCE(cp.mask_name, cb.name, u.username, 'Unknown') as author_name
		FROM posts p
		LEFT JOIN users u ON u.id = p.author_user_id
		LEFT JOIN character_profile_base cp ON cp.id = p.character_profile_id
		LEFT JOIN character_base cb ON cb.id = cp.character_id
		WHERE p.topic_id = ?
		  AND p.date_created >= ?
		  AND COALESCE(p.is_deleted, 0) != 1
		ORDER BY p.date_created ASC`, topicID, since)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var posts []digestPost
	for rows.Next() {
		var p digestPost
		if err := rows.Scan(&p.Content, &p.DateCreated, &p.AuthorName); err == nil {
			posts = append(posts, p)
		}
	}
	return posts
}

func loadDigestContext(db *sql.DB, implementationID int, topicID int64) string {
	var ctx sql.NullString
	_ = db.QueryRow(
		`SELECT context_text FROM game_digest_context WHERE implementation_id = ? AND topic_id = ?`,
		implementationID, topicID,
	).Scan(&ctx)
	if ctx.Valid {
		return ctx.String
	}
	return ""
}

func updateDigestContext(db *sql.DB, implementationID int, topicID int64, contextText string) {
	_, _ = db.Exec(`
		INSERT INTO game_digest_context (implementation_id, topic_id, context_text, date_updated)
		VALUES (?, ?, ?, NOW())
		ON DUPLICATE KEY UPDATE context_text = VALUES(context_text), date_updated = NOW()`,
		implementationID, topicID, contextText,
	)
}

// generateEpisodeDigestSection calls the AI to produce one BB-tagged digest section for an episode.
func generateEpisodeDigestSection(ctx context.Context, ep *digestEpisode, language string) (string, error) {
	var sb strings.Builder

	if language != "" {
		sb.WriteString(fmt.Sprintf("Output language: %s\n\n", language))
	}

	sb.WriteString(fmt.Sprintf("## Episode: %s\n\n", ep.TopicName))

	if len(ep.HeaderFields) > 0 {
		sb.WriteString("### In-world header fields:\n")
		for _, f := range ep.HeaderFields {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", f.MachineName, f.Value))
		}
		sb.WriteString("\n")
	}

	if ep.PrevContext != "" {
		sb.WriteString("### Previous summary (backstory):\n")
		sb.WriteString(ep.PrevContext)
		sb.WriteString("\n\n")
	} else {
		sb.WriteString("### Previous summary (backstory):\nNo previous events recorded.\n\n")
	}

	sb.WriteString("### New posts this period:\n\n")
	for _, p := range ep.Posts {
		sb.WriteString(fmt.Sprintf("**%s** (%s):\n%s\n\n---\n\n",
			p.AuthorName,
			p.DateCreated.Format("2006-01-02 15:04"),
			p.Content,
		))
	}

	history := []ChatMessage{{Role: "user", Content: sb.String()}}
	result, _, err := activeAgent.Chat(ctx, history, gameDigestInstructions)
	return result, err
}

func periodToTime(period string) time.Time {
	now := time.Now()
	switch period {
	case "daily":
		return now.Add(-24 * time.Hour)
	case "bi-weekly":
		return now.Add(-14 * 24 * time.Hour)
	case "monthly":
		return now.Add(-30 * 24 * time.Hour)
	default: // weekly
		return now.Add(-7 * 24 * time.Hour)
	}
}
