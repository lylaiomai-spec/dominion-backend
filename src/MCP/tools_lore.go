package MCP

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type loreTopicResult struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Status     int    `json:"status"`
	SubforumID int    `json:"subforum_id"`
}

func registerLoreTools(s *server.MCPServer, db *sql.DB) {
	registerTool(s,
		mcp.NewTool("get_lore_topics",
			mcp.WithDescription("Returns a list of all lore topics with their ID, name, status, and subforum ID."),
		),
		ToolDefinition{
			Name:        "get_lore_topics",
			Description: "Returns a list of all lore topics with their ID, name, status, and subforum ID.",
			Execute: func(ctx context.Context, args map[string]any) (string, error) {
				return executGetLoreTopics(ctx, db)
			},
		},
	)
}

func executGetLoreTopics(ctx context.Context, db *sql.DB) (string, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT t.id, t.name, t.status, t.subforum_id
		 FROM topics t
		 WHERE t.type = 4 AND t.status != 3
		 ORDER BY t.name ASC`,
	)
	if err != nil {
		return "", fmt.Errorf("failed to query lore topics: %w", err)
	}
	defer rows.Close()

	topics := make([]loreTopicResult, 0)
	for rows.Next() {
		var t loreTopicResult
		if err := rows.Scan(&t.ID, &t.Name, &t.Status, &t.SubforumID); err != nil {
			continue
		}
		topics = append(topics, t)
	}

	data, err := json.Marshal(topics)
	if err != nil {
		return "", fmt.Errorf("failed to serialize results")
	}
	return string(data), nil
}
