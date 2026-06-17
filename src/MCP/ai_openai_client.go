package MCP

import (
	"context"
	"encoding/json"
	"fmt"

	openai "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

// OpenAICompatClient works with any OpenAI-compatible API:
// openai, deepseek, groq, mistral, etc.
type OpenAICompatClient struct {
	client openai.Client
	model  string
}

func NewOpenAICompatClient(apiKey, model, baseURL string) (*OpenAICompatClient, error) {
	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	client := openai.NewClient(opts...)
	return &OpenAICompatClient{client: client, model: model}, nil
}

func (o *OpenAICompatClient) Chat(ctx context.Context, history []ChatMessage, systemInstruction string) (string, []ChatSource, error) {
	messages := make([]openai.ChatCompletionMessageParamUnion, 0, len(history)+1)
	if systemInstruction != "" {
		messages = append(messages, openai.SystemMessage(systemInstruction))
	}
	for _, msg := range history {
		switch msg.Role {
		case "user":
			messages = append(messages, openai.UserMessage(msg.Content))
		case "assistant":
			messages = append(messages, openai.AssistantMessage(msg.Content))
		}
	}

	tools := buildOpenAITools()
	var collectedSources []ChatSource

	const maxRounds = 10
	for round := 0; round < maxRounds; round++ {
		params := openai.ChatCompletionNewParams{
			Model:       openai.ChatModel(o.model),
			Messages:    messages,
			Temperature: openai.Float(0),
		}
		if len(tools) > 0 {
			params.Tools = tools
		}

		resp, err := o.client.Chat.Completions.New(ctx, params)
		if err != nil {
			return "", nil, err
		}
		if len(resp.Choices) == 0 {
			return "", nil, fmt.Errorf("empty response (no choices)")
		}

		msg := resp.Choices[0].Message
		if len(msg.ToolCalls) == 0 {
			return msg.Content, collectedSources, nil
		}

		// Add assistant turn to history using ToParam()
		messages = append(messages, msg.ToParam())

		// Execute tools and append results
		for _, tc := range msg.ToolCalls {
			var args map[string]any
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				args = map[string]any{}
			}

			executor, ok := findExecutor(tc.Function.Name)
			var resultStr string
			if !ok {
				resultStr = fmt.Sprintf(`{"error":"unknown tool: %s"}`, tc.Function.Name)
			} else {
				output, execErr := executor(ctx, args)
				if execErr != nil {
					resultStr = fmt.Sprintf(`{"error":%q}`, execErr.Error())
				} else {
					collectedSources = append(collectedSources, extractSources(tc.Function.Name, output)...)
					resultStr = output
				}
			}
			messages = append(messages, openai.ToolMessage(resultStr, tc.ID))
		}
	}

	return "", nil, fmt.Errorf("exceeded maximum tool call rounds")
}
