package Entities

type CharacterProfile struct {
	Id            int               `json:"id"`
	CharacterId   *int              `json:"character_id"`
	CharacterName string            `json:"character_name"`
	MaskName      *string           `json:"mask_name"`
	Avatar        *string           `json:"avatar"`
	CustomFields  CustomFieldEntity `json:"custom_fields"`
	CanEdit       *bool             `json:"can_edit,omitempty" db:"-"`
	UserId        *int              `json:"user_id"`
	IsMask        *bool             `json:"is_mask"`
	Signature     *string           `json:"signature"`
	SignatureHtml *string           `json:"signature_html"`
	Factions      []Faction         `json:"factions"`
}

func (cp *CharacterProfile) GetBaseFields() []string {
	return []string{"character_id", "avatar", "is_mask", "user_id", "mask_name", "signature"}
}

type ShortMask struct {
	Id       int    `json:"id"`
	MaskName string `json:"mask_name"`
	UserId   int    `json:"user_id"`
	UserName string `json:"user_name"`
}
