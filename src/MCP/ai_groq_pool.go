package MCP

import (
	"cuento-backend/src/Entities"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
)

const groqBaseURL = "https://api.groq.com/openai/v1"

type groqModel struct {
	ID            string `json:"id"`
	ContextWindow int    `json:"context_window"`
	Active        bool   `json:"active"`
}

// GroqModelPool selects from Groq's free hosted models via their OpenAI-compatible API.
// All Groq models are text->text and support tool calling, so we only filter by context window.
type GroqModelPool struct {
	apiKey string
}

func NewGroqModelPool(apiKey string) *GroqModelPool {
	return &GroqModelPool{apiKey: apiKey}
}

func (p *GroqModelPool) ClientForMinSize(minSize Entities.AIModelSize, modelType Entities.AIModelType) (AIClient, error) {
	return p.selectModel(minSize, nil)
}

func (p *GroqModelPool) ClientForMinSizeAndProtocol(minSize Entities.AIModelSize, modelType Entities.AIModelType, _ Entities.AIModelProtocolType) (AIClient, error) {
	return p.selectModel(minSize, nil)
}

func (p *GroqModelPool) ClientExcluding(minSize Entities.AIModelSize, modelType Entities.AIModelType, excluded []string) (AIClient, error) {
	return p.selectModel(minSize, excluded)
}

func (p *GroqModelPool) selectModel(minSize Entities.AIModelSize, excluded []string) (AIClient, error) {
	models, err := p.fetchModels()
	if err != nil {
		return nil, fmt.Errorf("groq: failed to fetch models: %w", err)
	}

	minCtx := minContextForSize[minSize]

	excludedSet := make(map[string]bool, len(excluded))
	for _, id := range excluded {
		excludedSet[id] = true
	}

	var candidates []groqModel
	for _, m := range models {
		if !m.Active {
			continue
		}
		if m.ContextWindow < minCtx {
			continue
		}
		if excludedSet[m.ID] {
			continue
		}
		candidates = append(candidates, m)
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("groq: no model available for size=%d", minSize)
	}

	// Prefer larger context window as a proxy for model capability.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].ContextWindow > candidates[j].ContextWindow
	})

	chosen := candidates[0]
	log.Printf("Groq: selected model %s (context=%d)", chosen.ID, chosen.ContextWindow)
	return NewOpenAICompatClient(p.apiKey, chosen.ID, groqBaseURL)
}

// GroqModelInfo is the public shape returned by FetchGroqModels.
type GroqModelInfo struct {
	ID            string `json:"id"`
	ContextLength int    `json:"context_length"`
	Modality      string `json:"modality"`
	PricePrompt   string `json:"price_prompt"`
}

// FetchGroqModels fetches the raw model catalogue from Groq for the admin UI.
func FetchGroqModels(apiKey string) ([]GroqModelInfo, error) {
	pool := &GroqModelPool{apiKey: apiKey}
	raw, err := pool.fetchModels()
	if err != nil {
		return nil, err
	}
	out := make([]GroqModelInfo, 0, len(raw))
	for _, m := range raw {
		if !m.Active {
			continue
		}
		out = append(out, GroqModelInfo{
			ID:            m.ID,
			ContextLength: m.ContextWindow,
			Modality:      "text->text",
			PricePrompt:   "0",
		})
	}
	return out, nil
}

func (p *GroqModelPool) fetchModels() ([]groqModel, error) {
	req, err := http.NewRequest(http.MethodGet, groqBaseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("groq /models returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Data []groqModel `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result.Data, nil
}
