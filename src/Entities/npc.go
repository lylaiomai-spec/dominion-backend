package Entities

type NPCArc struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

type NPC struct {
	ID           int     `json:"id"`
	Name         string  `json:"name"`
	Avatar       *string `json:"avatar"`
	Description  *string `json:"description"`
	DisplayOrder int     `json:"display_order"`
	Arc          *NPCArc `json:"arc,omitempty"`
	CanEdit      *bool   `json:"can_edit,omitempty"`
}
