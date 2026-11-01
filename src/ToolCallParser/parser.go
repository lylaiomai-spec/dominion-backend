// Package toolcallparser normalises tool call output from LLMs that do not use
// the standard OpenAI tool_calls API field. Several model families embed tool
// calls directly in the response text using their own formats:
//
//   - XML (Llama / Hermes style):
//     <tool_call>NAME<arg_key>K</arg_key><arg_value>V</arg_value>…</tool_call>
//
//   - JSON-in-text (Mistral / Qwen style):
//     {"name":"NAME","arguments":{…}}
//
// Parse returns a unified []ToolCall slice regardless of which format was used.
// Values are JSON-decoded where possible; otherwise kept as strings.
package toolcallparser

import (
	"encoding/json"
	"regexp"
	"strings"
)

// ToolCall represents a single tool invocation extracted from model output.
type ToolCall struct {
	Name string
	Args map[string]any
}

var (
	reXMLBlock   = regexp.MustCompile(`(?s)<tool_call>(.*?)</tool_call>`)
	reArgKey     = regexp.MustCompile(`<arg_key>(.*?)</arg_key>`)
	reArgValue   = regexp.MustCompile(`(?s)<arg_value>(.*?)</arg_value>`)
	reJSONBlock  = regexp.MustCompile(`(?s)\{[^{}]*"name"\s*:\s*"[^"]+"\s*,\s*"arguments"\s*:\s*\{.*?\}\s*\}`)
)

// Parse extracts tool calls from raw model content, trying all known formats.
// Returns nil if no tool calls are found.
func Parse(content string) []ToolCall {
	if calls := parseXML(content); len(calls) > 0 {
		return calls
	}
	if calls := parseJSONInText(content); len(calls) > 0 {
		return calls
	}
	return nil
}

// parseXML handles the Llama/Hermes XML format:
// <tool_call>NAME<arg_key>K</arg_key><arg_value>V</arg_value>…</tool_call>
func parseXML(content string) []ToolCall {
	var calls []ToolCall
	for _, m := range reXMLBlock.FindAllStringSubmatch(content, -1) {
		body := m[1]
		name := body
		if idx := strings.Index(body, "<arg_key>"); idx >= 0 {
			name = strings.TrimSpace(body[:idx])
		}
		if name == "" {
			continue
		}
		keys := reArgKey.FindAllStringSubmatch(body, -1)
		vals := reArgValue.FindAllStringSubmatch(body, -1)
		args := make(map[string]any, len(keys))
		for i, k := range keys {
			if i >= len(vals) {
				break
			}
			args[k[1]] = decodeValue(vals[i][1])
		}
		calls = append(calls, ToolCall{Name: name, Args: args})
	}
	return calls
}

// parseJSONInText handles the Mistral/Qwen format embedded in response text:
// {"name":"tool_name","arguments":{…}}
func parseJSONInText(content string) []ToolCall {
	var calls []ToolCall
	for _, raw := range reJSONBlock.FindAllString(content, -1) {
		var obj struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal([]byte(raw), &obj); err != nil || obj.Name == "" {
			continue
		}
		calls = append(calls, ToolCall{Name: obj.Name, Args: obj.Arguments})
	}
	return calls
}

// Strip removes all tool call markup from raw model content, returning only
// the human-readable text. Call this when a model outputs tool calls past its
// budget and you want a clean answer rather than raw XML/JSON fragments.
func Strip(content string) string {
	s := reXMLBlock.ReplaceAllString(content, "")
	s = reJSONBlock.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

// decodeValue tries to JSON-decode a string value; falls back to the raw string.
func decodeValue(raw string) any {
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return raw
	}
	return v
}
