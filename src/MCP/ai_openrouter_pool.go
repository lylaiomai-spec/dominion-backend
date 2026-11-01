package MCP

import (
	"cuento-backend/src/Entities"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

const openRouterBaseURL = "https://openrouter.ai/api/v1"

// minContextLength maps our size enum to the minimum context window a model
// must have to qualify. Used as a proxy for model capability.
var minContextForSize = map[Entities.AIModelSize]int{
	Entities.AIModelSizeTiny:   4096,
	Entities.AIModelSizeSmall:  16384,
	Entities.AIModelSizeMedium: 65536,
	Entities.AIModelSizeLarge:  131072,
}

// providerPrefixForProtocol maps our protocol enum to the OpenRouter model ID
// prefix used to filter by underlying provider.
var providerPrefixForProtocol = map[Entities.AIModelProtocolType]string{
	Entities.AIModelProtocolOpenAI:    "openai/",
	Entities.AIModelProtocolAnthropic: "anthropic/",
	Entities.AIModelProtocolGemini:    "google/",
}

type openRouterModel struct {
	ID           string `json:"id"`
	ContextLen   int    `json:"context_length"`
	Architecture struct {
		Modality string `json:"modality"`
	} `json:"architecture"`
	Pricing struct {
		Prompt string `json:"prompt"`
	} `json:"pricing"`
	SupportedParameters []string `json:"supported_parameters"`
}

type OpenRouterModelPool struct {
	apiKey      string
	useFreeOnly bool
}

func NewOpenRouterModelPool(apiKey string, useFreeOnly bool) *OpenRouterModelPool {
	return &OpenRouterModelPool{apiKey: apiKey, useFreeOnly: useFreeOnly}
}

func (p *OpenRouterModelPool) ClientForMinSize(minSize Entities.AIModelSize, modelType Entities.AIModelType) (AIClient, error) {
	return p.selectModel(minSize, modelType, "", nil)
}

func (p *OpenRouterModelPool) ClientForMinSizeAndProtocol(minSize Entities.AIModelSize, modelType Entities.AIModelType, _ Entities.AIModelProtocolType) (AIClient, error) {
	// OpenRouter exposes all providers through one OpenAI-compat API, so
	// protocol-based prefix filtering doesn't apply here.
	return p.selectModel(minSize, modelType, "", nil)
}

func (p *OpenRouterModelPool) ClientExcluding(minSize Entities.AIModelSize, modelType Entities.AIModelType, excluded []string) (AIClient, error) {
	return p.selectModel(minSize, modelType, "", excluded)
}

func (p *OpenRouterModelPool) selectModel(minSize Entities.AIModelSize, modelType Entities.AIModelType, providerPrefix string, excluded []string) (AIClient, error) {
	models, err := p.fetchModels()
	if err != nil {
		return nil, fmt.Errorf("openrouter: failed to fetch models: %w", err)
	}

	minCtx := minContextForSize[minSize]

	// Providers known to be broken or chronically degraded on the free tier.
	brokenProviders := []string{"recursive/", "xingchen-agi/", "dots-studio/"}

	// Specific models known to consistently time out or return garbage.
	brokenModels := map[string]bool{
		"nvidia/nemotron-3.5-super-vl:free": true,
		"liquid/lfm-2.5-2.6b:free":         true,
	}

	excludedSet := make(map[string]bool, len(excluded))
	for _, id := range excluded {
		excludedSet[id] = true
	}

	var candidates []openRouterModel
	for _, m := range models {
		if m.ContextLen < minCtx {
			continue
		}
		if !modalityMatchesType(m.Architecture.Modality, modelType) {
			continue
		}
		if providerPrefix != "" && !strings.HasPrefix(m.ID, providerPrefix) {
			continue
		}
		if p.useFreeOnly && !strings.HasSuffix(m.ID, ":free") {
			continue
		}
		if !supportsTools(m.SupportedParameters) {
			continue
		}
		if excludedSet[m.ID] {
			continue
		}
		if brokenModels[m.ID] {
			continue
		}
		broken := false
		for _, prefix := range brokenProviders {
			if strings.HasPrefix(m.ID, prefix) {
				broken = true
				break
			}
		}
		if broken {
			continue
		}
		candidates = append(candidates, m)
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("openrouter: no model available for size=%d type=%d prefix=%q", minSize, modelType, providerPrefix)
	}

	// Pick the cheapest qualifying model (lowest prompt price = smallest sufficient model).
	sort.Slice(candidates, func(i, j int) bool {
		return parsePrice(candidates[i].Pricing.Prompt) < parsePrice(candidates[j].Pricing.Prompt)
	})

	chosen := candidates[0]
	log.Printf("OpenRouter: selected model %s (context=%d, free=%v)", chosen.ID, chosen.ContextLen, p.useFreeOnly)
	return NewOpenAICompatClient(p.apiKey, chosen.ID, openRouterBaseURL)
}

func (p *OpenRouterModelPool) fetchModels() ([]openRouterModel, error) {
	req, err := http.NewRequest(http.MethodGet, openRouterBaseURL+"/models", nil)
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
		return nil, fmt.Errorf("openrouter /models returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Data []openRouterModel `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result.Data, nil
}

// OpenRouterModelInfo is the public shape returned by FetchOpenRouterModels.
type OpenRouterModelInfo struct {
	ID         string `json:"id"`
	ContextLen int    `json:"context_length"`
	Modality   string `json:"modality"`
	Price      string `json:"price_prompt"`
}

// FetchOpenRouterModels fetches the raw model catalogue from OpenRouter.
func FetchOpenRouterModels(apiKey string) ([]OpenRouterModelInfo, error) {
	pool := &OpenRouterModelPool{apiKey: apiKey}
	raw, err := pool.fetchModels()
	if err != nil {
		return nil, err
	}
	out := make([]OpenRouterModelInfo, 0, len(raw))
	for _, m := range raw {
		out = append(out, OpenRouterModelInfo{
			ID:         m.ID,
			ContextLen: m.ContextLen,
			Modality:   m.Architecture.Modality,
			Price:      m.Pricing.Prompt,
		})
	}
	return out, nil
}

// modalityMatchesType checks whether a model's OpenRouter modality string
// matches the requested AIModelType (text output vs. image output).
func modalityMatchesType(modality string, modelType Entities.AIModelType) bool {
	switch modelType {
	case Entities.AIModelTypeText:
		// Modality ends with "->text", e.g. "text->text" or "text+image->text"
		return strings.HasSuffix(modality, "->text")
	case Entities.AIModelTypeImage:
		return strings.HasSuffix(modality, "->image")
	}
	return false
}

func parsePrice(s string) float64 {
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

func supportsTools(params []string) bool {
	for _, p := range params {
		if p == "tools" {
			return true
		}
	}
	return false
}
