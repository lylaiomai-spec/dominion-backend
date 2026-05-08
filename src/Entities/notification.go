package Entities

import "time"

type NotificationBase struct {
	Id          int       `json:"id"`
	UserId      int       `json:"user_id"`
	Type        string    `json:"type"`
	Title       string    `json:"title"`
	Message     string    `json:"message"`
	DateCreated time.Time `json:"date_created"`
	IsRead      bool      `json:"is_read"`
}

type MentionNotification struct {
	NotificationBase
	Data NotificationMention `json:"data"`
}

type GameNotification struct {
	NotificationBase
	Data NotificationGame `json:"data"`
}

type SystemNotification struct {
	NotificationBase
	Data NotificationSystem `json:"data"`
}

type AccountUpdateNotification struct {
	NotificationBase
	Data NotificationAccountUpdate `json:"data"`
}

type DirectMessageNotification struct {
	NotificationBase
	Data NotificationDirectMessage `json:"data"`
}

type NotificationMention struct {
	UserId        int     `json:"user_id"`
	UserName      string  `json:"user_name"`
	CharacterId   *int    `json:"character_id"`
	CharacterName *string `json:"character_name"`
	PostId        int     `json:"post_id"`
	TopicId       int     `json:"topic_id"`
	TopicName     string  `json:"topic_name"`
}

type NotificationGame struct {
	TopicId           int    `json:"topic_id"`
	TopicName         string `json:"topic_name"`
	PostId            int    `json:"post_id"`
	Type              string `json:"type"`
	UserCharacterId   int    `json:"user_character_id"`
	UserCharacterName string `json:"user_character_name"`
	CharacterId       int    `json:"character_id"`
	CharacterName     string `json:"character_name"`
}

type NotificationSystem struct {
	TopicId int `json:"topic_id"`
}

type NotificationAutoArchiving struct {
	CharacterId   int    `json:"character_id"`
	CharacterName string `json:"character_name"`
	DaysLeft      *int   `json:"days_left,omitempty"`
}

type AutoArchivingNotification struct {
	NotificationBase
	Data NotificationAutoArchiving `json:"data"`
}

type NotificationAccountUpdate struct {
	IncomeTypeKey string  `json:"income_type_key"`
	Amount        int     `json:"amount"`
	TotalAmount   int     `json:"total_amount"`
	PostId        int     `json:"post_id"`
	TopicId       int     `json:"topic_id"`
	Comment       *string `json:"comment,omitempty"`
}

type NotificationDirectMessage struct {
	ChatId   int     `json:"chat_id"`
	Username string  `json:"username"`
	Avatar   *string `json:"avatar"`
}

type NotificationReaction struct {
	PostId     int    `json:"post_id"`
	TopicId    int64  `json:"topic_id"`
	TopicName  string `json:"topic_name"`
	ReactionId int    `json:"reaction_id"`
	Url        string `json:"url"`
	UserId     int    `json:"user_id"`
	UserName   string `json:"user_name"`
}

type ReactionNotification struct {
	NotificationBase
	Data NotificationReaction `json:"data"`
}

type UserNotificationSetting struct {
	NotificationType string `json:"notification_type"`
	DisableToast     bool   `json:"disable_toast"`
	DisableSound     bool   `json:"disable_sound"`
	DisableAll       bool   `json:"disable_all"`
}
