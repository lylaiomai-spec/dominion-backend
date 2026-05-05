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
