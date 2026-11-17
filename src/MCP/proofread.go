package MCP

import (
	"context"
	"cuento-backend/src/Entities"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
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

const proofreadStreamSystemInstruction = `You are a proofreader. Fix all spelling, grammar, and punctuation errors in the text the user provides.

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

The text may contain template placeholders like {0}, {1}, {2}, etc. These are technical markers, not text. Leave every placeholder exactly as-is.

Write the corrected text first. After the corrected text, write exactly on its own line:
===FIXES===
Then write each fix as a separate line. If no fixes were needed, write nothing after ===FIXES===.
Do not include any other commentary, headers, or formatting.`

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

// maxProofreadChars is the character limit for a single proofreading request.
// Groq's free tier allows 7000 input tokens per minute; the system prompt uses ~400,
// leaving ~6600 tokens (~26000 chars) for user text. 20000 chars is a safe margin.
const maxProofreadChars = 20000

// proofreadShouldRetry returns true for errors that mean "try a different model":
// 400 invalid request, 429 rate limit, quota exhaustion, or unparseable JSON response.
func proofreadShouldRetry(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "400") ||
		strings.Contains(msg, "429") ||
		strings.Contains(msg, "quota") ||
		strings.Contains(msg, "failed to parse proofreading response")
}

// ProofreadText asks a large model to fix spelling, grammar, and punctuation — no rephrasing.
func ProofreadText(text string, db *sql.DB) (*ProofreadResult, error) {
	if len([]rune(text)) > maxProofreadChars {
		return nil, fmt.Errorf("text is too long for proofreading (max %d characters)", maxProofreadChars)
	}

	pool := getActivePool()
	if pool == nil {
		return nil, fmt.Errorf("AI is not configured")
	}

	client, err := pool.ClientForSimpleChat(Entities.AIModelSizeSmall, Entities.AIModelTypeText)
	if err != nil {
		return nil, fmt.Errorf("no AI model available: %w", err)
	}

	// Replace BBCode tags with placeholders so the model only sees plain prose.
	masked, bbTags := maskBBCode(text)

	var result ProofreadResult
	err = withModelRetryOpts(
		pool,
		Entities.AIModelSizeSmall, Entities.AIModelTypeText,
		client,
		func(c AIClient) error {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()

			reply, e := c.SimpleChat(ctx, []ChatMessage{
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
				log.Printf("Proofread: parse error — raw reply: %.500s", reply)
				return fmt.Errorf("failed to parse proofreading response: %w", err)
			}
			return nil
		},
		ModelRetryOptions{
			MaxRetries:  4,
			ShouldRetry: proofreadShouldRetry,
			SimpleChat:  true,
		},
	)
	if err != nil {
		return nil, err
	}

	result.Text = restoreBBCode(result.Text, bbTags)
	return &result, nil
}

// ProofreadTextStream streams corrected text tokens via onToken, then calls onDone
// with the BBCode-restored text and list of fixes once the stream completes.
func ProofreadTextStream(
	text string,
	db *sql.DB,
	onToken func(string) error,
	onDone func(restoredText string, fixes []string) error,
) error {
	if len([]rune(text)) > maxProofreadChars {
		return fmt.Errorf("text is too long for proofreading (max %d characters)", maxProofreadChars)
	}

	pool := getActivePool()
	if pool == nil {
		return fmt.Errorf("AI is not configured")
	}

	masked, bbTags := maskBBCode(text)

	const delimiter = "===FIXES==="
	const delimLen = len(delimiter)

	const maxAttempts = 5
	excluded := make([]string, 0, maxAttempts)

	for attempt := 0; attempt < maxAttempts; attempt++ {
		var client AIClient
		var err error
		if attempt == 0 {
			client, err = pool.ClientForSimpleChat(Entities.AIModelSizeSmall, Entities.AIModelTypeText)
		} else {
			client, err = pool.ClientExcludingForSimpleChat(Entities.AIModelSizeSmall, Entities.AIModelTypeText, excluded)
		}
		if err != nil {
			return fmt.Errorf("no AI model available: %w", err)
		}

		sc, ok := client.(StreamingClient)
		if !ok {
			excluded = append(excluded, client.ModelID())
			continue
		}

		var fullBuf strings.Builder
		var emittedLen int
		delimFound := false
		delimPos := -1

		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)

		streamErr := sc.ChatSimpleStream(ctx, []ChatMessage{
			{Role: "user", Content: masked},
		}, proofreadStreamSystemInstruction, func(token string) error {
			if delimFound {
				fullBuf.WriteString(token)
				return nil
			}
			fullBuf.WriteString(token)
			full := fullBuf.String()

			if idx := strings.Index(full, delimiter); idx >= 0 {
				delimFound = true
				delimPos = idx
				if idx > emittedLen {
					if err := onToken(full[emittedLen:idx]); err != nil {
						return err
					}
				}
				emittedLen = idx
				return nil
			}

			safeEnd := len(full) - (delimLen - 1)
			if safeEnd > emittedLen {
				if err := onToken(full[emittedLen:safeEnd]); err != nil {
					return err
				}
				emittedLen = safeEnd
			}
			return nil
		})
		cancel()

		if streamErr != nil {
			// Only retry if no tokens were emitted yet (safe to start over).
			if emittedLen == 0 && proofreadShouldRetry(streamErr) {
				excluded = append(excluded, client.ModelID())
				continue
			}
			return streamErr
		}

		full := fullBuf.String()
		var correctedText string
		var fixes []string

		if !delimFound {
			if len(full) > emittedLen {
				if err := onToken(full[emittedLen:]); err != nil {
					return err
				}
			}
			correctedText = strings.TrimSpace(full)
		} else {
			correctedText = strings.TrimSpace(full[:delimPos])
			fixesRaw := strings.TrimSpace(full[delimPos+delimLen:])
			for _, line := range strings.Split(fixesRaw, "\n") {
				if line = strings.TrimSpace(line); line != "" {
					fixes = append(fixes, line)
				}
			}
		}

		correctedText = restoreBBCode(correctedText, bbTags)
		if fixes == nil {
			fixes = []string{}
		}
		return onDone(correctedText, fixes)
	}

	return fmt.Errorf("all available models failed for proofreading")
}
