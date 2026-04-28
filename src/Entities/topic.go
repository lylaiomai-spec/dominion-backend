package Entities

import "time"

type TopicType int

const (
	GeneralTopic         TopicType = 0
	EpisodeTopic         TopicType = 1
	CharacterSheetTopic  TopicType = 2
	WantedCharacterTopic TopicType = 3
	LoreTopic            TopicType = 4
)

type TopicStatus int

const (
	ActiveTopic   TopicStatus = 0
	InactiveTopic TopicStatus = 1
	FullTopic     TopicStatus = 2
)

const TopicPostCap = 1000

type Topic struct {
	Id                    int                  `json:"id"`
	Status                TopicStatus          `json:"status"`
	Name                  string               `json:"name"`
	Type                  TopicType            `json:"type"`
	DateCreated           time.Time            `json:"date_created"`
	DateLastPost          time.Time            `json:"date_last_post"`
	DateLastPostLocalized string               `json:"date_last_post_localized,omitempty"`
	PostNumber            int                  `json:"post_number"`
	AuthorUserId          int                  `json:"author_user_id"`
	AuthorUsername        *string              `json:"author_username"`
	LastPostAuthorUserId  int                  `json:"last_post_author_user_id"`
	LastPostAuthorName    *string              `json:"last_post_author_name"`
	SubforumId            int                  `json:"subforum_id"`
	IsSticky              bool                 `json:"is_sticky"`
	IsStickyFirstPost     bool                 `json:"is_sticky_first_post"`
	Episode               *Episode             `json:"episode"`
	Character             *Character           `json:"character"`
	WantedCharacter       *WantedCharacter     `json:"wanted_character"`
	CanEdit               *bool                `json:"can_edit,omitempty" db:"-"`
	Permissions           *SubforumPermissions `json:"permissions,omitempty" db:"-"`
}
