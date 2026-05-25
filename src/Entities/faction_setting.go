package Entities

type FactionSetting struct {
	Id              int          `json:"id"`
	Level           int          `json:"level"`
	HumanName       string       `json:"human_name"`
	ParentFactionId *int         `json:"parent_faction_id"`
	Parent          *FactionInfo `json:"parent,omitempty"`
}
