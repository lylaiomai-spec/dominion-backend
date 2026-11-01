package Entities

type AIModelSize int

const (
	AIModelSizeTiny   AIModelSize = 1
	AIModelSizeSmall  AIModelSize = 2
	AIModelSizeMedium AIModelSize = 3
	AIModelSizeLarge  AIModelSize = 4
)

type AIModelProtocolType int

const (
	AIModelProtocolOpenAI    AIModelProtocolType = 1
	AIModelProtocolAnthropic AIModelProtocolType = 2
	AIModelProtocolGemini    AIModelProtocolType = 3
)

type AIModelType int

const (
	AIModelTypeText  AIModelType = 1
	AIModelTypeImage AIModelType = 2
)

type AIModel struct {
	Id           int                 `json:"id"`
	Name         string              `json:"name"`
	Size         AIModelSize         `json:"size"`
	ApiAddress   string              `json:"api_address"`
	ApiKey       string              `json:"api_key"`
	MachineName  string              `json:"machine_name"`
	IsActive     bool                `json:"is_active"`
	ProtocolType AIModelProtocolType `json:"protocol_type"`
	ModelType    AIModelType         `json:"model_type"`
}
