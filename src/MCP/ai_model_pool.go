package MCP

import (
	"cuento-backend/src/Entities"
	"database/sql"
	"fmt"
	"strings"
)

// ModelPool selects and instantiates an AI client based on size and type requirements.
type ModelPool interface {
	ClientForMinSize(minSize Entities.AIModelSize, modelType Entities.AIModelType) (AIClient, error)
	ClientForMinSizeAndProtocol(minSize Entities.AIModelSize, modelType Entities.AIModelType, protocol Entities.AIModelProtocolType) (AIClient, error)
	// ClientExcluding is like ClientForMinSize but skips models whose ID is in excluded.
	// Used to retry with a different model after a rate-limit failure.
	ClientExcluding(minSize Entities.AIModelSize, modelType Entities.AIModelType, excluded []string) (AIClient, error)
}

type AIModelPool struct {
	db *sql.DB
}

func NewAIModelPool(db *sql.DB) *AIModelPool {
	return &AIModelPool{db: db}
}

// ClientForMinSize returns the smallest active AI client of the given type whose size >= minSize.
func (p *AIModelPool) ClientForMinSize(minSize Entities.AIModelSize, modelType Entities.AIModelType) (AIClient, error) {
	return p.clientForMinSize(minSize, modelType, nil)
}

// ClientForMinSizeAndProtocol is like ClientForMinSize but also restricts to a specific protocol.
func (p *AIModelPool) ClientForMinSizeAndProtocol(minSize Entities.AIModelSize, modelType Entities.AIModelType, protocol Entities.AIModelProtocolType) (AIClient, error) {
	return p.clientForMinSize(minSize, modelType, &protocol)
}

func (p *AIModelPool) ClientExcluding(minSize Entities.AIModelSize, modelType Entities.AIModelType, excluded []string) (AIClient, error) {
	if len(excluded) == 0 {
		return p.clientForMinSize(minSize, modelType, nil)
	}
	placeholders := strings.Repeat("?,", len(excluded))
	placeholders = placeholders[:len(placeholders)-1]
	args := []any{minSize, modelType}
	for _, e := range excluded {
		args = append(args, e)
	}
	var apiAddress, apiKey, machineName string
	var protocolType int
	err := p.db.QueryRow(fmt.Sprintf(`
		SELECT api_address, api_key, machine_name, protocol_type
		FROM ai_models
		WHERE is_active = 1 AND size >= ? AND model_type = ? AND machine_name NOT IN (%s)
		ORDER BY size ASC LIMIT 1
	`, placeholders), args...).Scan(&apiAddress, &apiKey, &machineName, &protocolType)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no active AI model available (excluding %d tried models)", len(excluded))
	}
	if err != nil {
		return nil, fmt.Errorf("querying ai_models: %w", err)
	}
	switch Entities.AIModelProtocolType(protocolType) {
	case Entities.AIModelProtocolOpenAI:
		return NewOpenAICompatClient(apiKey, machineName, apiAddress)
	case Entities.AIModelProtocolAnthropic:
		return NewClaudeClient(apiKey, machineName)
	case Entities.AIModelProtocolGemini:
		return NewGeminiClient(apiKey, machineName)
	default:
		return nil, fmt.Errorf("unsupported protocol_type: %d", protocolType)
	}
}

func (p *AIModelPool) clientForMinSize(minSize Entities.AIModelSize, modelType Entities.AIModelType, protocol *Entities.AIModelProtocolType) (AIClient, error) {
	var (
		apiAddress, apiKey, machineName string
		protocolType                    int
	)

	var err error
	if protocol != nil {
		err = p.db.QueryRow(`
			SELECT api_address, api_key, machine_name, protocol_type
			FROM ai_models
			WHERE is_active = 1 AND size >= ? AND model_type = ? AND protocol_type = ?
			ORDER BY size ASC
			LIMIT 1
		`, minSize, modelType, *protocol).Scan(&apiAddress, &apiKey, &machineName, &protocolType)
	} else {
		err = p.db.QueryRow(`
			SELECT api_address, api_key, machine_name, protocol_type
			FROM ai_models
			WHERE is_active = 1 AND size >= ? AND model_type = ?
			ORDER BY size ASC
			LIMIT 1
		`, minSize, modelType).Scan(&apiAddress, &apiKey, &machineName, &protocolType)
	}

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no active AI model available with size >= %d and model_type = %d", minSize, modelType)
	}
	if err != nil {
		return nil, fmt.Errorf("querying ai_models: %w", err)
	}

	switch Entities.AIModelProtocolType(protocolType) {
	case Entities.AIModelProtocolOpenAI:
		return NewOpenAICompatClient(apiKey, machineName, apiAddress)
	case Entities.AIModelProtocolAnthropic:
		return NewClaudeClient(apiKey, machineName)
	case Entities.AIModelProtocolGemini:
		return NewGeminiClient(apiKey, machineName)
	default:
		return nil, fmt.Errorf("unsupported protocol_type: %d", protocolType)
	}
}
