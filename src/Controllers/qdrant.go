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

	"github.com/gin-gonic/gin"
)

type QdrantCursorItem struct {
	Bucket       string     `json:"bucket"`
	LastId       *int64     `json:"last_id"`
	DateIngested *time.Time `json:"date_ingested"`
	CurrentMaxId int64      `json:"current_max_id"`
}

// GetQdrantCursors returns the ingest cursor state for all Qdrant buckets.
// GET /admin/qdrant/cursors
func GetQdrantCursors(c *gin.Context, db *sql.DB) {
	rows, err := db.Query("SELECT bucket, last_id, date_ingested FROM qdrant_ingest_cursor")
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to fetch cursors: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	cursorMap := make(map[string]*QdrantCursorItem)
	for rows.Next() {
		var item QdrantCursorItem
		if err := rows.Scan(&item.Bucket, &item.LastId, &item.DateIngested); err == nil {
			cursorMap[item.Bucket] = &item
		}
	}

	result := make([]QdrantCursorItem, 0, len(Services.AllSonicBuckets))
	for _, bucket := range Services.AllSonicBuckets {
		item := QdrantCursorItem{Bucket: bucket}
		if existing, ok := cursorMap[bucket]; ok {
			item.LastId = existing.LastId
			item.DateIngested = existing.DateIngested
		}
		item.CurrentMaxId = getBucketMaxId(bucket, db)
		result = append(result, item)
	}

	c.JSON(http.StatusOK, result)
}

// GetQdrantCollectionStatus returns the number of indexed vectors per bucket.
func GetQdrantCollectionStatus(c *gin.Context) {
	if !Services.QdrantAvailable() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Qdrant is not available"})
		return
	}

	status := Services.GetQdrantCollectionCounts()
	c.JSON(http.StatusOK, status)
}

// QdrantCatchUpBucket re-embeds and upserts all content for a given bucket.
// Accepts optional ?from_id=N to resume from a specific ID.
// POST /admin/qdrant/catchup/:bucket
func QdrantCatchUpBucket(c *gin.Context, db *sql.DB) {
	bucket := c.Param("bucket")

	if !isKnownQdrantBucket(bucket) {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Unknown bucket: " + bucket})
		c.Abort()
		return
	}

	if !Services.QdrantAvailable() {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusServiceUnavailable, Message: "Qdrant is not available"})
		c.Abort()
		return
	}

	var fromID int64
	if q := c.Query("from_id"); q != "" {
		fromID, _ = strconv.ParseInt(q, 10, 64)
	} else {
		_ = db.QueryRow("SELECT last_id FROM qdrant_ingest_cursor WHERE bucket = ?", bucket).Scan(&fromID)
	}

	var count int
	var lastID int64
	var ingestErr error

	if entityName, isEntity := qdrantEntityBuckets[bucket]; isEntity {
		lastID, count, ingestErr = qdrantIngestFlattenedBucket(bucket, entityName, fromID, db)
	} else {
		lastID, count, ingestErr = qdrantIngestPostBucket(bucket, fromID, db)
	}

	if ingestErr != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: ingestErr.Error()})
		c.Abort()
		return
	}

	if count > 0 {
		_, _ = db.Exec(
			`INSERT INTO qdrant_ingest_cursor (bucket, last_id, date_ingested) VALUES (?, ?, NOW())
			 ON DUPLICATE KEY UPDATE last_id = VALUES(last_id), date_ingested = VALUES(date_ingested)`,
			bucket, lastID,
		)
	}

	c.JSON(http.StatusOK, gin.H{
		"bucket":   bucket,
		"ingested": count,
		"last_id":  lastID,
	})
}

func qdrantIngestPostBucket(bucket string, fromID int64, db *sql.DB) (lastID int64, count int, err error) {
	topicType, ok := qdrantPostBucketTopicType(bucket)
	if !ok {
		return 0, 0, fmt.Errorf("unknown post bucket: %s", bucket)
	}

	rows, err := db.Query(
		`SELECT p.id, p.content FROM posts p
		 JOIN topics t ON p.topic_id = t.id
		 JOIN vector_search_bucket_subforum vsbs ON vsbs.subforum_id = t.subforum_id AND vsbs.bucket = ?
		 WHERE t.type = ? AND COALESCE(p.is_deleted, 0) != 1 AND t.status != ? AND p.id > ?
		 ORDER BY p.id ASC`,
		bucket, topicType, Entities.DeletedTopic, fromID,
	)
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
		if err := Services.QdrantPush(bucket, strconv.FormatInt(id, 10), text); err != nil {
			return lastID, count, fmt.Errorf("failed to push post %d: %w", id, err)
		}
		if id > lastID {
			lastID = id
		}
		count++
	}
	return lastID, count, nil
}

func qdrantIngestFlattenedBucket(bucket, entityName string, fromID int64, db *sql.DB) (lastID int64, count int, err error) {
	rows, err := db.Query(
		fmt.Sprintf(
			`SELECT b.*, f.* FROM %s_base b
			 LEFT JOIN %s_flattened f ON b.id = f.entity_id
			 JOIN topics t ON b.topic_id = t.id
			 JOIN vector_search_bucket_subforum vsbs ON vsbs.subforum_id = t.subforum_id AND vsbs.bucket = ?
			 WHERE b.id > ? AND t.status != ? ORDER BY b.id ASC`,
			entityName, entityName,
		),
		bucket, fromID, Entities.DeletedTopic,
	)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to fetch %s entities: %w", entityName, err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read columns: %w", err)
	}

	for rows.Next() {
		id, doc, err := qdrantScanFlattenedRow(rows, cols)
		if err != nil || doc == "" {
			continue
		}
		if err := Services.QdrantPush(bucket, strconv.FormatInt(id, 10), doc); err != nil {
			return lastID, count, fmt.Errorf("failed to push %s %d: %w", entityName, id, err)
		}
		if id > lastID {
			lastID = id
		}
		count++
	}
	return lastID, count, nil
}

func qdrantScanFlattenedRow(rows *sql.Rows, cols []string) (int64, string, error) {
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

func qdrantPostBucketTopicType(bucket string) (Entities.TopicType, bool) {
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

// SubforumBucketRow is one row in the matrix response.
type SubforumBucketRow struct {
	SubforumID   int64           `json:"subforum_id"`
	SubforumName string          `json:"subforum_name"`
	Buckets      map[string]bool `json:"buckets"`
}

// GetVectorSearchMatrix returns a matrix of all subforums × all buckets,
// with true where a row exists in vector_search_bucket_subforum.
// GET /admin/qdrant/subforum-matrix
func GetVectorSearchMatrix(c *gin.Context, db *sql.DB) {
	subRows, err := db.Query(`SELECT id, COALESCE(name, '') FROM subforums ORDER BY position, id`)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: err.Error()})
		c.Abort()
		return
	}
	defer subRows.Close()

	var matrix []SubforumBucketRow
	subforumIdx := map[int64]int{}
	for subRows.Next() {
		var id int64
		var name string
		if err := subRows.Scan(&id, &name); err != nil {
			continue
		}
		buckets := make(map[string]bool, len(Services.AllSonicBuckets))
		for _, b := range Services.AllSonicBuckets {
			buckets[b] = false
		}
		subforumIdx[id] = len(matrix)
		matrix = append(matrix, SubforumBucketRow{SubforumID: id, SubforumName: name, Buckets: buckets})
	}

	enabledRows, err := db.Query(`SELECT subforum_id, bucket FROM vector_search_bucket_subforum`)
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: err.Error()})
		c.Abort()
		return
	}
	defer enabledRows.Close()

	for enabledRows.Next() {
		var subforumID int64
		var bucket string
		if err := enabledRows.Scan(&subforumID, &bucket); err != nil {
			continue
		}
		if idx, ok := subforumIdx[subforumID]; ok {
			matrix[idx].Buckets[bucket] = true
		}
	}

	if matrix == nil {
		matrix = []SubforumBucketRow{}
	}
	c.JSON(http.StatusOK, matrix)
}

type vectorSearchEntry struct {
	SubforumID int64  `json:"subforum_id"`
	Bucket     string `json:"bucket"`
}

// UpdateVectorSearchMatrix replaces the entire vector_search_bucket_subforum table.
// Body: array of {subforum_id, bucket} pairs that should be enabled.
// PUT /admin/qdrant/subforum-matrix
func UpdateVectorSearchMatrix(c *gin.Context, db *sql.DB) {
	var entries []vectorSearchEntry
	if err := c.ShouldBindJSON(&entries); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Invalid body: " + err.Error()})
		c.Abort()
		return
	}

	for _, e := range entries {
		if !isKnownQdrantBucket(e.Bucket) {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusBadRequest, Message: "Unknown bucket: " + e.Bucket})
			c.Abort()
			return
		}
	}

	tx, err := db.Begin()
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: err.Error()})
		c.Abort()
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM vector_search_bucket_subforum`); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: err.Error()})
		c.Abort()
		return
	}

	for _, e := range entries {
		if _, err := tx.Exec(
			`INSERT INTO vector_search_bucket_subforum (subforum_id, bucket) VALUES (?, ?)`,
			e.SubforumID, e.Bucket,
		); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: err.Error()})
			c.Abort()
			return
		}
	}

	if err := tx.Commit(); err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: err.Error()})
		c.Abort()
		return
	}

	c.Status(http.StatusNoContent)
}

var qdrantEntityBuckets = map[string]string{
	Services.SonicBucketCharacters:  "character",
	Services.SonicBucketWantedPosts: "wanted_character",
	Services.SonicBucketEpisodes:    "episode",
}

func isKnownQdrantBucket(bucket string) bool {
	for _, b := range Services.AllSonicBuckets {
		if b == bucket {
			return true
		}
	}
	return false
}
