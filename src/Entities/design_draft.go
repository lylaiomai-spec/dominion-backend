package Entities

import "time"

type DesignDraft struct {
	Id                      int       `json:"id"`
	Name                    string    `json:"name"`
	SessionKey              string    `json:"session_key"`
	DateCreated             time.Time `json:"date_created"`
	DateLastChanged         time.Time `json:"date_last_changed"`
	DateCreatedLocalized    string    `json:"date_created_localized,omitempty"`
	DateLastChangedLocalized string   `json:"date_last_changed_localized,omitempty"`
	MainCss                 string    `json:"main_css"`
	CustomStyleCss          string    `json:"custom_style_css"`
}

type DesignDraftListItem struct {
	Id                      int       `json:"id"`
	Name                    string    `json:"name"`
	SessionKey              string    `json:"session_key"`
	DateCreated             time.Time `json:"date_created"`
	DateLastChanged         time.Time `json:"date_last_changed"`
	DateCreatedLocalized    string    `json:"date_created_localized,omitempty"`
	DateLastChangedLocalized string   `json:"date_last_changed_localized,omitempty"`
}
