package Services

import (
	"cuento-backend/config"
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/expectedsh/go-sonic/sonic"
)

const SonicCollection = "cuento"

const (
	SonicBucketGamePosts    = "game_posts"
	SonicBucketGeneralPosts = "general_posts"
	SonicBucketLorePosts    = "lore_posts"
	SonicBucketCharacters   = "characters"
	SonicBucketWantedPosts  = "wanted_posts"
	SonicBucketEpisodes     = "episodes"
)

var sonicCfg *config.SonicConfig

func InitSonic() {
	cfg := config.LoadSonicConfig()

	// Verify connectivity via a control connection
	c, err := sonic.NewControl(cfg.Host, cfg.Port, cfg.Password)
	if err != nil {
		log.Printf("Warning: could not connect to Sonic at %s:%d — search will be unavailable: %v", cfg.Host, cfg.Port, err)
		return
	}
	_ = c.Quit()

	sonicCfg = cfg
	log.Printf("Successfully connected to Sonic at %s:%d", cfg.Host, cfg.Port)
}

func SonicAvailable() bool {
	return sonicCfg != nil
}

// SonicPush indexes text for an object in the given collection and bucket.
func SonicPush(collection, bucket, objectID, text string, lang sonic.Lang) error {
	c, err := sonic.NewIngester(sonicCfg.Host, sonicCfg.Port, sonicCfg.Password)
	if err != nil {
		return err
	}
	defer c.Quit()
	return c.Push(collection, bucket, objectID, text, lang)
}

// SonicDelete removes all indexed text for an object from a collection/bucket.
func SonicDelete(collection, bucket, objectID string) error {
	c, err := sonic.NewIngester(sonicCfg.Host, sonicCfg.Port, sonicCfg.Password)
	if err != nil {
		return err
	}
	defer c.Quit()
	return c.FlushObject(collection, bucket, objectID)
}

// SonicDeleteBatch removes multiple objects from the same collection/bucket over a single connection.
func SonicDeleteBatch(collection, bucket string, objectIDs []string) error {
	if len(objectIDs) == 0 {
		return nil
	}
	c, err := sonic.NewIngester(sonicCfg.Host, sonicCfg.Port, sonicCfg.Password)
	if err != nil {
		return err
	}
	defer c.Quit()
	for _, id := range objectIDs {
		if err := c.FlushObject(collection, bucket, id); err != nil {
			return err
		}
	}
	return nil
}

// SonicQuery searches for a term in the given collection and bucket.
// Returns a slice of object IDs.
func SonicQuery(collection, bucket, term string, limit, offset int, lang sonic.Lang) ([]string, error) {
	c, err := sonic.NewSearch(sonicCfg.Host, sonicCfg.Port, sonicCfg.Password)
	if err != nil {
		return nil, err
	}
	defer c.Quit()
	return c.Query(collection, bucket, term, limit, offset, lang)
}

// SonicSuggest returns autocomplete suggestions for a word prefix.
func SonicSuggest(collection, bucket, word string, limit int) ([]string, error) {
	c, err := sonic.NewSearch(sonicCfg.Host, sonicCfg.Port, sonicCfg.Password)
	if err != nil {
		return nil, err
	}
	defer c.Quit()
	return c.Suggest(collection, bucket, word, limit)
}

// SonicPushFlattenedEntity reads a single entity from its base+flattened tables and pushes it to Sonic.
func SonicPushFlattenedEntity(bucket, entityName string, id int64, db *sql.DB) error {
	rows, err := db.Query(
		fmt.Sprintf(`SELECT b.*, f.* FROM %s_base b LEFT JOIN %s_flattened f ON b.id = f.entity_id WHERE b.id = ?`, entityName, entityName),
		id,
	)
	if err != nil {
		return fmt.Errorf("failed to query %s %d: %w", entityName, id, err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("failed to read columns for %s: %w", entityName, err)
	}

	if !rows.Next() {
		return nil
	}

	entityID, doc, err := scanFlattenedRowForSonic(rows, cols)
	if err != nil || doc == "" {
		return err
	}

	return SonicPush(SonicCollection, bucket, strconv.FormatInt(entityID, 10), doc, sonic.LangAutoDetect)
}

// AllSonicBuckets lists every known bucket in a defined order.
var AllSonicBuckets = []string{
	SonicBucketGamePosts,
	SonicBucketGeneralPosts,
	SonicBucketLorePosts,
	SonicBucketCharacters,
	SonicBucketWantedPosts,
	SonicBucketEpisodes,
}

// EntityBuckets maps bucket name to entity base-table prefix for flattened buckets.
var EntityBuckets = map[string]string{
	SonicBucketCharacters:  "character",
	SonicBucketWantedPosts: "wanted_character",
	SonicBucketEpisodes:    "episode",
}

// SearchResultItem is returned by SearchInBucket.
type SearchResultItem struct {
	ID        string `json:"id"`
	Content   string `json:"content"`
	TopicID   *int64 `json:"topic_id,omitempty"`
	TopicName string `json:"topic_name,omitempty"`
	TopicType *int   `json:"topic_type,omitempty"`
}

// SearchInBucket queries Sonic for query in the given bucket, then fetches full content
// from the database. Results are filtered to visibleSubforums; filterSubforum and
// filterTopicType (0 = no filter) are applied as additional constraints.
func SearchInBucket(bucket, query string, limit, filterSubforum, filterTopicType int, visibleSubforums map[int]bool, db *sql.DB) ([]SearchResultItem, error) {
	sonicIDs, err := SonicQuery(SonicCollection, bucket, query, limit, 0, sonic.LangAutoDetect)
	if err != nil || len(sonicIDs) == 0 {
		return nil, nil
	}

	placeholders := strings.Repeat("?,", len(sonicIDs)-1) + "?"
	args := make([]interface{}, len(sonicIDs))
	for i, id := range sonicIDs {
		args[i] = id
	}

	// --- permission + filter query ---
	var metaQuery string
	switch bucket {
	case SonicBucketGamePosts, SonicBucketGeneralPosts, SonicBucketLorePosts:
		metaQuery = fmt.Sprintf(`SELECT p.id, t.subforum_id, t.type FROM posts p JOIN topics t ON p.topic_id = t.id WHERE p.id IN (%s)`, placeholders)
	case SonicBucketCharacters:
		metaQuery = fmt.Sprintf(`SELECT cb.id, t.subforum_id, t.type FROM character_base cb JOIN topics t ON cb.topic_id = t.id WHERE cb.id IN (%s)`, placeholders)
	case SonicBucketWantedPosts:
		metaQuery = fmt.Sprintf(`SELECT wc.id, t.subforum_id, t.type FROM wanted_character_base wc JOIN topics t ON wc.topic_id = t.id WHERE wc.id IN (%s)`, placeholders)
	case SonicBucketEpisodes:
		metaQuery = fmt.Sprintf(`SELECT eb.id, t.subforum_id, t.type FROM episode_base eb JOIN topics t ON eb.topic_id = t.id WHERE eb.id IN (%s)`, placeholders)
	default:
		return nil, fmt.Errorf("unknown bucket: %s", bucket)
	}

	metaRows, err := db.Query(metaQuery, args...)
	if err != nil {
		return nil, err
	}
	defer metaRows.Close()

	allowedIDs := make(map[string]bool)
	for metaRows.Next() {
		var rawID int64
		var subforumID, topicType int
		if err := metaRows.Scan(&rawID, &subforumID, &topicType); err != nil {
			continue
		}
		if !visibleSubforums[subforumID] {
			continue
		}
		if filterSubforum != 0 && subforumID != filterSubforum {
			continue
		}
		if filterTopicType != 0 && topicType != filterTopicType {
			continue
		}
		allowedIDs[strconv.FormatInt(rawID, 10)] = true
	}

	if len(allowedIDs) == 0 {
		return nil, nil
	}

	// Rebuild args from allowed IDs only
	filtered := make([]string, 0, len(allowedIDs))
	for _, id := range sonicIDs { // preserve Sonic ranking order
		if allowedIDs[id] {
			filtered = append(filtered, id)
		}
	}

	// --- content fetch ---
	filteredPlaceholders := strings.Repeat("?,", len(filtered)-1) + "?"
	filteredArgs := make([]interface{}, len(filtered))
	for i, id := range filtered {
		filteredArgs[i] = id
	}

	switch bucket {
	case SonicBucketGamePosts, SonicBucketGeneralPosts, SonicBucketLorePosts:
		rows, err := db.Query(fmt.Sprintf(
			`SELECT p.id, p.content, p.topic_id, t.name, t.type
			 FROM posts p JOIN topics t ON p.topic_id = t.id
			 WHERE p.id IN (%s)`, filteredPlaceholders), filteredArgs...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		contentMap := make(map[string]SearchResultItem, len(filtered))
		for rows.Next() {
			var id, topicID int64
			var content, topicName string
			var topicType int
			if err := rows.Scan(&id, &content, &topicID, &topicName, &topicType); err != nil {
				continue
			}
			sid := strconv.FormatInt(id, 10)
			contentMap[sid] = SearchResultItem{
				ID:        sid,
				Content:   extractMatchingParagraphs(content, query),
				TopicID:   &topicID,
				TopicName: topicName,
				TopicType: &topicType,
			}
		}

		results := make([]SearchResultItem, 0, len(filtered))
		for _, id := range filtered {
			if item, ok := contentMap[id]; ok {
				results = append(results, item)
			}
		}
		return results, nil

	default:
		entityName := EntityBuckets[bucket]
		rows, err := db.Query(
			fmt.Sprintf(`SELECT b.*, f.*, t.name AS _topic_name, t.type AS _topic_type
			 FROM %s_base b
			 LEFT JOIN %s_flattened f ON b.id = f.entity_id
			 JOIN topics t ON b.topic_id = t.id
			 WHERE b.id IN (%s)`, entityName, entityName, filteredPlaceholders),
			filteredArgs...,
		)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		cols, err := rows.Columns()
		if err != nil {
			return nil, err
		}

		contentMap := make(map[string]SearchResultItem, len(filtered))
		for rows.Next() {
			id, topicID, topicName, topicType, doc, err := scanFlattenedRowWithTopicID(rows, cols)
			if err != nil {
				continue
			}
			sid := strconv.FormatInt(id, 10)
			contentMap[sid] = SearchResultItem{
				ID:        sid,
				Content:   extractMatchingParagraphs(doc, query),
				TopicID:   &topicID,
				TopicName: topicName,
				TopicType: &topicType,
			}
		}

		results := make([]SearchResultItem, 0, len(filtered))
		for _, id := range filtered {
			if item, ok := contentMap[id]; ok {
				results = append(results, item)
			}
		}
		return results, nil
	}
}

// extractMatchingParagraphs returns the paragraphs from text that contain the query term.
// Paragraphs are split on blank lines or single newlines. If no paragraph matches
// (e.g. Sonic used stemming/fuzzy matching), the first paragraph is returned as fallback.
func extractMatchingParagraphs(text, query string) string {
	paragraphs := strings.Split(text, "\n")
	lowerQuery := strings.ToLower(query)

	var matched []string
	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.Contains(strings.ToLower(p), lowerQuery) {
			matched = append(matched, p)
		}
	}

	if len(matched) > 0 {
		return strings.Join(matched, "\n")
	}

	// Fallback: first non-empty paragraph
	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p != "" {
			return p
		}
	}
	return text
}

func scanFlattenedRowWithTopicID(rows *sql.Rows, cols []string) (id int64, topicID int64, topicName string, topicType int, doc string, err error) {
	vals := make([]interface{}, len(cols))
	ptrs := make([]interface{}, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err = rows.Scan(ptrs...); err != nil {
		return
	}

	var parts []string
	for i, col := range cols {
		raw := vals[i]
		if raw == nil {
			continue
		}
		var s string
		switch v := raw.(type) {
		case []byte:
			s = string(v)
		case string:
			s = v
		case int64:
			s = strconv.FormatInt(v, 10)
		default:
			s = fmt.Sprintf("%v", v)
		}
		switch col {
		case "id":
			if id == 0 {
				id, _ = strconv.ParseInt(s, 10, 64)
			}
		case "topic_id":
			topicID, _ = strconv.ParseInt(s, 10, 64)
		case "entity_id", "_topic_name", "_topic_type":
			if col == "_topic_name" {
				topicName = s
			} else if col == "_topic_type" {
				t, _ := strconv.Atoi(s)
				topicType = t
			}
		default:
			s = strings.TrimSpace(s)
			if s != "" {
				parts = append(parts, s)
			}
		}
	}
	doc = strings.Join(parts, " ")
	return
}

func scanFlattenedRowForSonic(rows *sql.Rows, cols []string) (int64, string, error) {
	vals := make([]interface{}, len(cols))
	ptrs := make([]interface{}, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return 0, "", err
	}

	var id int64
	var parts []string

	for i, col := range cols {
		raw := vals[i]
		if raw == nil {
			continue
		}

		var s string
		switch v := raw.(type) {
		case []byte:
			s = string(v)
		case string:
			s = v
		case int64:
			s = strconv.FormatInt(v, 10)
		default:
			s = fmt.Sprintf("%v", v)
		}

		if col == "id" {
			if id == 0 {
				id, _ = strconv.ParseInt(s, 10, 64)
			}
			continue
		}
		if col == "entity_id" {
			continue
		}

		s = strings.TrimSpace(s)
		if s != "" {
			parts = append(parts, s)
		}
	}

	return id, strings.Join(parts, " "), nil
}
