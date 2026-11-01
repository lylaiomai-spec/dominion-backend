package MCP

import (
	"context"
	"cuento-backend/src/Services"
	"database/sql"
	"fmt"
	"sync"

	"github.com/mark3labs/mcp-go/server"
	"google.golang.org/genai"
)

var (
	activePoolMu sync.RWMutex
	activePool   ModelPool
)

func getActivePool() ModelPool {
	activePoolMu.RLock()
	defer activePoolMu.RUnlock()
	return activePool
}

// SwitchPool replaces the active model pool without restarting the server.
func SwitchPool(pool ModelPool) {
	activePoolMu.Lock()
	defer activePoolMu.Unlock()
	activePool = pool
	fmt.Printf("MCP: switched to pool %T\n", pool)
}

// InitPoolFromSettings reads model_pool_source and related settings from the DB
// and activates the appropriate pool. Called on startup and after settings change.
func InitPoolFromSettings(db *sql.DB) {
	source, _ := Services.GetGlobalSetting("model_pool_source", db)
	switch source {
	case "openrouter":
		apiKey, _ := Services.GetGlobalSetting("openrouter_api_key", db)
		freeOnly, _ := Services.GetGlobalSetting("openrouter_use_free_only", db)
		SwitchPool(NewOpenRouterModelPool(apiKey, freeOnly != "n"))
	case "groq":
		apiKey, _ := Services.GetGlobalSetting("groq_api_key", db)
		SwitchPool(NewGroqModelPool(apiKey))
	default:
		SwitchPool(NewAIModelPool(db))
	}
}

func ReinitializeAgent(db *sql.DB) {
	InitPoolFromSettings(db)
}

func StartMCPServer(db *sql.DB, addr string) error {
	InitPoolFromSettings(db)

	s := server.NewMCPServer(
		"cuento",
		"1.0.0",
		server.WithToolCapabilities(true),
	)

	registerLoreTools(s, db)
	registerSearchTools(s, db)
	registerCitationTools(s)

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

