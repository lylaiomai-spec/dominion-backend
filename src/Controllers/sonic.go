package Controllers

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Services"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/expectedsh/go-sonic/sonic"
	"github.com/gin-gonic/gin"
)

type SonicCursorItem struct {
	Bucket       string     `json:"bucket"`
	LastId       *int64     `json:"last_id"`
	DateIngested *time.Time `json:"date_ingested"`
	CurrentMaxId int64      `json:"current_max_id"`
}

var allBuckets = Services.AllSonicBuckets

// entityBuckets maps bucket name to the entity base table name for flattened ingestion.
var entityBuckets = Services.EntityBuckets

func GetSonicBuckets(c *gin.Context) {
	c.JSON(http.StatusOK, allBuckets)
}

func GetSonicCursors(c *gin.Context, db *sql.DB) {
	rows, err := db.Query("SELECT bucket, last_id, date_ingested FROM sonic_ingest_cursor")
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch cursors: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	cursorMap := make(map[string]*SonicCursorItem)
	for rows.Next() {
		var item SonicCursorItem
		if err := rows.Scan(&item.Bucket, &item.LastId, &item.DateIngested); err == nil {
			cursorMap[item.Bucket] = &item
		}
	}

	result := make([]SonicCursorItem, 0, len(allBuckets))
	for _, bucket := range allBuckets {
		item := SonicCursorItem{Bucket: bucket}
		if existing, ok := cursorMap[bucket]; ok {
			item.LastId = existing.LastId
			item.DateIngested = existing.DateIngested
		}
		item.CurrentMaxId = getBucketMaxId(bucket, db)
		result = append(result, item)
	}

	c.JSON(http.StatusOK, result)
}

func CatchUpSonicBucket(c *gin.Context, db *sql.DB) {
	bucket := c.Param("bucket")

	if !isKnownBucket(bucket) {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Unknown bucket: " + bucket})
		c.Abort()
		return
	}

	var fromId int64
	_ = db.QueryRow("SELECT last_id FROM sonic_ingest_cursor WHERE bucket = ?", bucket).Scan(&fromId)

	var lastId int64
	var count int
	var ingestErr error

	if entityName, isEntity := entityBuckets[bucket]; isEntity {
		lastId, count, ingestErr = ingestFlattenedBucket(bucket, entityName, fromId, db)
	} else {
		lastId, count, ingestErr = ingestPostBucket(bucket, fromId, db)
	}

	if ingestErr != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: ingestErr.Error()})
		c.Abort()
		return
	}

	if count > 0 {
		_, err := db.Exec(
			`INSERT INTO sonic_ingest_cursor (bucket, last_id, date_ingested) VALUES (?, ?, ?)
			 ON DUPLICATE KEY UPDATE last_id = VALUES(last_id), date_ingested = VALUES(date_ingested)`,
			bucket, lastId, time.Now(),
		)
		if err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to update cursor: " + err.Error()})
			c.Abort()
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"bucket":   bucket,
		"ingested": count,
		"last_id":  lastId,
	})
}

// ingestPostBucket handles posts buckets: queries (id, content) and pushes each post.
func ingestPostBucket(bucket string, fromId int64, db *sql.DB) (lastId int64, count int, err error) {
	rows, err := fetchPostRows(bucket, fromId, db)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to fetch posts: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var text string
		if err := rows.Scan(&id, &text); err != nil {
			continue
		}
		if err := Services.SonicPush(Services.SonicCollection, bucket, strconv.FormatInt(id, 10), text, sonic.LangAutoDetect); err != nil {
			return lastId, count, fmt.Errorf("failed to push post %d to Sonic: %w", id, err)
		}
		if id > lastId {
			lastId = id
		}
		count++
	}
	return lastId, count, nil
}

// ingestFlattenedBucket handles entity buckets: reads all columns dynamically from the
// base+flattened join and builds a text document from every non-null value.
func ingestFlattenedBucket(bucket, entityName string, fromId int64, db *sql.DB) (lastId int64, count int, err error) {
	query := fmt.Sprintf(
		`SELECT b.*, f.* FROM %s_base b
		 LEFT JOIN %s_flattened f ON b.id = f.entity_id
		 JOIN topics t ON b.topic_id = t.id
		 WHERE b.id > ? AND t.status != ? ORDER BY b.id ASC`,
		entityName, entityName,
	)
	rows, err := db.Query(query, fromId, Entities.DeletedTopic)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to fetch %s entities: %w", entityName, err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read columns: %w", err)
	}

	for rows.Next() {
		id, doc, err := scanFlattenedRow(rows, cols)
		if err != nil {
			continue
		}
		if doc == "" {
			continue
		}
		if err := Services.SonicPush(Services.SonicCollection, bucket, strconv.FormatInt(id, 10), doc, sonic.LangAutoDetect); err != nil {
			return lastId, count, fmt.Errorf("failed to push %s %d to Sonic: %w", entityName, id, err)
		}
		if id > lastId {
			lastId = id
		}
		count++
	}
	return lastId, count, nil
}

// scanFlattenedRow scans a row into a map and returns the entity id and a space-joined
// text document built from all non-null column values (entity_id is skipped as a duplicate of id).
func scanFlattenedRow(rows *sql.Rows, cols []string) (int64, string, error) {
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

		// Convert value to string
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
			// Parse entity id from the base table's first id column
			if id == 0 {
				id, _ = strconv.ParseInt(s, 10, 64)
			}
			continue
		}
		if col == "entity_id" {
			// Duplicate of id from the flattened table — skip
			continue
		}

		s = strings.TrimSpace(s)
		if s != "" {
			parts = append(parts, s)
		}
	}

	return id, strings.Join(parts, " "), nil
}

func fetchPostRows(bucket string, fromId int64, db *sql.DB) (*sql.Rows, error) {
	topicType, ok := postBucketTopicType(bucket)
	if !ok {
		return nil, fmt.Errorf("unknown post bucket: %s", bucket)
	}
	return db.Query(
		`SELECT p.id, p.content FROM posts p
		 JOIN topics t ON p.topic_id = t.id
		 WHERE t.type = ? AND COALESCE(p.is_deleted, 0) != 1 AND t.status != ? AND p.id > ?
		 ORDER BY p.id ASC`,
		topicType, Entities.DeletedTopic, fromId,
	)
}

func postBucketTopicType(bucket string) (Entities.TopicType, bool) {
	switch bucket {
	case Services.SonicBucketGamePosts:
		return Entities.EpisodeTopic, true
	case Services.SonicBucketGeneralPosts:
		return Entities.GeneralTopic, true
	case Services.SonicBucketLorePosts:
		return Entities.LoreTopic, true
	}
	return 0, false
}

func getBucketMaxId(bucket string, db *sql.DB) int64 {
	var maxId int64
	switch bucket {
	case Services.SonicBucketGamePosts:
		_ = db.QueryRow(`SELECT COALESCE(MAX(p.id), 0) FROM posts p JOIN topics t ON p.topic_id = t.id WHERE t.type = ? AND COALESCE(p.is_deleted, 0) != 1`, Entities.EpisodeTopic).Scan(&maxId)
	case Services.SonicBucketGeneralPosts:
		_ = db.QueryRow(`SELECT COALESCE(MAX(p.id), 0) FROM posts p JOIN topics t ON p.topic_id = t.id WHERE t.type = ? AND COALESCE(p.is_deleted, 0) != 1`, Entities.GeneralTopic).Scan(&maxId)
	case Services.SonicBucketLorePosts:
		_ = db.QueryRow(`SELECT COALESCE(MAX(p.id), 0) FROM posts p JOIN topics t ON p.topic_id = t.id WHERE t.type = ? AND COALESCE(p.is_deleted, 0) != 1`, Entities.LoreTopic).Scan(&maxId)
	case Services.SonicBucketCharacters:
		_ = db.QueryRow(`SELECT COALESCE(MAX(id), 0) FROM character_base`).Scan(&maxId)
	case Services.SonicBucketWantedPosts:
		_ = db.QueryRow(`SELECT COALESCE(MAX(id), 0) FROM wanted_character_base`).Scan(&maxId)
	case Services.SonicBucketEpisodes:
		_ = db.QueryRow(`SELECT COALESCE(MAX(id), 0) FROM episode_base`).Scan(&maxId)
	}
	return maxId
}

type searchResultMeta struct {
	id         string
	subforumID int
	authorID   int
}

type SearchResultItem struct {
	ID      string `json:"id"`
	Snippet string `json:"snippet"`
	TopicID *int64 `json:"topic_id,omitempty"`
}

// parseSearchParams extracts and validates shared search parameters from the request.
// Returns term, buckets, filterSubforum, filterAuthor, visibleSet, and any error.
func parseSearchParams(c *gin.Context, db *sql.DB) (term string, buckets []string, filterSubforum, filterAuthor int, visibleSet map[int]bool, ok bool) {
	term = strings.TrimSpace(c.Query("q"))
	if term == "" {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Missing search query"})
		c.Abort()
		return
	}

	if raw := c.Query("buckets"); raw != "" {
		for _, b := range strings.Split(raw, ",") {
			b = strings.TrimSpace(b)
			if isKnownBucket(b) {
				buckets = append(buckets, b)
			}
		}
		if len(buckets) == 0 {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "No valid buckets specified"})
			c.Abort()
			return
		}
	} else {
		buckets = append([]string{}, allBuckets...)
	}

	filterSubforum, _ = strconv.Atoi(c.Query("subforum_id"))
	filterAuthor, _ = strconv.Atoi(c.Query("user_id"))

	userID := Services.GetUserIdFromContext(c)
	visibleSubforums, _ := Services.GetVisibleSubforums(userID, "subforum_read", db)
	visibleSet = make(map[int]bool, len(visibleSubforums))
	for _, id := range visibleSubforums {
		visibleSet[id] = true
	}

	ok = true
	return
}

// queryAndFilter fetches IDs from Sonic and applies DB-side permission and optional filters.
func queryAndFilter(bucket, term string, limit, offset, filterSubforum, filterAuthor int, visibleSet map[int]bool, db *sql.DB) ([]string, error) {
	sonicIDs, err := Services.SonicQuery(Services.SonicCollection, bucket, term, limit, offset, sonic.LangAutoDetect)
	if err != nil || len(sonicIDs) == 0 {
		return []string{}, nil
	}

	metas, err := fetchSearchMeta(bucket, sonicIDs, db)
	if err != nil {
		return nil, err
	}

	var filtered []string
	for _, m := range metas {
		if !visibleSet[m.subforumID] {
			continue
		}
		if filterSubforum != 0 && m.subforumID != filterSubforum {
			continue
		}
		if filterAuthor != 0 && m.authorID != filterAuthor {
			continue
		}
		filtered = append(filtered, m.id)
	}
	return filtered, nil
}

func Search(c *gin.Context, db *sql.DB) {
	term, buckets, filterSubforum, filterAuthor, visibleSet, ok := parseSearchParams(c, db)
	if !ok {
		return
	}

	const limit = 20
	page := 1
	if p, err := strconv.Atoi(c.Query("page")); err == nil && p > 1 {
		page = p
	}
	offset := (page - 1) * limit

	results := make(map[string][]SearchResultItem, len(buckets))
	for _, bucket := range buckets {
		filteredIDs, err := queryAndFilter(bucket, term, limit, offset, filterSubforum, filterAuthor, visibleSet, db)
		if err != nil || len(filteredIDs) == 0 {
			results[bucket] = []SearchResultItem{}
			continue
		}

		texts, err := fetchSnippetTexts(bucket, filteredIDs, db)
		if err != nil {
			results[bucket] = []SearchResultItem{}
			continue
		}

		items := make([]SearchResultItem, 0, len(filteredIDs))
		for _, id := range filteredIDs {
			t, ok := texts[id]
			if !ok {
				continue
			}
			items = append(items, SearchResultItem{
				ID:      id,
				Snippet: buildSnippet(t.text, term),
				TopicID: t.topicID,
			})
		}
		results[bucket] = items
	}

	c.JSON(http.StatusOK, results)
}

func SearchCount(c *gin.Context, db *sql.DB) {
	term, buckets, filterSubforum, filterAuthor, visibleSet, ok := parseSearchParams(c, db)
	if !ok {
		return
	}

	// Use the Sonic maximum to count as many results as possible
	const countLimit = 100

	counts := make(map[string]int, len(buckets))
	for _, bucket := range buckets {
		filtered, err := queryAndFilter(bucket, term, countLimit, 0, filterSubforum, filterAuthor, visibleSet, db)
		if err != nil {
			counts[bucket] = 0
			continue
		}
		counts[bucket] = len(filtered)
	}

	c.JSON(http.StatusOK, counts)
}

type snippetText struct {
	text    string
	topicID *int64
}

// fetchSnippetTexts fetches the source text (and topic_id for posts) for the given IDs.
func fetchSnippetTexts(bucket string, ids []string, db *sql.DB) (map[string]snippetText, error) {
	placeholders := strings.Repeat("?,", len(ids)-1) + "?"
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		args[i] = id
	}

	result := make(map[string]snippetText, len(ids))

	switch bucket {
	case Services.SonicBucketGamePosts, Services.SonicBucketGeneralPosts, Services.SonicBucketLorePosts:
		rows, err := db.Query(fmt.Sprintf(`SELECT id, content, topic_id FROM posts WHERE id IN (%s)`, placeholders), args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var id, topicID int64
			var content string
			if err := rows.Scan(&id, &content, &topicID); err != nil {
				continue
			}
			result[strconv.FormatInt(id, 10)] = snippetText{text: content, topicID: &topicID}
		}

	default:
		// Entity buckets: rebuild document from base+flattened join
		entityName, ok := entityBuckets[bucket]
		if !ok {
			return nil, fmt.Errorf("unknown bucket: %s", bucket)
		}
		rows, err := db.Query(
			fmt.Sprintf(`SELECT b.*, f.* FROM %s_base b LEFT JOIN %s_flattened f ON b.id = f.entity_id WHERE b.id IN (%s)`, entityName, entityName, placeholders),
			args...,
		)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		cols, err := rows.Columns()
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			id, doc, err := scanFlattenedRow(rows, cols)
			if err != nil {
				continue
			}
			result[strconv.FormatInt(id, 10)] = snippetText{text: doc}
		}
	}

	return result, nil
}

// buildSnippet finds the search term in text and returns up to 50 runes on each side,
// wrapping the match in <strong> and adding ellipsis where the text is truncated.
func buildSnippet(text, term string) string {
	runes := []rune(text)
	termRunes := []rune(term)
	lowerText := []rune(strings.ToLower(text))
	lowerTerm := []rune(strings.ToLower(term))

	// Find first occurrence of term (case-insensitive)
	idx := -1
outer:
	for i := 0; i <= len(lowerText)-len(lowerTerm); i++ {
		for j, tr := range lowerTerm {
			if lowerText[i+j] != tr {
				continue outer
			}
		}
		idx = i
		break
	}

	// Term not found (stemmed/fuzzy match) — return opening of text
	if idx == -1 {
		if len(runes) <= 100 {
			return string(runes)
		}
		return string(runes[:100]) + "…"
	}

	start := idx - 50
	end := idx + len(termRunes) + 50

	prefix, suffix := "…", "…"
	if start <= 0 {
		start = 0
		prefix = ""
	}
	if end >= len(runes) {
		end = len(runes)
		suffix = ""
	}

	return prefix +
		string(runes[start:idx]) +
		"<strong>" + string(runes[idx:idx+len(termRunes)]) + "</strong>" +
		string(runes[idx+len(termRunes):end]) +
		suffix
}

// fetchSearchMeta retrieves subforum and author metadata for a list of Sonic result IDs.
func fetchSearchMeta(bucket string, ids []string, db *sql.DB) ([]searchResultMeta, error) {
	placeholders := strings.Repeat("?,", len(ids)-1) + "?"
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		args[i] = id
	}

	var query string
	var hasAuthor bool

	switch bucket {
	case Services.SonicBucketGamePosts, Services.SonicBucketGeneralPosts, Services.SonicBucketLorePosts:
		query = fmt.Sprintf(`SELECT p.id, t.subforum_id, p.author_user_id FROM posts p JOIN topics t ON p.topic_id = t.id WHERE p.id IN (%s)`, placeholders)
		hasAuthor = true
	case Services.SonicBucketCharacters:
		query = fmt.Sprintf(`SELECT cb.id, t.subforum_id, cb.user_id FROM character_base cb JOIN topics t ON cb.topic_id = t.id WHERE cb.id IN (%s)`, placeholders)
		hasAuthor = true
	case Services.SonicBucketWantedPosts:
		query = fmt.Sprintf(`SELECT wc.id, t.subforum_id, wc.author_user_id FROM wanted_character_base wc JOIN topics t ON wc.topic_id = t.id WHERE wc.id IN (%s)`, placeholders)
		hasAuthor = true
	case Services.SonicBucketEpisodes:
		query = fmt.Sprintf(`SELECT eb.id, t.subforum_id, 0 FROM episode_base eb JOIN topics t ON eb.topic_id = t.id WHERE eb.id IN (%s)`, placeholders)
		hasAuthor = false
	default:
		return nil, fmt.Errorf("unknown bucket: %s", bucket)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var metas []searchResultMeta
	for rows.Next() {
		var rawID int64
		var m searchResultMeta
		if hasAuthor {
			if err := rows.Scan(&rawID, &m.subforumID, &m.authorID); err != nil {
				continue
			}
		} else {
			var dummy int
			if err := rows.Scan(&rawID, &m.subforumID, &dummy); err != nil {
				continue
			}
		}
		m.id = strconv.FormatInt(rawID, 10)
		metas = append(metas, m)
	}
	return metas, nil
}

func isKnownBucket(bucket string) bool {
	for _, b := range allBuckets {
		if b == bucket {
			return true
		}
	}
	return false
}
