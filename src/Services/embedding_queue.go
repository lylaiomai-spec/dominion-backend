package Services

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
)

// QueueNotifyFunc is set by MCP at startup so Services can wake the worker
// without creating an import cycle.
var QueueNotifyFunc func()

// EnqueueEmbedding adds an embedding task to ai_task_queue and wakes the worker.
func EnqueueEmbedding(bucket string, entityID int64, db *sql.DB) error {
	payload, err := json.Marshal(map[string]any{
		"bucket":    bucket,
		"entity_id": entityID,
	})
	if err != nil {
		return err
	}
	_, err = db.Exec(
		`INSERT INTO ai_task_queue (type, payload) VALUES ('embedding', ?)`,
		string(payload),
	)
	if err == nil && QueueNotifyFunc != nil {
		QueueNotifyFunc()
	}
	return err
}

// ExecuteEmbeddingTask fetches content for the given bucket+entity, embeds it,
// and upserts the vector into Qdrant. Called by the MCP queue worker.
func ExecuteEmbeddingTask(bucket string, entityID int64, db *sql.DB) error {
	if entityName, isEntity := EntityBuckets[bucket]; isEntity {
		return QdrantPushFlattenedEntity(bucket, entityName, entityID, db)
	}
	return qdrantPushPost(bucket, entityID, db)
}

func qdrantPushPost(bucket string, postID int64, db *sql.DB) error {
	var content string
	err := db.QueryRow(
		`SELECT content FROM posts WHERE id = ? AND COALESCE(is_deleted, 0) != 1`,
		postID,
	).Scan(&content)
	if err != nil {
		return fmt.Errorf("fetching post %d: %w", postID, err)
	}
	return QdrantPush(bucket, strconv.FormatInt(postID, 10), content)
}
