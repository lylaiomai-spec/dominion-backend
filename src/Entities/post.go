package Entities

import "time"

type PostReactionUser struct {
	Id   int    `json:"id"`
	Name string `json:"name"`
}

type PostReaction struct {
	ReactionId int                `json:"reaction_id"`
	Url        string             `json:"url"`
	Number     int                `json:"number"`
	Users      []PostReactionUser `json:"users"`
}

type Post struct {
	Id                   int               `json:"id"`
	TopicId              int               `json:"topic_id"`
	AuthorUserId         int               `json:"author_user_id"`
	AuthorUserName       string            `json:"author_user_name"`
	GuestName            *string           `json:"guest_name,omitempty"`
	AuthorCharacterId    *int              `json:"author_character_id"`
	DateCreated          time.Time         `json:"date_created"`
	DateCreatedLocalized string            `json:"date_created_localized,omitempty"`
	Content              string            `json:"content"`
	ContentHtml          string            `json:"content_html"`
	CharacterProfile     *CharacterProfile `json:"character_profile"`
	UserProfile          *UserProfile      `json:"user_profile"`
	UseCharacterProfile  bool              `json:"use_character_profile"`
	CanEdit              *bool             `json:"can_edit,omitempty" db:"-"`
	CanDelete            *bool             `json:"can_delete,omitempty" db:"-"`
	Reactions            []PostReaction    `json:"reactions"`
}
