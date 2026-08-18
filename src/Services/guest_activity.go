package Services

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const GuestActivityWindow = 10 * time.Minute

type GuestEntry struct {
	ShortID         string
	LastActive      time.Time
	CurrentPageType string
	CurrentPageId   string
}

type guestRecord struct {
	lastActive      time.Time
	currentPageType string
	currentPageId   string
}

type GuestActivityStorage struct {
	mu     sync.Mutex
	guests map[string]*guestRecord // fingerprint hash -> record
}

var GuestActivity = &GuestActivityStorage{
	guests: make(map[string]*guestRecord),
}

func GuestFingerprint(r *http.Request) string {
	raw := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s",
		r.Header.Get("User-Agent"),
		r.Header.Get("Accept-Language"),
		r.Header.Get("Accept"),
		r.Header.Get("Accept-Encoding"),
		r.Header.Get("X-Screen-Resolution"),
		r.Header.Get("X-Color-Depth"),
		r.Header.Get("Sec-CH-UA"),
	)
	h := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", h)
}

func (s *GuestActivityStorage) Track(fingerprint string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, exists := s.guests[fingerprint]; exists {
		r.lastActive = time.Now()
	} else {
		s.guests[fingerprint] = &guestRecord{lastActive: time.Now()}
	}
}

func (s *GuestActivityStorage) UpdateLocation(fingerprint, pageType, pageId string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, exists := s.guests[fingerprint]; exists {
		r.lastActive = time.Now()
		r.currentPageType = pageType
		r.currentPageId = pageId
	} else {
		s.guests[fingerprint] = &guestRecord{
			lastActive:      time.Now(),
			currentPageType: pageType,
			currentPageId:   pageId,
		}
	}
}

func (s *GuestActivityStorage) GetActiveGuests() []GuestEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-GuestActivityWindow)
	entries := make([]GuestEntry, 0)
	for fp, r := range s.guests {
		if r.lastActive.After(cutoff) {
			entries = append(entries, GuestEntry{
				ShortID:         fp[len(fp)-6:],
				LastActive:      r.lastActive,
				CurrentPageType: r.currentPageType,
				CurrentPageId:   r.currentPageId,
			})
		}
	}
	return entries
}

func (s *GuestActivityStorage) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-GuestActivityWindow)
	count := 0
	for _, r := range s.guests {
		if r.lastActive.After(cutoff) {
			count++
		}
	}
	return count
}

func (s *GuestActivityStorage) Cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-GuestActivityWindow)
	for k, r := range s.guests {
		if r.lastActive.Before(cutoff) {
			delete(s.guests, k)
		}
	}
}
