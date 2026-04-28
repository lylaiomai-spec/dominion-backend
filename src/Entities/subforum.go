package Entities

type Subform struct {
	Id                    int                  `json:"id"`
	CategoryId            int                  `json:"category_id"`
	Name                  string               `json:"name"`
	Description           string               `json:"description"`
	DescriptionHtml       string               `json:"description_html,omitempty"`
	Position              int                  `json:"position"`
	TopicNumber           int                  `json:"topic_number"`
	PostNumber            int                  `json:"post_number"`
	LastPostTopicId       *int                 `json:"last_post_topic_id"`
	LastPostTopicName     *string              `json:"last_post_topic_name"`
	LastPostId            *int                 `json:"last_post_id"`
	DateLastPost          *string              `json:"date_last_post"`
	DateLastPostLocalized *string              `json:"date_last_post_localized,omitempty"`
	LastPostAuthorName    *string              `json:"last_post_author_name"`
	ShowLastTopic         *bool                `json:"show_last_topic"`
	Permissions           *SubforumPermissions `json:"permissions"`
}

type ShortSubform struct {
	Id   int    `json:"id"`
	Name string `json:"name"`
}

type SubforumPermissions struct {
	SubforumCreateGeneralTopic         bool `json:"subforum_create_general_topic"`
	SubforumCreateEpisodeTopic         bool `json:"subforum_create_episode_topic"`
	SubforumCreateCharacterTopic       bool `json:"subforum_create_character_topic"`
	SubforumCreateWantedCharacterTopic bool `json:"subforum_create_wanted_character_topic"`
	SubforumCreateLoreTopic            bool `json:"subforum_create_lore_topic"`
	SubforumPost                       bool `json:"subforum_post"`
	SubforumDeleteOwnTopic             bool `json:"subforum_delete_topic"`
	SubforumDeleteOthersTopic          bool `json:"subforum_delete_others_topic"`
	SubforumEditOwnTopic               bool `json:"subforum_edit_own_topic"`
	SubforumEditOthersTopic            bool `json:"subforum_edit_others_topic"`
	SubforumEditOthersPost             bool `json:"subforum_edit_others_post"`
	SubforumEditOwnPost                bool `json:"subforum_edit_own_post"`
}
