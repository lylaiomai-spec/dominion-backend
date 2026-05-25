package MCP

import (
	"context"
	"cuento-backend/src/Services"
	"database/sql"
	"fmt"

	"github.com/mark3labs/mcp-go/server"
	"google.golang.org/genai"
)

var activeAgent *AIAgent

func ReinitializeAgent(db *sql.DB) {
	agent, err := buildAgent(db)
	if err != nil {
		fmt.Printf("MCP: AI agent reinitialization failed: %v\n", err)
		activeAgent = nil
		return
	}
	activeAgent = agent
	fmt.Println("MCP: AI agent reinitialized successfully")
}

func StartMCPServer(db *sql.DB, addr string) error {
	agent, err := buildAgent(db)
	if err != nil {
		fmt.Printf("MCP: AI agent not initialized: %v\n", err)
	}
	activeAgent = agent

	s := server.NewMCPServer(
		"cuento",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	registerLoreTools(s, db)
	registerSearchTools(s, db)

	StartQueueWorker(db)

	httpServer := server.NewStreamableHTTPServer(s)
	fmt.Printf("MCP server listening on %s/mcp\n", addr)
	return httpServer.Start(addr)
}

func ListAvailableModels(db *sql.DB) ([]string, error) {
	apiKey, err := Services.GetGlobalSetting("ai_api_key", db)
	if err != nil || apiKey == "" {
		return []string{}, nil
	}

	aiName, err := Services.GetGlobalSetting("ai_name", db)
	if err != nil || aiName == "" {
		return []string{}, nil
	}

	switch aiName {
	case "gemini":
		client, err := genai.NewClient(context.Background(), &genai.ClientConfig{APIKey: apiKey})
		if err != nil {
			return nil, fmt.Errorf("failed to create Gemini client: %w", err)
		}
		page, err := client.Models.List(context.Background(), nil)
		if err != nil {
			return nil, fmt.Errorf("failed to list Gemini models: %w", err)
		}
		names := make([]string, 0, len(page.Items))
		for _, m := range page.Items {
			names = append(names, m.Name)
		}
		return names, nil
	default:
		return []string{}, nil
	}
}

// openAIBaseURLs re-exports the provider→base URL map from Services.
var openAIBaseURLs = Services.OpenAIBaseURLs

// defaultModels maps provider names to sensible default model names.
var defaultModels = map[string]string{
	"gemini":   "gemini-2.0-flash",
	"claude":   "claude-3-5-sonnet-20241022",
	"openai":   "gpt-4o",
	"deepseek": "deepseek-chat",
	"groq":     "llama-3.3-70b-versatile",
	"mistral":  "mistral-large-latest",
}

func buildAgent(db *sql.DB) (*AIAgent, error) {
	apiKey, err := Services.GetGlobalSetting("ai_api_key", db)
	if err != nil || apiKey == "" {
		return nil, fmt.Errorf("ai_api_key not set")
	}

	aiName, err := Services.GetGlobalSetting("ai_name", db)
	if err != nil || aiName == "" {
		return nil, fmt.Errorf("ai_name not set")
	}

	aiModel, err := Services.GetGlobalSetting("ai_model", db)
	if err != nil || aiModel == "" {
		if def, ok := defaultModels[aiName]; ok {
			aiModel = def
		} else {
			aiModel = "gemini-2.0-flash"
		}
	}

	switch aiName {
	case "gemini":
		client, err := NewGeminiClient(apiKey, aiModel)
		if err != nil {
			return nil, fmt.Errorf("failed to create Gemini client: %w", err)
		}
		return NewAIAgent(client), nil

	case "claude":
		client, err := NewClaudeClient(apiKey, aiModel)
		if err != nil {
			return nil, fmt.Errorf("failed to create Claude client: %w", err)
		}
		return NewAIAgent(client), nil

	case "openai", "deepseek", "groq", "mistral":
		return NewAIAgent(&OpenAICompatClient{client: Services.OpenAIClient, model: aiModel}), nil

	default:
		return nil, fmt.Errorf("unknown ai_name: %s (supported: gemini, claude, openai, deepseek, groq, mistral)", aiName)
	}
}
