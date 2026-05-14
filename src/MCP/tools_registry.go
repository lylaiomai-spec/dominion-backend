package MCP

import (
	"context"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	openai "github.com/openai/openai-go"
	"github.com/openai/openai-go/shared"
	"google.golang.org/genai"
)

// --- Generic tool schema ---

type ToolPropType string

const (
	PropTypeString  ToolPropType = "string"
	PropTypeInteger ToolPropType = "integer"
	PropTypeArray   ToolPropType = "array"
)

type ToolProp struct {
	Type        ToolPropType
	Description string
	Items       *ToolProp // for arrays
}

type ToolSchema struct {
	Properties map[string]*ToolProp
	Required   []string
}

// ToolExecutor is a function that executes a tool call and returns a JSON string result.
type ToolExecutor func(ctx context.Context, args map[string]any) (string, error)

// ToolDefinition pairs a generic schema with its executor.
type ToolDefinition struct {
	Name        string
	Description string
	Schema      *ToolSchema // nil = no parameters
	Execute     ToolExecutor
}

// toolRegistry holds all registered tools.
var toolRegistry []ToolDefinition

// registerTool adds a tool to both the MCP server and the AI tool registry.
func registerTool(s interface {
	AddTool(mcp.Tool, server.ToolHandlerFunc)
}, tool mcp.Tool, def ToolDefinition) {
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := def.Execute(ctx, req.GetArguments())
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(result), nil
	})
	toolRegistry = append(toolRegistry, def)
}

// findExecutor looks up a tool executor by function name.
func findExecutor(name string) (ToolExecutor, bool) {
	for _, t := range toolRegistry {
		if t.Name == name {
			return t.Execute, true
		}
	}
	return nil, false
}

// --- Gemini tool builder ---

func buildGeminiTools() []*genai.Tool {
	if len(toolRegistry) == 0 {
		return nil
	}
	declarations := make([]*genai.FunctionDeclaration, len(toolRegistry))
	for i, t := range toolRegistry {
		declarations[i] = &genai.FunctionDeclaration{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  toGeminiSchema(t.Schema),
		}
	}
	return []*genai.Tool{{FunctionDeclarations: declarations}}
}

func toGeminiSchema(s *ToolSchema) *genai.Schema {
	if s == nil {
		return nil
	}
	props := make(map[string]*genai.Schema, len(s.Properties))
	for name, p := range s.Properties {
		props[name] = toGeminiProp(p)
	}
	return &genai.Schema{
		Type:       genai.TypeObject,
		Properties: props,
		Required:   s.Required,
	}
}

func toGeminiProp(p *ToolProp) *genai.Schema {
	s := &genai.Schema{Description: p.Description}
	switch p.Type {
	case PropTypeString:
		s.Type = genai.TypeString
	case PropTypeInteger:
		s.Type = genai.TypeInteger
	case PropTypeArray:
		s.Type = genai.TypeArray
		if p.Items != nil {
			s.Items = toGeminiProp(p.Items)
		}
	}
	return s
}

// --- OpenAI tool builder (shared by openai, deepseek, groq, mistral) ---

func buildOpenAITools() []openai.ChatCompletionToolParam {
	if len(toolRegistry) == 0 {
		return nil
	}
	tools := make([]openai.ChatCompletionToolParam, len(toolRegistry))
	for i, t := range toolRegistry {
		tools[i] = openai.ChatCompletionToolParam{
			Function: shared.FunctionDefinitionParam{
				Name:        t.Name,
				Description: openai.String(t.Description),
				Parameters:  toOpenAISchema(t.Schema),
			},
		}
	}
	return tools
}

func toOpenAISchema(s *ToolSchema) shared.FunctionParameters {
	if s == nil {
		return shared.FunctionParameters{"type": "object", "properties": map[string]any{}}
	}
	props := make(map[string]any, len(s.Properties))
	for name, p := range s.Properties {
		props[name] = toOpenAIProp(p)
	}
	schema := shared.FunctionParameters{
		"type":       "object",
		"properties": props,
	}
	if len(s.Required) > 0 {
		schema["required"] = s.Required
	}
	return schema
}

func toOpenAIProp(p *ToolProp) map[string]any {
	m := map[string]any{"description": p.Description}
	switch p.Type {
	case PropTypeString:
		m["type"] = "string"
	case PropTypeInteger:
		m["type"] = "integer"
	case PropTypeArray:
		m["type"] = "array"
		if p.Items != nil {
			m["items"] = toOpenAIProp(p.Items)
		}
	}
	return m
}

// --- Claude tool builder ---

func buildClaudeTools() []anthropic.ToolParam {
	if len(toolRegistry) == 0 {
		return nil
	}
	tools := make([]anthropic.ToolParam, len(toolRegistry))
	for i, t := range toolRegistry {
		tools[i] = anthropic.ToolParam{
			Name:        t.Name,
			Description: anthropic.String(t.Description),
			InputSchema: toClaudeSchema(t.Schema),
		}
	}
	return tools
}

func toClaudeSchema(s *ToolSchema) anthropic.ToolInputSchemaParam {
	if s == nil {
		return anthropic.ToolInputSchemaParam{Properties: map[string]any{}}
	}
	props := make(map[string]any, len(s.Properties))
	for name, p := range s.Properties {
		props[name] = toOpenAIProp(p) // same JSON Schema format as OpenAI
	}
	return anthropic.ToolInputSchemaParam{
		Properties: props,
		Required:   s.Required,
	}
}

// --- Schema helpers ---

func schemaObject(required []string, props map[string]*ToolProp) *ToolSchema {
	return &ToolSchema{Properties: props, Required: required}
}

func schemaString(description string) *ToolProp {
	return &ToolProp{Type: PropTypeString, Description: description}
}

func schemaInteger(description string) *ToolProp {
	return &ToolProp{Type: PropTypeInteger, Description: description}
}

func schemaStringArray(description string) *ToolProp {
	return &ToolProp{
		Type:        PropTypeArray,
		Description: description,
		Items:       &ToolProp{Type: PropTypeString},
	}
}
