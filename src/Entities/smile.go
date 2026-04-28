package Entities

type SmileCategory struct {
	Id   int    `json:"id"`
	Name string `json:"name"`
}

type Smile struct {
	Id       int            `json:"id"`
	TextForm string         `json:"text_form"`
	URL      string         `json:"url"`
	Category *SmileCategory `json:"category,omitempty"`
}
