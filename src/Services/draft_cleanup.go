package Services

import (
	"database/sql"
	"log"
	"time"
)

func StartDraftCleanupScheduler(db *sql.DB) {
	go func() {
		cleanPublishedPostDrafts(db)
		ticker := time.NewTicker(7 * 24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			cleanPublishedPostDrafts(db)
		}
	}()
}

func cleanPublishedPostDrafts(db *sql.DB) {
	res, err := db.Exec(`
		DELETE FROM post_drafts
		WHERE is_published = 1
		  AND entity_type = 'post'
		  AND date_created < NOW() - INTERVAL 1 MONTH
	`)
	if err != nil {
		log.Printf("Draft cleanup: %v", err)
		return
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		log.Printf("Draft cleanup: deleted %d published post drafts", n)
	}
}
