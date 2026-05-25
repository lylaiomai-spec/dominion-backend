package Entities

import "time"

type MaskStats struct {
	Id           int        `json:"id"`
	UserId       int        `json:"user_id"`
	TotalPosts   int        `json:"total_posts"`
	DateLastPost *time.Time `json:"date_last_post"`
}
