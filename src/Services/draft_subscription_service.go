package Services

import (
	"sync"
	"time"
)

const draftSubscriptionTimeout = 1 * time.Minute

var draftSubscriptions = struct {
	mu   sync.Mutex
	data map[string]map[any]time.Time // sessionKey -> client -> lastSeen
}{
	data: make(map[string]map[any]time.Time),
}

func SubscribeDraft(sessionKey string, client any) {
	draftSubscriptions.mu.Lock()
	defer draftSubscriptions.mu.Unlock()
	if draftSubscriptions.data[sessionKey] == nil {
		draftSubscriptions.data[sessionKey] = make(map[any]time.Time)
	}
	draftSubscriptions.data[sessionKey][client] = time.Now()
}

func UnsubscribeClientFromAllDrafts(client any) {
	draftSubscriptions.mu.Lock()
	defer draftSubscriptions.mu.Unlock()
	for sessionKey, clients := range draftSubscriptions.data {
		delete(clients, client)
		if len(clients) == 0 {
			delete(draftSubscriptions.data, sessionKey)
		}
	}
}

func GetDraftSubscribers(sessionKey string) []any {
	draftSubscriptions.mu.Lock()
	defer draftSubscriptions.mu.Unlock()
	clients := draftSubscriptions.data[sessionKey]
	result := make([]any, 0, len(clients))
	for client := range clients {
		result = append(result, client)
	}
	return result
}

func StartDraftSubscriptionCleaner() {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			evictStaleDraftSubscriptions()
		}
	}()
}

func evictStaleDraftSubscriptions() {
	cutoff := time.Now().Add(-draftSubscriptionTimeout)
	draftSubscriptions.mu.Lock()
	defer draftSubscriptions.mu.Unlock()
	for sessionKey, clients := range draftSubscriptions.data {
		for client, lastSeen := range clients {
			if lastSeen.Before(cutoff) {
				delete(clients, client)
			}
		}
		if len(clients) == 0 {
			delete(draftSubscriptions.data, sessionKey)
		}
	}
}
