package EventHandlers

import (
	"cuento-backend/src/Events"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func RegisterWorkflowEventHandlers() {
	Events.Subscribe(Events.TopicFull, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.TopicFullEvent)
		if !ok {
			return
		}
		dispatchWorkflows(db, string(Events.TopicFull), event.TopicID, event.SubforumID, nil)
	})

	Events.Subscribe(Events.TopicStatusChanged, func(db *sql.DB, data Events.EventData) {
		event, ok := data.(Events.TopicStatusChangedEvent)
		if !ok {
			return
		}
		dispatchWorkflows(db, string(Events.TopicStatusChanged), event.TopicID, event.SubforumID, func(eventConfig json.RawMessage) bool {
			var cfg struct {
				NewStatus *int `json:"new_status"`
			}
			if err := json.Unmarshal(eventConfig, &cfg); err != nil || cfg.NewStatus == nil {
				return true
			}
			return event.NewStatus == *cfg.NewStatus
		})
	})
}

func dispatchWorkflows(db *sql.DB, eventName string, topicID int64, subforumID int, matchEventConfig func(json.RawMessage) bool) {
	rows, err := db.Query(
		"SELECT subforum_ids, handler_function, config, event_config FROM workflows WHERE event_name = ?",
		eventName,
	)
	if err != nil {
		fmt.Printf("WorkflowHandler: failed to query workflows: %v\n", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var subforumIDs, handlerName string
		var config json.RawMessage
		var eventConfigRaw []byte
		if err := rows.Scan(&subforumIDs, &handlerName, &config, &eventConfigRaw); err != nil {
			continue
		}
		if !subforumInList(subforumID, subforumIDs) {
			continue
		}
		if matchEventConfig != nil && len(eventConfigRaw) > 0 {
			if !matchEventConfig(json.RawMessage(eventConfigRaw)) {
				continue
			}
		}
		handler, ok := workflowHandlers[handlerName]
		if !ok {
			fmt.Printf("WorkflowHandler: unknown handler function %q\n", handlerName)
			continue
		}
		handler(db, topicID, config)
	}
}

func subforumInList(subforumID int, list string) bool {
	for _, part := range strings.Split(list, ",") {
		id, err := strconv.Atoi(strings.TrimSpace(part))
		if err == nil && id == subforumID {
			return true
		}
	}
	return false
}
