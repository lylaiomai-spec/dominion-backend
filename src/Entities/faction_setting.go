package Entities

type FactionSetting struct {
	Id              int          `json:"id"`
	Level           int          `json:"level"`
	HumanName       string       `json:"human_name"`
	ParentFactionId *int         `json:"parent_faction_id"`
	Parent          *FactionInfo `json:"parent,omitempty"`
}

type FreeFormatDateSetting struct {
	Id             int            `json:"id"`
	Name           string         `json:"name"`
	FreeFormatDate FreeFormatDate `json:"free_format_date"`
}

type FactionFreeFormatDateItem struct {
	Id                    int                    `json:"id"`
	Name                  string                 `json:"name"`
	FreeFormatDateSetting *FreeFormatDateSetting `json:"free_format_date_setting"`
}
