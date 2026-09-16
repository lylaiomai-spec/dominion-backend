package Entities

import "encoding/json"

type Workflow struct {
	Id              int              `json:"id"`
	EventName       string           `json:"event_name"`
	SubforumIds     string           `json:"subforum_ids"`
	HandlerFunction string           `json:"handler_function"`
	Config          json.RawMessage  `json:"config"`
	EventConfig     *json.RawMessage `json:"event_config"`
}
