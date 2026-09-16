package Services

import (
	"cuento-backend/src/Events"
	"database/sql"
	"fmt"
	"strings"
)

func MoveTopics(db *sql.DB, topicIDs []int, targetSubforumID int) error {
	placeholders := strings.Repeat("?,", len(topicIDs)-1) + "?"

	idArgs := make([]interface{}, len(topicIDs))
	for i, id := range topicIDs {
		idArgs[i] = id
	}

	sourceRows, err := db.Query(fmt.Sprintf("SELECT DISTINCT subforum_id FROM topics WHERE id IN (%s)", placeholders), idArgs...)
	var sourceSubforumIDs []int
	if err == nil {
		defer sourceRows.Close()
		for sourceRows.Next() {
			var sfID int
			if sourceRows.Scan(&sfID) == nil {
				sourceSubforumIDs = append(sourceSubforumIDs, sfID)
			}
		}
	}

	moveArgs := make([]interface{}, 0, len(topicIDs)+1)
	moveArgs = append(moveArgs, targetSubforumID)
	moveArgs = append(moveArgs, idArgs...)

	_, err = db.Exec(fmt.Sprintf("UPDATE topics SET subforum_id = ? WHERE id IN (%s)", placeholders), moveArgs...)
	if err != nil {
		return err
	}

	affected := make(map[int]bool)
	affected[targetSubforumID] = true
	for _, id := range sourceSubforumIDs {
		affected[id] = true
	}
	subforumIDs := make([]int, 0, len(affected))
	for id := range affected {
		subforumIDs = append(subforumIDs, id)
	}

	Events.Publish(db, Events.TopicsMoved, Events.TopicsMovedEvent{SubforumIDs: subforumIDs})
	return nil
}
