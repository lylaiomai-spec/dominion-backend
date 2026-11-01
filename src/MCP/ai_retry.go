package MCP

import (
	"cuento-backend/src/Entities"
	"strings"
)

// ModelRetryOptions configures withModelRetry behaviour.
type ModelRetryOptions struct {
	// MaxRetries is the number of retries after the first attempt (default 4).
	MaxRetries int
	// ShouldRetry returns true when the error warrants trying a different model.
	// Defaults to defaultShouldRetry (retry on 429 / quota).
	ShouldRetry func(error) bool
	// OnRetry is called before each retry with the failing model ID.
	OnRetry func(modelID string)
}

// withModelRetry calls fn with a client from pool, retrying with a fresh model when
// opts.ShouldRetry returns true. The first attempt uses initialClient; subsequent
// attempts call pool.ClientExcluding with the accumulated list of failed model IDs.
func withModelRetry(
	pool ModelPool,
	minSize Entities.AIModelSize,
	modelType Entities.AIModelType,
	initialClient AIClient,
	fn func(AIClient) error,
	onRetry func(modelID string),
	shouldRetry ...func(error) bool,
) error {
	retryable := defaultShouldRetry
	if len(shouldRetry) > 0 && shouldRetry[0] != nil {
		retryable = shouldRetry[0]
	}

	const maxRetries = 4
	excluded := make([]string, 0, maxRetries)
	client := initialClient

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			var err error
			client, err = pool.ClientExcluding(minSize, modelType, excluded)
			if err != nil {
				return err
			}
		}

		err := fn(client)
		if err == nil {
			return nil
		}

		if !retryable(err) || attempt == maxRetries {
			return err
		}

		excluded = append(excluded, client.ModelID())
		if onRetry != nil {
			onRetry(client.ModelID())
		}
	}

	return nil
}

// withModelRetryOpts is like withModelRetry but accepts a full ModelRetryOptions.
func withModelRetryOpts(
	pool ModelPool,
	minSize Entities.AIModelSize,
	modelType Entities.AIModelType,
	initialClient AIClient,
	fn func(AIClient) error,
	opts ModelRetryOptions,
) error {
	retryable := defaultShouldRetry
	if opts.ShouldRetry != nil {
		retryable = opts.ShouldRetry
	}
	maxRetries := 4
	if opts.MaxRetries > 0 {
		maxRetries = opts.MaxRetries
	}

	excluded := make([]string, 0, maxRetries)
	client := initialClient

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			var err error
			client, err = pool.ClientExcluding(minSize, modelType, excluded)
			if err != nil {
				return err
			}
		}

		err := fn(client)
		if err == nil {
			return nil
		}

		if !retryable(err) || attempt == maxRetries {
			return err
		}

		excluded = append(excluded, client.ModelID())
		if opts.OnRetry != nil {
			opts.OnRetry(client.ModelID())
		}
	}

	return nil
}

func defaultShouldRetry(err error) bool {
	return strings.Contains(err.Error(), "429") || strings.Contains(err.Error(), "quota")
}
