package MCP

import (
	"context"
	"cuento-backend/src/Entities"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const proofreadSystemInstruction = `You are a proofreader. Fix all spelling, grammar, and punctuation errors in the text the user provides.

What to fix:
- Spelling mistakes and typos
- Grammatical errors: wrong verb forms, tense inconsistencies, subject-verb disagreement, incorrect pronoun case
- Punctuation errors: missing or misplaced commas, periods, apostrophes, quotation marks

What NOT to change:
- Do NOT rephrase, reword, or restructure any sentence.
- Do NOT add, remove, or change any ideas, facts, or meaning.
- Do NOT change the tone, style, or voice of the text.
- Do NOT expand abbreviations or change formatting.
- If a sentence is stylistically unusual but grammatically correct, leave it as-is.

Return your response as a JSON object with exactly this structure:
{"text": "<corrected text>", "fixes": ["<short description of fix 1>", "<short description of fix 2>"]}
If no fixes were needed, return: {"text": "<original text unchanged>", "fixes": []}
The text may contain template placeholders like {0}, {1}, {2}, etc. These are technical markers, not text. Leave every placeholder exactly as-is — do not alter, remove, reorder, or reformat them in any way.
Return only the raw JSON object — no markdown fences, no commentary.`

// ProofreadResult holds the corrected text and a human-readable list of changes.
type ProofreadResult struct {
	Text  string   `json:"text"`
	Fixes []string `json:"fixes"`
}

var bbcodeTagRe = regexp.MustCompile(`\[[^\[\]\n]{0,60}\]`)

// maskBBCode replaces every BBCode tag with a numbered placeholder {0}, {1}, …
// so the model only sees plain prose. restoreBBCode reverses the substitution.
func maskBBCode(text string) (masked string, tags []string) {
	masked = bbcodeTagRe.ReplaceAllStringFunc(text, func(tag string) string {
		n := len(tags)
		tags = append(tags, tag)
		return fmt.Sprintf("{%d}", n)
	})
	return masked, tags
}

func restoreBBCode(text string, tags []string) string {
	for i, tag := range tags {
		text = strings.ReplaceAll(text, fmt.Sprintf("{%d}", i), tag)
	}
	return text
}

// ProofreadText asks a large model to fix spelling, grammar, and punctuation — no rephrasing.
func ProofreadText(text string, db *sql.DB) (*ProofreadResult, error) {
	pool := getActivePool()
	if pool == nil {
		return nil, fmt.Errorf("AI is not configured")
	}

	client, err := pool.ClientForMinSize(Entities.AIModelSizeMedium, Entities.AIModelTypeText)
	if err != nil {
		return nil, fmt.Errorf("no AI model available: %w", err)
	}

	// Replace BBCode tags with placeholders so the model only sees plain prose.
	masked, bbTags := maskBBCode(text)

	var result ProofreadResult
	err = withModelRetryOpts(
		pool,
		Entities.AIModelSizeMedium, Entities.AIModelTypeText,
		client,
		func(c AIClient) error {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			reply, _, e := c.Chat(ctx, []ChatMessage{
				{Role: "user", Content: masked},
			}, proofreadSystemInstruction)
			if e != nil {
				return e
			}

			reply = strings.TrimSpace(reply)
			if strings.HasPrefix(reply, "```") {
				reply = strings.TrimPrefix(reply, "```json")
				reply = strings.TrimPrefix(reply, "```")
				reply = strings.TrimSuffix(reply, "```")
				reply = strings.TrimSpace(reply)
			}

			if err := json.Unmarshal([]byte(reply), &result); err != nil {
				return fmt.Errorf("failed to parse proofreading response: %w", err)
			}
			return nil
		},
		ModelRetryOptions{
			MaxRetries: 2,
			ShouldRetry: func(err error) bool {
				msg := err.Error()
				return strings.Contains(msg, "429") ||
					strings.Contains(msg, "quota") ||
					strings.Contains(msg, "failed to parse proofreading response") ||
					strings.Contains(msg, "context deadline exceeded")
			},
		},
	)
	if err != nil {
		return nil, err
	}

	// Restore BBCode tags into the corrected text.
	result.Text = restoreBBCode(result.Text, bbTags)
	return &result, nil
}
