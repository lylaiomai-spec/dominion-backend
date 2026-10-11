package EventHandlers

import (
	"cuento-backend/src/Events"
	"database/sql"
	"regexp"
	"strconv"
)

var npcTagRe = regexp.MustCompile(`(?i)\[npc\s+id=["']?(\d+)["']?`)

func extractNpcIDs(content string) []int {
	matches := npcTagRe.FindAllStringSubmatch(content, -1)
	seen := map[int]bool{}
	var ids []int
	for _, m := range matches {
		if id, err := strconv.Atoi(m[1]); err == nil && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

func RegisterNpcEventHandlers() {
	Events.Subscribe(Events.NpcUsed, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.NpcUsedEvent)
		if !ok {
			return
		}

		npcIDs := extractNpcIDs(event.Content)

		if event.IsUpdate {
			db.Exec("DELETE FROM npc_post WHERE post_id = ?", event.PostID)
		}

		for _, npcID := range npcIDs {
			db.Exec("INSERT IGNORE INTO npc_post (post_id, npc_id) VALUES (?, ?)", event.PostID, npcID)
		}
	})
}
