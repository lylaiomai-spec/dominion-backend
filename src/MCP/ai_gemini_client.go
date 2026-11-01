package MCP

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"google.golang.org/genai"
)

type GeminiClient struct {
	client *genai.Client
	model  string
}

func NewGeminiClient(apiKey string, model string) (*GeminiClient, error) {
	client, err := genai.NewClient(context.Background(), &genai.ClientConfig{
		APIKey: apiKey,
	})
	if err != nil {
		return nil, err
	}
	return &GeminiClient{client: client, model: model}, nil
}

func (g *GeminiClient) ModelID() string { return g.model }

func (g *GeminiClient) Chat(ctx context.Context, history []ChatMessage, systemInstruction string) (string, []ChatSource, error) {
	contents := make([]*genai.Content, len(history))
	for i, msg := range history {
		role := msg.Role
		if role == "assistant" {
			role = "model"
		}
		contents[i] = &genai.Content{
			Role:  role,
			Parts: []*genai.Part{{Text: msg.Content}},
		}
	}

	temperature := float32(0)
	cfg := &genai.GenerateContentConfig{
		Tools:       buildGeminiTools(),
		Temperature: &temperature,
	}
	if systemInstruction != "" {
		cfg.SystemInstruction = &genai.Content{
			Parts: []*genai.Part{{Text: systemInstruction}},
		}
	}

	var collectedSources []ChatSource
	citedTopicIDs := make(map[int64]bool)

	const maxRounds = 20
	const toolBudget = 15
	toolRounds := 0
	for round := 0; round < maxRounds; round++ {
		activeCfg := cfg
		if toolRounds >= toolBudget {
			activeCfg = &genai.GenerateContentConfig{
				Temperature:       cfg.Temperature,
				SystemInstruction: cfg.SystemInstruction,
			}
			if toolRounds == toolBudget {
				contents = append(contents, &genai.Content{
					Role:  "user",
					Parts: []*genai.Part{{Text: "You have reached the search limit. Do not call any more tools. Write your final answer now based only on the results you have already gathered."}},
				})
			}
		}
		result, err := g.client.Models.GenerateContent(ctx, g.model, contents, activeCfg)
		if err != nil {
			return "", nil, err
		}

		if len(result.Candidates) == 0 {
			return "", nil, fmt.Errorf("empty response from Gemini (no candidates)")
		}

		candidate := result.Candidates[0]
		if candidate.Content == nil {
			return "", nil, fmt.Errorf("empty response from Gemini (finish_reason=%v)", candidate.FinishReason)
		}

		var functionCalls []*genai.Part
		var textParts []*genai.Part
		for _, part := range candidate.Content.Parts {
			if part.FunctionCall != nil {
				functionCalls = append(functionCalls, part)
			} else if part.Text != "" {
				textParts = append(textParts, part)
			}
		}

		if len(functionCalls) == 0 {
			if len(textParts) > 0 {
				return textParts[0].Text, filterSourcesByCitations(collectedSources, citedTopicIDs), nil
			}
			return "", nil, fmt.Errorf("empty response from Gemini (finish_reason=%v)", candidate.FinishReason)
		}

		toolRounds++
		contents = append(contents, candidate.Content)

		responseParts := make([]*genai.Part, 0, len(functionCalls))
		for _, part := range functionCalls {
			fc := part.FunctionCall
			if fc.Name == "cite_sources" {
				for _, id := range postIDsFromArgs(fc.Args) {
					citedTopicIDs[id] = true
				}
			}
			executor, ok := findExecutor(fc.Name)
			if !ok {
				responseParts = append(responseParts, genai.NewPartFromFunctionResponse(fc.Name, map[string]any{
					"error": fmt.Sprintf("unknown tool: %s", fc.Name),
				}))
				continue
			}

			output, err := executor(ctx, fc.Args)
			if err != nil {
				responseParts = append(responseParts, genai.NewPartFromFunctionResponse(fc.Name, map[string]any{
					"error": err.Error(),
				}))
			} else {
				collectedSources = append(collectedSources, extractSources(fc.Name, output)...)
				responseParts = append(responseParts, genai.NewPartFromFunctionResponse(fc.Name, map[string]any{
					"result": output,
				}))
			}
		}

		contents = append(contents, &genai.Content{
			Role:  "user",
			Parts: responseParts,
		})
	}

	return "", nil, fmt.Errorf("exceeded maximum tool call rounds")
}

// postIDsFromArgs extracts the post_ids array from a cite_sources tool call.
// Handles numeric types and string-encoded integers (search results expose IDs as strings).
func postIDsFromArgs(args map[string]any) []int64 {
	raw, ok := args["post_ids"]
	if !ok || raw == nil {
		return nil
	}
	slice, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	ids := make([]int64, 0, len(slice))
	for _, v := range slice {
		switch n := v.(type) {
		case float64:
			ids = append(ids, int64(n))
		case int64:
			ids = append(ids, n)
		case int:
			ids = append(ids, int64(n))
		case string:
			if parsed, err := strconv.ParseInt(n, 10, 64); err == nil {
				ids = append(ids, parsed)
			}
		}
	}
	return ids
}

// filterSourcesByCitations returns only sources whose PostID is in cited.
// If cited is empty (model never called cite_sources), returns nil.
func filterSourcesByCitations(sources []ChatSource, cited map[int64]bool) []ChatSource {
	if len(cited) == 0 {
		return nil
	}
	out := make([]ChatSource, 0, len(cited))
	for _, s := range sources {
		if s.PostID != nil && cited[*s.PostID] {
			out = append(out, s)
		}
	}
	return out
}

// extractSources parses tool output JSON and collects post/topic ID pairs.
func extractSources(toolName, output string) []ChatSource {
	switch toolName {
	case "search_content":
		var buckets []struct {
			Results []struct {
				ID        string `json:"id"`
				TopicID   *int64 `json:"topic_id"`
				TopicName string `json:"topic_name"`
				TopicType *int   `json:"topic_type"`
			} `json:"results"`
		}
		if json.Unmarshal([]byte(output), &buckets) == nil {
			var sources []ChatSource
			for _, b := range buckets {
				for _, r := range b.Results {
					s := ChatSource{TopicID: r.TopicID, TopicName: r.TopicName, TopicType: r.TopicType}
					if id, err := strconv.ParseInt(r.ID, 10, 64); err == nil {
						s.PostID = &id
					}
					sources = append(sources, s)
				}
			}
			return sources
		}
	case "get_lore_topics":
		var topics []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		}
		if json.Unmarshal([]byte(output), &topics) == nil {
			var sources []ChatSource
			for _, t := range topics {
				id := t.ID
				topicType := 4 // LoreTopic
				sources = append(sources, ChatSource{TopicID: &id, TopicName: t.Name, TopicType: &topicType})
			}
			return sources
		}
	}
	return nil
}
