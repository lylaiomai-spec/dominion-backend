package Controllers

import (
	"cuento-backend/src/Entities"
	"cuento-backend/src/Middlewares"
	"database/sql"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

type BoardInfo struct {
	SiteName                       string              `json:"site_name"`
	Domain                         string              `json:"domain"`
	PostsPerPage                   int                 `json:"posts_per_page"`
	TotalUserNumber                int                 `json:"total_user_number"`
	TotalCharacterNumber           int                 `json:"total_character_number"`
	TotalEpisodeNumber             int                 `json:"total_episode_number"`
	TotalTopicNumber               int                 `json:"total_topic_number"`
	TotalPostNumber                int                 `json:"total_post_number"`
	TotalEpisodePostNumber         int                 `json:"total_episode_post_number"`
	LastRegisteredUser             *Entities.ShortUser `json:"last_registered_user"`
	VisualNavlinksAfterHeaderPanel string              `json:"visual_navlinks_after_header_panel"`
	AutoArchivingShowPageLink      string              `json:"auto_archiving_show_page_link"`
	AutoArchivingEnabled           string              `json:"auto_archiving_enabled"`
	AutoArchivingDays              int                 `json:"auto_archiving_days"`
	UseRatingSystem                string              `json:"use_rating_system"`
	SiteMaxRating                  string              `json:"site_max_rating"`
	UseImageUploading              string              `json:"use_image_uploading"`
	UserAvatarWidth                int                 `json:"user_avatar_width"`
	UserAvatarHeight               int                 `json:"user_avatar_height"`
	CharacterAvatarWidth           int                 `json:"character_avatar_width"`
	CharacterAvatarHeight          int                 `json:"character_avatar_height"`
	Features                       map[string]int      `json:"features"`
}

func GetBoard(c *gin.Context, db *sql.DB) {
	var boardInfo = BoardInfo{
		TotalUserNumber:        0,
		TotalCharacterNumber:   0,
		TotalEpisodeNumber:     0,
		TotalTopicNumber:       0,
		TotalPostNumber:        0,
		TotalEpisodePostNumber: 0,
		LastRegisteredUser:     nil,
	}

	rows, err := db.Query("SELECT setting_name, setting_value FROM global_settings WHERE setting_name IN ('site_name', 'domain', 'posts_per_page', 'visual_navlinks_after_header_panel', 'auto_archiving_show_page_link', 'auto_archiving_enabled', 'auto_archiving_days', 'use_rating_system', 'site_max_rating', 'use_image_uploading', 'user_avatar_width', 'user_avatar_height', 'character_avatar_width', 'character_avatar_height')")
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get global settings: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		var nullValue sql.NullString
		if err := rows.Scan(&name, &nullValue); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan settings: " + err.Error()})
			c.Abort()
			return
		}
		value := nullValue.String
		switch name {
		case "site_name":
			boardInfo.SiteName = value
		case "domain":
			boardInfo.Domain = value
		case "posts_per_page":
			boardInfo.PostsPerPage, _ = strconv.Atoi(value)
		case "visual_navlinks_after_header_panel":
			boardInfo.VisualNavlinksAfterHeaderPanel = value
		case "auto_archiving_show_page_link":
			boardInfo.AutoArchivingShowPageLink = value
		case "auto_archiving_enabled":
			boardInfo.AutoArchivingEnabled = value
		case "auto_archiving_days":
			boardInfo.AutoArchivingDays, _ = strconv.Atoi(value)
		case "use_rating_system":
			boardInfo.UseRatingSystem = value
		case "site_max_rating":
			boardInfo.SiteMaxRating = value
		case "use_image_uploading":
			boardInfo.UseImageUploading = value
		case "user_avatar_width":
			boardInfo.UserAvatarWidth, _ = strconv.Atoi(value)
		case "user_avatar_height":
			boardInfo.UserAvatarHeight, _ = strconv.Atoi(value)
		case "character_avatar_width":
			boardInfo.CharacterAvatarWidth, _ = strconv.Atoi(value)
		case "character_avatar_height":
			boardInfo.CharacterAvatarHeight, _ = strconv.Atoi(value)
		}
	}

	rows, err = db.Query("SELECT stat_name, stat_value, stat_secondary FROM global_stats WHERE stat_name IN ('total_user_number', 'total_character_number', 'total_episode_number', 'total_topic_number', 'total_post_number', 'total_episode_post_number', 'last_user')")
	if err != nil {
		_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to get global stats: " + err.Error()})
		c.Abort()
		return
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		var value sql.NullInt64
		var secondary sql.NullString
		if err := rows.Scan(&name, &value, &secondary); err != nil {
			_ = c.Error(&Middlewares.AppError{Code: http.StatusInternalServerError, Message: "Failed to scan stats: " + err.Error()})
			c.Abort()
			return
		}
		switch name {
		case "total_user_number":
			boardInfo.TotalUserNumber = int(value.Int64)
		case "total_character_number":
			boardInfo.TotalCharacterNumber = int(value.Int64)
		case "total_episode_number":
			boardInfo.TotalEpisodeNumber = int(value.Int64)
		case "total_topic_number":
			boardInfo.TotalTopicNumber = int(value.Int64)
		case "total_post_number":
			boardInfo.TotalPostNumber = int(value.Int64)
		case "total_episode_post_number":
			boardInfo.TotalEpisodePostNumber = int(value.Int64)
		case "last_user":
			if value.Valid && secondary.Valid && value.Int64 > 0 {
				boardInfo.LastRegisteredUser = &Entities.ShortUser{
					Id:       int(value.Int64),
					Username: secondary.String,
				}
			}
		}
	}

	boardInfo.Features = map[string]int{}
	featureRows, err := db.Query("SELECT `key`, is_active FROM features")
	if err == nil {
		defer featureRows.Close()
		for featureRows.Next() {
			var key string
			var isActive bool
			if featureRows.Scan(&key, &isActive) == nil {
				if isActive {
					boardInfo.Features[key] = 1
				} else {
					boardInfo.Features[key] = 0
				}
			}
		}
	}

	c.JSON(http.StatusOK, boardInfo)
}
