package Services

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const GuestActivityWindow = 10 * time.Minute

type GuestActivityStorage struct {
	mu     sync.Mutex
	guests map[string]time.Time // fingerprint hash -> last seen
}

var GuestActivity = &GuestActivityStorage{
	guests: make(map[string]time.Time),
}

func GuestFingerprint(r *http.Request) string {
	raw := fmt.Sprintf("%s|%s|%s|%s|%s|%s",
		r.Header.Get("User-Agent"),
		r.Header.Get("Accept-Language"),
		r.Header.Get("Accept"),
		r.Header.Get("Accept-Encoding"),
		r.Header.Get("X-Screen-Resolution"),
		r.Header.Get("Sec-CH-UA"),
	)
	h := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", h)
}

func (s *GuestActivityStorage) Track(fingerprint string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.guests[fingerprint] = time.Now()
}

func (s *GuestActivityStorage) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-GuestActivityWindow)
	count := 0
	for _, t := range s.guests {
		if t.After(cutoff) {
			count++
		}
	}
	return count
}

func (s *GuestActivityStorage) Cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-GuestActivityWindow)
	for k, t := range s.guests {
		if t.Before(cutoff) {
			delete(s.guests, k)
		}
	}
}
