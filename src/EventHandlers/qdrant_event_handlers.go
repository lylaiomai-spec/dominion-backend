package EventHandlers

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Events"
	"cuento-backend/src/Services"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

// isVectorEnabled checks whether the given subforum+bucket pair is configured
// for vector indexing in vector_search_bucket_subforum.
func isVectorEnabled(subforumID int, bucket string, db *sql.DB) bool {
	var count int
	_ = db.QueryRow(
		`SELECT COUNT(*) FROM vector_search_bucket_subforum WHERE subforum_id = ? AND bucket = ?`,
		subforumID, bucket,
	).Scan(&count)
	return count > 0
}

// postBucket returns the Qdrant bucket for a given topic type, or ("", false) if not applicable.
func postBucket(topicType Entities.TopicType) (string, bool) {
	switch topicType {
	case Entities.EpisodeTopic:
		return Services.SonicBucketGamePosts, true
	case Entities.GeneralTopic:
		return Services.SonicBucketGeneralPosts, true
	case Entities.LoreTopic:
		return Services.SonicBucketLorePosts, true
	}
	return "", false
}

func updateQdrantCursor(bucket string, id int64, db *sql.DB) {
	_, _ = db.Exec(
		`INSERT INTO qdrant_ingest_cursor (bucket, last_id, date_ingested) VALUES (?, ?, NOW())
		 ON DUPLICATE KEY UPDATE last_id = GREATEST(last_id, VALUES(last_id)), date_ingested = VALUES(date_ingested)`,
		bucket, id,
	)
}

func RegisterQdrantEventHandlers() {
	// ── Posts ────────────────────────────────────────────────────────────────

	Events.Subscribe(Events.PostCreated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.PostCreatedEvent)
		if !ok {
			return
		}
		if !Services.QdrantAvailable() {
			return
		}

		var topicType Entities.TopicType
		if err := db.QueryRow("SELECT type FROM topics WHERE id = ?", event.TopicID).Scan(&topicType); err != nil {
			return
		}
		bucket, ok := postBucket(topicType)
		if !ok {
			return
		}

		switch event.Type {
		case "", "post_created":
			if !isVectorEnabled(event.SubforumID, bucket, db) {
				return
			}
			if err := Services.EnqueueEmbedding(bucket, int64(event.Post.Id), db); err != nil {
				fmt.Printf("Error enqueueing post %d for embedding: %v\n", event.Post.Id, err)
			} else {
				updateQdrantCursor(bucket, int64(event.Post.Id), db)
			}

		case "post_updated":
			if !isVectorEnabled(event.SubforumID, bucket, db) {
				return
			}
			if err := Services.EnqueueEmbedding(bucket, int64(event.Post.Id), db); err != nil {
				fmt.Printf("Error enqueueing post %d for re-embedding: %v\n", event.Post.Id, err)
			}

		case "post_deleted":
			if err := Services.QdrantDelete(bucket, strconv.Itoa(event.Post.Id)); err != nil {
				fmt.Printf("Error deleting post %d from Qdrant: %v\n", event.Post.Id, err)
			}
		}
	})

	// ── Characters ───────────────────────────────────────────────────────────

	Events.Subscribe(Events.CharacterAccepted, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.CharacterAcceptedEvent)
		if !ok || !Services.QdrantAvailable() {
			return
		}
		if !isVectorEnabled(event.SubforumID, Services.SonicBucketCharacters, db) {
			return
		}
		if err := Services.EnqueueEmbedding(Services.SonicBucketCharacters, int64(event.CharacterID), db); err != nil {
			fmt.Printf("Error enqueueing character %d for embedding: %v\n", event.CharacterID, err)
		} else {
			updateQdrantCursor(Services.SonicBucketCharacters, int64(event.CharacterID), db)
		}
	})

	Events.Subscribe(Events.CharacterDeactivated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.CharacterDeactivatedEvent)
		if !ok || !Services.QdrantAvailable() {
			return
		}
		if err := Services.QdrantDelete(Services.SonicBucketCharacters, strconv.Itoa(event.CharacterID)); err != nil {
			fmt.Printf("Error removing character %d from Qdrant: %v\n", event.CharacterID, err)
		}
	})

	Events.Subscribe(Events.CharacterUpdated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.CharacterUpdatedEvent)
		if !ok || !Services.QdrantAvailable() {
			return
		}
		if !isVectorEnabled(event.SubforumID, Services.SonicBucketCharacters, db) {
			return
		}
		if err := Services.EnqueueEmbedding(Services.SonicBucketCharacters, event.CharacterID, db); err != nil {
			fmt.Printf("Error enqueueing character %d for re-embedding: %v\n", event.CharacterID, err)
		}
	})

	// ── Episodes ─────────────────────────────────────────────────────────────

	Events.Subscribe(Events.EpisodeCreated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.EpisodeCreatedEvent)
		if !ok || !Services.QdrantAvailable() {
			return
		}
		if !isVectorEnabled(event.SubforumID, Services.SonicBucketEpisodes, db) {
			return
		}
		if err := Services.EnqueueEmbedding(Services.SonicBucketEpisodes, event.EpisodeID, db); err != nil {
			fmt.Printf("Error enqueueing episode %d for embedding: %v\n", event.EpisodeID, err)
		} else {
			updateQdrantCursor(Services.SonicBucketEpisodes, event.EpisodeID, db)
		}
	})

	Events.Subscribe(Events.EpisodeUpdated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.EpisodeUpdatedEvent)
		if !ok || !Services.QdrantAvailable() {
			return
		}
		if !isVectorEnabled(event.SubforumID, Services.SonicBucketEpisodes, db) {
			return
		}
		if err := Services.EnqueueEmbedding(Services.SonicBucketEpisodes, event.EpisodeID, db); err != nil {
			fmt.Printf("Error enqueueing episode %d for re-embedding: %v\n", event.EpisodeID, err)
		}
	})

	// ── Wanted Characters ────────────────────────────────────────────────────

	Events.Subscribe(Events.WantedCharacterCreated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.WantedCharacterCreatedEvent)
		if !ok || !Services.QdrantAvailable() {
			return
		}
		if !isVectorEnabled(event.SubforumID, Services.SonicBucketWantedPosts, db) {
			return
		}
		if err := Services.EnqueueEmbedding(Services.SonicBucketWantedPosts, event.WantedCharacterID, db); err != nil {
			fmt.Printf("Error enqueueing wanted character %d for embedding: %v\n", event.WantedCharacterID, err)
		} else {
			updateQdrantCursor(Services.SonicBucketWantedPosts, event.WantedCharacterID, db)
		}
	})

	Events.Subscribe(Events.WantedCharacterUpdated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.WantedCharacterUpdatedEvent)
		if !ok || !Services.QdrantAvailable() {
			return
		}
		if !isVectorEnabled(event.SubforumID, Services.SonicBucketWantedPosts, db) {
			return
		}
		if err := Services.EnqueueEmbedding(Services.SonicBucketWantedPosts, event.WantedCharacterID, db); err != nil {
			fmt.Printf("Error enqueueing wanted character %d for re-embedding: %v\n", event.WantedCharacterID, err)
		}
	})

	// ── Bulk deletes ─────────────────────────────────────────────────────────

	// When topics are deleted, remove all associated posts and entity vectors.
	Events.Subscribe(Events.TopicsDeleted, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.TopicsDeletedEvent)
		if !ok || len(event.TopicIDs) == 0 || !Services.QdrantAvailable() {
			return
		}

		placeholders := strings.Repeat("?,", len(event.TopicIDs)-1) + "?"
		args := make([]interface{}, len(event.TopicIDs))
		for i, id := range event.TopicIDs {
			args[i] = id
		}

		postIDs := map[string][]string{
			Services.SonicBucketGamePosts:    {},
			Services.SonicBucketGeneralPosts: {},
			Services.SonicBucketLorePosts:    {},
		}
		postRows, err := db.Query(
			fmt.Sprintf("SELECT p.id, t.type FROM posts p JOIN topics t ON p.topic_id = t.id WHERE t.id IN (%s)", placeholders),
			args...,
		)
		if err == nil {
			defer postRows.Close()
			for postRows.Next() {
				var postID int64
				var topicType Entities.TopicType
				if postRows.Scan(&postID, &topicType) != nil {
					continue
				}
				if b, ok := postBucket(topicType); ok {
					postIDs[b] = append(postIDs[b], strconv.FormatInt(postID, 10))
				}
			}
		}
		for bucket, ids := range postIDs {
			if err := Services.QdrantDeleteBatch(bucket, ids); err != nil {
				fmt.Printf("Error deleting posts from Qdrant bucket %s on topic delete: %v\n", bucket, err)
			}
		}

		charRows, err := db.Query(
			fmt.Sprintf("SELECT id FROM character_base WHERE topic_id IN (%s)", placeholders), args...,
		)
		if err == nil {
			defer charRows.Close()
			for charRows.Next() {
				var id int64
				if charRows.Scan(&id) == nil {
					_ = Services.QdrantDelete(Services.SonicBucketCharacters, strconv.FormatInt(id, 10))
				}
			}
		}

		wantedRows, err := db.Query(
			fmt.Sprintf("SELECT id FROM wanted_character_base WHERE topic_id IN (%s)", placeholders), args...,
		)
		if err == nil {
			defer wantedRows.Close()
			for wantedRows.Next() {
				var id int64
				if wantedRows.Scan(&id) == nil {
					_ = Services.QdrantDelete(Services.SonicBucketWantedPosts, strconv.FormatInt(id, 10))
				}
			}
		}
	})

	// When episode topics are deleted, remove their vectors.
	Events.Subscribe(Events.EpisodeTopicsDeleted, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.EpisodeTopicsDeletedEvent)
		if !ok || len(event.EpisodeIDs) == 0 || !Services.QdrantAvailable() {
			return
		}
		ids := make([]string, len(event.EpisodeIDs))
		for i, id := range event.EpisodeIDs {
			ids[i] = strconv.Itoa(id)
		}
		if err := Services.QdrantDeleteBatch(Services.SonicBucketEpisodes, ids); err != nil {
			fmt.Printf("Error deleting episodes from Qdrant: %v\n", err)
		}
	})

	// When a user is wiped, remove their general posts.
	Events.Subscribe(Events.UserWiped, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.UserWipedEvent)
		if !ok || len(event.DeletedGeneralPostIDs) == 0 || !Services.QdrantAvailable() {
			return
		}
		ids := make([]string, len(event.DeletedGeneralPostIDs))
		for i, id := range event.DeletedGeneralPostIDs {
			ids[i] = strconv.Itoa(id)
		}
		if err := Services.QdrantDeleteBatch(Services.SonicBucketGeneralPosts, ids); err != nil {
			fmt.Printf("Error deleting general posts from Qdrant on user wipe: %v\n", err)
		}
	})
}
