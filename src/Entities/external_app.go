package Entities

type ExternalApp struct {
	Id     int    `json:"id"`
	Name   string `json:"name"`
	ApiKey string `json:"api_key"`
	Status bool   `json:"status"`
	UserId *int   `json:"user_id"`
}

type ExternalAppPermission string

const (
	ExternalAppPermissionPost            ExternalAppPermission = "post"
	ExternalAppPermissionGetActiveTopics ExternalAppPermission = "get_active_topics"
	ExternalAppPermissionGetFirstPost    ExternalAppPermission = "get_first_post"
	ExternalAppPermissionGetPost         ExternalAppPermission = "get_post"
	ExternalAppPermissionUpdatePost      ExternalAppPermission = "update_post"
)
