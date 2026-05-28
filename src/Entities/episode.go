package Entities

type EpisodeStatus int

const (
	ActiveEpisode   EpisodeStatus = 0
	InactiveEpisode EpisodeStatus = 1
)

type Episode struct {
	Id              int               `json:"id" db:"id"`
	Topic_Id        int               `json:"topic_id" db:"topic_id"`
	Name            string            `json:"name" db:"name"`
	OpenToEveryone  bool              `json:"open_to_everyone" db:"open_to_everyone"`
	EpisodeStatus   EpisodeStatus     `json:"episode_status" db:"episode_status"`
	RatingSet       bool              `json:"rating_set" db:"rating_set"`
	RatingLanguage  int               `json:"rating_language" db:"rating_language"`
	RatingViolence  int               `json:"rating_violence" db:"rating_violence"`
	RatingSex       int               `json:"rating_sex" db:"rating_sex"`
	Characters      []*ShortCharacter `json:"characters" db:"-"`
	Masks           []ShortMask       `json:"masks" db:"-"`
	HasWarnings     bool              `json:"has_warnings" db:"-"`
	WarningsConsent bool              `json:"warnings_consent" db:"-"`
	Warnings        []StandardWarning `json:"warnings" db:"-"`
	CustomFields    CustomFieldEntity `json:"custom_fields" db:"-"`
	CanEdit         *bool             `json:"can_edit,omitempty" db:"-"`
}

type StandardWarning struct {
	Id             int    `json:"id" db:"id"`
	Name           string `json:"name" db:"name"`
	Description    string `json:"description" db:"description"`
	Locale         string `json:"locale" db:"locale"`
	RatingLanguage int    `json:"rating_language" db:"rating_language"`
	RatingViolence int    `json:"rating_violence" db:"rating_violence"`
	RatingSex      int    `json:"rating_sex" db:"rating_sex"`
}

func (e *Episode) GetBaseFields() []string {
	return []string{"topic_id", "name", "open_to_everyone", "episode_status", "rating_set", "rating_language", "rating_violence", "rating_sex"}
}
