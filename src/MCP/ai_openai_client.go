package MCP

import (
	"context"
	"cuento-backend/src/ToolCallParser"
	"encoding/json"
	"fmt"
	"log"
	"strings"

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

func (o *OpenAICompatClient) ModelID() string { return o.model }

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
	citedPostIDs := make(map[int64]bool)

	const maxRounds = 20
	const toolBudget = 15 // after this many tool-use rounds, force a text response
	toolRounds := 0
	for round := 0; round < maxRounds; round++ {
		params := openai.ChatCompletionNewParams{
			Model:       openai.ChatModel(o.model),
			Messages:    messages,
			Temperature: openai.Float(0),
		}
		if len(tools) > 0 && toolRounds < toolBudget {
			params.Tools = tools
		} else if toolRounds == toolBudget {
			messages = append(messages, openai.UserMessage("You have reached the search limit. Do not call any more tools. Write your final answer now based only on the results you have already gathered."))
		}

		resp, err := o.client.Chat.Completions.New(ctx, params)
		if err != nil {
			return "", nil, err
		}
		if len(resp.Choices) == 0 {
			return "", nil, fmt.Errorf("empty response (no choices)")
		}

		msg := resp.Choices[0].Message

		// Some models output tool calls in the content instead of tool_calls.
		if len(msg.ToolCalls) == 0 {
			altCalls := toolcallparser.Parse(msg.Content)
			if len(altCalls) == 0 {
				return msg.Content, filterSourcesByCitations(collectedSources, citedPostIDs), nil
			}
			if toolRounds >= toolBudget {
				// Tool calls found but budget exhausted — strip markup and return text.
				text := toolcallparser.Strip(msg.Content)
				log.Printf("AI chat: tool budget exhausted (rounds=%d), raw content len=%d, stripped len=%d", toolRounds, len(msg.Content), len(text))
				if text == "" {
					return "", nil, fmt.Errorf("model produced no text response after %d tool rounds", toolRounds)
				}
				return text, filterSourcesByCitations(collectedSources, citedPostIDs), nil
			}
			toolRounds++
			messages = append(messages, openai.AssistantMessage(msg.Content))
			var resultParts []string
			for _, xc := range altCalls {
				if xc.Name == "cite_sources" {
					for _, id := range postIDsFromArgs(xc.Args) {
						citedPostIDs[id] = true
					}
				}
				executor, ok := findExecutor(xc.Name)
				var resultStr string
				if !ok {
					resultStr = fmt.Sprintf(`{"error":"unknown tool: %s"}`, xc.Name)
				} else {
					output, execErr := executor(ctx, xc.Args)
					if execErr != nil {
						resultStr = fmt.Sprintf(`{"error":%q}`, execErr.Error())
					} else {
						collectedSources = append(collectedSources, extractSources(xc.Name, output)...)
						resultStr = output
					}
				}
				resultParts = append(resultParts, fmt.Sprintf("<tool_response>%s</tool_response>", resultStr))
			}
			messages = append(messages, openai.UserMessage(strings.Join(resultParts, "\n")))
			continue
		}

		toolRounds++
		// Add assistant turn to history using ToParam()
		messages = append(messages, msg.ToParam())

		// Execute tools and append results
		for _, tc := range msg.ToolCalls {
			var args map[string]any
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				args = map[string]any{}
			}

			if tc.Function.Name == "cite_sources" {
				for _, id := range postIDsFromArgs(args) {
					citedPostIDs[id] = true
				}
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
