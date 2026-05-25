package EventHandlers

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Events"
	"cuento-backend/src/Services"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/expectedsh/go-sonic/sonic"
)

func updateSonicCursor(bucket string, id int64, db *sql.DB) {
	_, _ = db.Exec(
		`INSERT INTO sonic_ingest_cursor (bucket, last_id, date_ingested) VALUES (?, ?, NOW())
		 ON DUPLICATE KEY UPDATE last_id = GREATEST(last_id, VALUES(last_id)), date_ingested = VALUES(date_ingested)`,
		bucket, id,
	)
}

func RegisterSonicEventHandlers() {
	Events.Subscribe(Events.PostCreated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.PostCreatedEvent)
		if !ok || event.Type == "post_updated" {
			return
		}
		if !Services.SonicAvailable() {
			return
		}

		var topicType Entities.TopicType
		if err := db.QueryRow("SELECT type FROM topics WHERE id = ?", event.TopicID).Scan(&topicType); err != nil {
			return
		}

		var bucket string
		switch topicType {
		case Entities.EpisodeTopic:
			bucket = Services.SonicBucketGamePosts
		case Entities.GeneralTopic:
			bucket = Services.SonicBucketGeneralPosts
		case Entities.LoreTopic:
			bucket = Services.SonicBucketLorePosts
		default:
			return
		}

		objectID := strconv.Itoa(event.Post.Id)
		if err := Services.SonicPush(Services.SonicCollection, bucket, objectID, event.Post.Content, sonic.LangAutoDetect); err != nil {
			fmt.Printf("Error pushing post %s to Sonic: %v\n", objectID, err)
		} else {
			updateSonicCursor(bucket, int64(event.Post.Id), db)
		}
	})

	Events.Subscribe(Events.CharacterAccepted, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.CharacterAcceptedEvent)
		if !ok {
			return
		}
		if !Services.SonicAvailable() {
			return
		}
		if err := Services.SonicPushFlattenedEntity(Services.SonicBucketCharacters, "character", int64(event.CharacterID), db); err != nil {
			fmt.Printf("Error pushing character %d to Sonic: %v\n", event.CharacterID, err)
		} else {
			updateSonicCursor(Services.SonicBucketCharacters, int64(event.CharacterID), db)
		}
	})

	Events.Subscribe(Events.CharacterDeactivated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.CharacterDeactivatedEvent)
		if !ok || !Services.SonicAvailable() {
			return
		}
		if err := Services.SonicDelete(Services.SonicCollection, Services.SonicBucketCharacters, strconv.Itoa(event.CharacterID)); err != nil {
			fmt.Printf("Error removing character %d from Sonic: %v\n", event.CharacterID, err)
		}
	})

	Events.Subscribe(Events.EpisodeCreated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.EpisodeCreatedEvent)
		if !ok {
			return
		}
		if !Services.SonicAvailable() {
			return
		}
		if err := Services.SonicPushFlattenedEntity(Services.SonicBucketEpisodes, "episode", event.EpisodeID, db); err != nil {
			fmt.Printf("Error pushing episode %d to Sonic: %v\n", event.EpisodeID, err)
		} else {
			updateSonicCursor(Services.SonicBucketEpisodes, event.EpisodeID, db)
		}
	})

	Events.Subscribe(Events.WantedCharacterCreated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.WantedCharacterCreatedEvent)
		if !ok {
			return
		}
		if !Services.SonicAvailable() {
			return
		}
		if err := Services.SonicPushFlattenedEntity(Services.SonicBucketWantedPosts, "wanted_character", event.WantedCharacterID, db); err != nil {
			fmt.Printf("Error pushing wanted character %d to Sonic: %v\n", event.WantedCharacterID, err)
		} else {
			updateSonicCursor(Services.SonicBucketWantedPosts, event.WantedCharacterID, db)
		}
	})

	// Remove a soft-deleted post from its Sonic bucket.
	Events.Subscribe(Events.PostCreated, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.PostCreatedEvent)
		if !ok || event.Type != "post_deleted" {
			return
		}
		if !Services.SonicAvailable() {
			return
		}

		var topicType Entities.TopicType
		if err := db.QueryRow("SELECT type FROM topics WHERE id = ?", event.TopicID).Scan(&topicType); err != nil {
			return
		}

		var bucket string
		switch topicType {
		case Entities.EpisodeTopic:
			bucket = Services.SonicBucketGamePosts
		case Entities.GeneralTopic:
			bucket = Services.SonicBucketGeneralPosts
		case Entities.LoreTopic:
			bucket = Services.SonicBucketLorePosts
		default:
			return
		}

		if err := Services.SonicDelete(Services.SonicCollection, bucket, strconv.Itoa(event.Post.Id)); err != nil {
			fmt.Printf("Error deleting post %d from Sonic: %v\n", event.Post.Id, err)
		}
	})

	// When topics are batch-deleted, remove their posts and any character/wanted-character
	// entities from Sonic. Episode entities are handled separately via EpisodeTopicsDeleted.
	Events.Subscribe(Events.TopicsDeleted, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.TopicsDeletedEvent)
		if !ok || len(event.TopicIDs) == 0 {
			return
		}
		if !Services.SonicAvailable() {
			return
		}

		placeholders := strings.Repeat("?,", len(event.TopicIDs)-1) + "?"
		args := make([]interface{}, len(event.TopicIDs))
		for i, id := range event.TopicIDs {
			args[i] = id
		}

		// Remove all posts belonging to the deleted topics, grouped by bucket.
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
				var bucket string
				switch topicType {
				case Entities.EpisodeTopic:
					bucket = Services.SonicBucketGamePosts
				case Entities.GeneralTopic:
					bucket = Services.SonicBucketGeneralPosts
				case Entities.LoreTopic:
					bucket = Services.SonicBucketLorePosts
				default:
					continue
				}
				postIDs[bucket] = append(postIDs[bucket], strconv.FormatInt(postID, 10))
			}
		}
		for bucket, ids := range postIDs {
			if err := Services.SonicDeleteBatch(Services.SonicCollection, bucket, ids); err != nil {
				fmt.Printf("Error deleting posts from Sonic bucket %s on topic delete: %v\n", bucket, err)
			}
		}

		// Remove character entities whose topics were deleted.
		charRows, err := db.Query(
			fmt.Sprintf("SELECT id FROM character_base WHERE topic_id IN (%s)", placeholders),
			args...,
		)
		if err == nil {
			defer charRows.Close()
			for charRows.Next() {
				var charID int64
				if charRows.Scan(&charID) == nil {
					_ = Services.SonicDelete(Services.SonicCollection, Services.SonicBucketCharacters, strconv.FormatInt(charID, 10))
				}
			}
		}

		// Remove wanted-character entities whose topics were deleted.
		wantedRows, err := db.Query(
			fmt.Sprintf("SELECT id FROM wanted_character_base WHERE topic_id IN (%s)", placeholders),
			args...,
		)
		if err == nil {
			defer wantedRows.Close()
			for wantedRows.Next() {
				var wcID int64
				if wantedRows.Scan(&wcID) == nil {
					_ = Services.SonicDelete(Services.SonicCollection, Services.SonicBucketWantedPosts, strconv.FormatInt(wcID, 10))
				}
			}
		}
	})

	// Remove a wiped user's general posts from Sonic.
	Events.Subscribe(Events.UserWiped, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.UserWipedEvent)
		if !ok || len(event.DeletedGeneralPostIDs) == 0 {
			return
		}
		if !Services.SonicAvailable() {
			return
		}

		ids := make([]string, len(event.DeletedGeneralPostIDs))
		for i, id := range event.DeletedGeneralPostIDs {
			ids[i] = strconv.Itoa(id)
		}
		if err := Services.SonicDeleteBatch(Services.SonicCollection, Services.SonicBucketGeneralPosts, ids); err != nil {
			fmt.Printf("Error deleting general posts from Sonic on user wipe: %v\n", err)
		}
	})

	// Remove episode entities from Sonic when their topics are deleted.
	Events.Subscribe(Events.EpisodeTopicsDeleted, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.EpisodeTopicsDeletedEvent)
		if !ok || len(event.EpisodeIDs) == 0 {
			return
		}
		if !Services.SonicAvailable() {
			return
		}

		for _, id := range event.EpisodeIDs {
			if err := Services.SonicDelete(Services.SonicCollection, Services.SonicBucketEpisodes, strconv.Itoa(id)); err != nil {
				fmt.Printf("Error deleting episode %d from Sonic: %v\n", id, err)
			}
		}
	})
}
