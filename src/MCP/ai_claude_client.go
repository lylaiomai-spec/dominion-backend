package MCP

import (
	"context"
	"encoding/json"
	"fmt"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

type ClaudeClient struct {
	client anthropic.Client
	model  string
}

func NewClaudeClient(apiKey, model string) (*ClaudeClient, error) {
	client := anthropic.NewClient(option.WithAPIKey(apiKey))
	return &ClaudeClient{client: client, model: model}, nil
}

func (c *ClaudeClient) Chat(ctx context.Context, history []ChatMessage, systemInstruction string) (string, []ChatSource, error) {
	messages := make([]anthropic.MessageParam, 0, len(history))
	for _, msg := range history {
		switch msg.Role {
		case "user":
			messages = append(messages, anthropic.NewUserMessage(anthropic.NewTextBlock(msg.Content)))
		case "assistant":
			messages = append(messages, anthropic.NewAssistantMessage(anthropic.NewTextBlock(msg.Content)))
		}
	}

	claudeTools := buildClaudeTools()
	toolUnions := make([]anthropic.ToolUnionParam, len(claudeTools))
	for i, t := range claudeTools {
		t := t
		toolUnions[i] = anthropic.ToolUnionParam{OfTool: &t}
	}

	var collectedSources []ChatSource

	const maxRounds = 10
	for round := 0; round < maxRounds; round++ {
		params := anthropic.MessageNewParams{
			Model:       anthropic.Model(c.model),
			Messages:    messages,
			MaxTokens:   4096,
			Tools:       toolUnions,
			Temperature: anthropic.Float(0),
		}
		if systemInstruction != "" {
			params.System = []anthropic.TextBlockParam{{Text: systemInstruction}}
		}

		resp, err := c.client.Messages.New(ctx, params)
		if err != nil {
			return "", nil, err
		}

		var textContent string
		var toolUseBlocks []anthropic.ContentBlockUnion

		for _, block := range resp.Content {
			switch block.Type {
			case "text":
				textContent = block.Text
			case "tool_use":
				toolUseBlocks = append(toolUseBlocks, block)
			}
		}

		if len(toolUseBlocks) == 0 {
			return textContent, collectedSources, nil
		}

		// Add assistant turn to history
		messages = append(messages, resp.ToParam())

		// Execute tools and collect results
		toolResults := make([]anthropic.ContentBlockParamUnion, 0, len(toolUseBlocks))
		for _, block := range toolUseBlocks {
			var args map[string]any
			if err := json.Unmarshal(block.Input, &args); err != nil {
				args = map[string]any{}
			}

			executor, ok := findExecutor(block.Name)
			var resultStr string
			var isError bool
			if !ok {
				resultStr = fmt.Sprintf(`{"error":"unknown tool: %s"}`, block.Name)
				isError = true
			} else {
				output, execErr := executor(ctx, args)
				if execErr != nil {
					resultStr = execErr.Error()
					isError = true
				} else {
					collectedSources = append(collectedSources, extractSources(block.Name, output)...)
					resultStr = output
				}
			}
			toolResults = append(toolResults, anthropic.NewToolResultBlock(block.ID, resultStr, isError))
		}
		messages = append(messages, anthropic.NewUserMessage(toolResults...))
	}

	return "", nil, fmt.Errorf("exceeded maximum tool call rounds")
}
