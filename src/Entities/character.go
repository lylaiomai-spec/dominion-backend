package Entities

import "time"

type Character struct {
	Id              int               `json:"id"`
	UserId          int               `json:"user_id"`
	Name            string            `json:"name"`
	Avatar          *string           `json:"avatar"`
	CustomFields    CustomFieldEntity `json:"custom_fields" db:"-"`
	CharacterStatus CharacterStatus   `json:"character_status"`
	TopicId         int               `json:"topic_id"`
	TotalEpisodes   int               `json:"total_episodes"`
	Factions        []Faction         `json:"factions" db:"-"`
	Episodes        []EpisodeListItem `json:"episodes" db:"-"`
	CanEdit         *bool             `json:"can_edit,omitempty" db:"-"`
	ClaimRecord     *ClaimRecord      `json:"claim_record,omitempty" db:"-"`
}

type EpisodeListItem struct {
	Id                     int               `json:"id"`
	Name                   string            `json:"name"`
	TopicId                int               `json:"topic_id"`
	Characters             []*ShortCharacter `json:"characters"`
	DateLastPost           *time.Time        `json:"date_last_post"`
	LastPostAuthorUsername *string           `json:"last_post_author_username"`
}

func (c *Character) GetBaseFields() []string {
	return []string{"user_id", "name", "avatar", "character_status", "topic_id", "total_episodes"}
}

type ShortCharacter struct {
	Id   int    `json:"id"`
	Name string `json:"name"`
}

type CharacterStatus int

const (
	ActiveCharacter   CharacterStatus = 0
	InactiveCharacter CharacterStatus = 1
	PendingCharacter  CharacterStatus = 2
	DeclinedCharacter CharacterStatus = 3
)

type ClaimRecord struct {
	Id                             int       `json:"id"`
	ClaimId                        int       `json:"claim_id"`
	UserId                         *int      `json:"user_id"`
	GuestHash                      *string   `json:"guest_hash"`
	IsGuest                        bool      `json:"is_guest"`
	ClaimDate                      time.Time `json:"claim_date"`
	ClaimExpirationDate            time.Time `json:"claim_expiration_date"`
	CharacterId                    *int      `json:"character_id"`
	ClaimCreatedWithCharacterSheet *bool     `json:"claim_created_with_character_sheet"`
	ClaimAuthorId                  *int      `json:"claim_author_id"`
	ClaimAuthorUsername            *string   `json:"claim_author_username"`
}

type CharacterClaim struct {
	Id                      int     `json:"id"`
	Name                    string  `json:"name"`
	Description             *string `json:"description"`
	IsClaimed               bool    `json:"is_claimed"`
	ClaimRecordId           *int    `json:"claim_record_id"`
	CanChangeName           bool    `json:"can_change_name"`
	ShowOnlyWithActiveClaim bool    `json:"show_only_with_active_claim"`
}

type WantedCharacterStatus int

const (
	ActiveWantedCharacter   WantedCharacterStatus = 0
	InactiveWantedCharacter WantedCharacterStatus = 1
)

type WantedCharacter struct {
	Id                    int                   `json:"id"`
	Name                  string                `json:"name"`
	IsClaimed             bool                  `json:"is_claimed"`
	AuthorUserId          int                   `json:"author_user_id"`
	DateCreated           time.Time             `json:"date_created"`
	CharacterClaimId      *int                  `json:"character_claim_id"`
	IsDeleted             *bool                 `json:"is_deleted"`
	TopicId               int                   `json:"topic_id"`
	WantedCharacterStatus WantedCharacterStatus `json:"wanted_character_status"`
	CustomFields          CustomFieldEntity     `json:"custom_fields" db:"-"`
	Factions              []Faction             `json:"factions" db:"-"`
	ClaimRecord           *ClaimRecord          `json:"claim_record,omitempty" db:"-"`
}

func (w *WantedCharacter) GetBaseFields() []string {
	return []string{"name", "is_claimed", "author_user_id", "date_created", "character_claim_id", "is_deleted", "topic_id", "wanted_character_status"}
}

type CharacterListItem struct {
	Id                  int     `json:"id"`
	Name                string  `json:"name"`
	IsClaim             bool    `json:"is_claim"`
	WantedCharacterId   *int    `json:"wanted_character_id"`
	ClaimRecordId       *int    `json:"claim_record_id,omitempty"`
	ClaimAuthorId       *int    `json:"claim_author_id,omitempty"`
	ClaimAuthorUsername *string `json:"claim_author_username,omitempty"`
	ClaimGuestHash      *string `json:"claim_guest_hash,omitempty"`
	ClaimExpirationDate string  `json:"claim_expiration_date,omitempty"`
}
