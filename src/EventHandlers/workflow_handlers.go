package EventHandlers

import (
	"cuento-backend/src/Services"
	"database/sql"
	"encoding/json"
	"fmt"
)

type WorkflowHandlerFunc func(db *sql.DB, topicID int64, config json.RawMessage)

var workflowHandlers = map[string]WorkflowHandlerFunc{
	"MoveTopic": moveTopicHandler,
}

func moveTopicHandler(db *sql.DB, topicID int64, config json.RawMessage) {
	var cfg struct {
		TargetSubforumID int `json:"target_subforum_id"`
	}
	if err := json.Unmarshal(config, &cfg); err != nil || cfg.TargetSubforumID == 0 {
		fmt.Printf("MoveTopic workflow: invalid config: %v\n", err)
		return
	}

	if err := Services.MoveTopics(db, []int{int(topicID)}, cfg.TargetSubforumID); err != nil {
		fmt.Printf("MoveTopic workflow: failed to move topic %d: %v\n", topicID, err)
	}
}
