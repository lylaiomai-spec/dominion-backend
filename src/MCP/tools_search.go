package MCP

import (
	"context"
	"cuento-backend/src/Services"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type bucketSearchResult struct {
	Bucket  string                      `json:"bucket"`
	Results []Services.SearchResultItem `json:"results"`
}

func registerSearchTools(s *server.MCPServer, db *sql.DB) {
	bucketsDesc := "Available buckets: " + strings.Join(Services.AllSonicBuckets, ", ")

	registerTool(s,
		mcp.NewTool("get_search_buckets",
			mcp.WithDescription("Returns the list of available search buckets that can be used with search_content."),
		),
		ToolDefinition{
			Name:        "get_search_buckets",
			Description: "Returns the list of available search buckets that can be used with search_content.",
			Execute: func(ctx context.Context, args map[string]any) (string, error) {
				data, _ := json.Marshal(Services.AllSonicBuckets)
				return string(data), nil
			},
		},
	)

	registerTool(s,
		mcp.NewTool("search_content",
			mcp.WithDescription("Search forum content using semantic (vector) search when available, falling back to keyword search. "+
				"Results are filtered by the user's subforum visibility permissions. "+bucketsDesc),
			mcp.WithNumber("user_id",
				mcp.Required(),
				mcp.Description("The ID of the current user. Used to apply subforum visibility permissions."),
			),
			mcp.WithString("query",
				mcp.Required(),
				mcp.Description("The keyword or phrase to search for."),
			),
			mcp.WithArray("buckets",
				mcp.WithStringItems(),
				mcp.Description("Which buckets to search. Omit to search all buckets. "+bucketsDesc),
			),
			mcp.WithNumber("subforum_id",
				mcp.Description("Optional. Restrict results to a specific subforum ID."),
			),
			mcp.WithNumber("topic_type",
				mcp.Description("Optional. Restrict results by topic type: 0=general, 1=episode, 2=character_sheet, 3=wanted_character, 4=lore."),
			),
			mcp.WithNumber("limit",
				mcp.Description("Max results per bucket. Default 10, max 100."),
			),
		),
		ToolDefinition{
			Name: "search_content",
			Description: "Search forum content using semantic (vector) search when available, falling back to keyword search. " +
				"Results are filtered by the user's subforum visibility permissions. " + bucketsDesc,
			Schema: schemaObject(
				[]string{"user_id", "query"},
				map[string]*ToolProp{
					"user_id":     schemaInteger("The ID of the current user. Used to apply subforum visibility permissions."),
					"query":       schemaString("The keyword or phrase to search for."),
					"buckets":     schemaStringArray("Which buckets to search. Omit to search all. " + bucketsDesc),
					"subforum_id": schemaInteger("Optional. Restrict results to a specific subforum ID."),
					"topic_type":  schemaInteger("Optional. Restrict by topic type: 0=general, 1=episode, 2=character_sheet, 3=wanted_character, 4=lore."),
					"limit":       schemaInteger("Max results per bucket. Default 10, max 100."),
				},
			),
			Execute: func(ctx context.Context, args map[string]any) (string, error) {
				return executeSearchContent(ctx, args, db)
			},
		},
	)
}

func executeSearchContent(ctx context.Context, args map[string]any, db *sql.DB) (string, error) {
	query, _ := args["query"].(string)
	if strings.TrimSpace(query) == "" {
		return "", fmt.Errorf("query must not be empty")
	}

	userID := intArg(args, "user_id")

	buckets := Services.AllSonicBuckets
	if raw, ok := args["buckets"]; ok && raw != nil {
		if rawSlice, ok := raw.([]interface{}); ok && len(rawSlice) > 0 {
			buckets = make([]string, 0, len(rawSlice))
			for _, b := range rawSlice {
				if bs, ok := b.(string); ok {
					buckets = append(buckets, bs)
				}
			}
		}
	}

	filterSubforum := intArg(args, "subforum_id")
	filterTopicType := intArg(args, "topic_type")

	limit := intArgDefault(args, "limit", 10)
	if limit <= 0 || limit > 100 {
		limit = 10
	}

	visibleIDs, _ := Services.GetVisibleSubforums(userID, "subforum_read", db)
	visibleSet := make(map[int]bool, len(visibleIDs))
	for _, id := range visibleIDs {
		visibleSet[id] = true
	}

	var output []bucketSearchResult
	for _, bucket := range buckets {
		var items []Services.SearchResultItem
		var err error
		if Services.QdrantAvailable() {
			items, err = Services.QdrantSearchInBucket(bucket, query, limit, filterSubforum, filterTopicType, visibleSet, db)
		} else {
			items, err = Services.SearchInBucket(bucket, query, limit, filterSubforum, filterTopicType, visibleSet, db)
		}
		if err != nil || len(items) == 0 {
			continue
		}
		output = append(output, bucketSearchResult{Bucket: bucket, Results: items})
	}

	if output == nil {
		output = []bucketSearchResult{}
	}

	data, err := json.Marshal(output)
	if err != nil {
		return "", fmt.Errorf("failed to serialize results")
	}
	return string(data), nil
}

func intArg(args map[string]any, key string) int {
	if v, ok := args[key]; ok && v != nil {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		case int64:
			return int(n)
		}
	}
	return 0
}

func intArgDefault(args map[string]any, key string, def int) int {
	if v := intArg(args, key); v != 0 {
		return v
	}
	return def
}
