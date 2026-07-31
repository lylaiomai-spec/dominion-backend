package Services

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Concurrent access model:
//   Get  – map lookup + lastAccessed update under mutex; file read outside mutex.
//          If eviction deletes the file between the map lookup and the read,
//          os.ReadFile returns an error and we return (nil, false) — a harmless
//          false miss that falls through to a fresh fetch from origin.
//   Set  – file is written to a unique temp path outside the mutex (so concurrent
//          disk writes don't block each other), then an atomic os.Rename + map
//          update are done under the mutex.
//   evictOldest – map delete + os.Remove both under mutex; eviction is infrequent
//          so holding the lock for the Remove is acceptable.

const imageCacheDir = "cache/images"

type proxyCacheEntry struct {
	key          string
	filePath     string
	size         int64
	lastAccessed time.Time
}

type ImageProxyCacheService struct {
	mu        sync.Mutex
	entries   map[string]*proxyCacheEntry
	totalSize int64
	dir       string
}

var ImageProxyCache *ImageProxyCacheService

func InitImageProxyCache() error {
	if err := os.RemoveAll(imageCacheDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to clear image cache dir: %w", err)
	}
	if err := os.MkdirAll(imageCacheDir, 0755); err != nil {
		return fmt.Errorf("failed to create image cache dir: %w", err)
	}
	ImageProxyCache = &ImageProxyCacheService{
		entries: make(map[string]*proxyCacheEntry),
		dir:     imageCacheDir,
	}
	return nil
}

func ProxyCacheKey(url, size string) string {
	h := sha256.Sum256([]byte(url + "|" + size))
	return fmt.Sprintf("%x", h)
}

func (c *ImageProxyCacheService) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	entry, ok := c.entries[key]
	if ok {
		entry.lastAccessed = time.Now()
	}
	c.mu.Unlock()

	if !ok {
		return nil, false
	}
	data, err := os.ReadFile(entry.filePath)
	if err != nil {
		return nil, false
	}
	return data, true
}

func (c *ImageProxyCacheService) Set(key string, data []byte, maxSizeMB int64) {
	if maxSizeMB > 0 && int64(len(data)) > maxSizeMB*1024*1024 {
		return
	}

	filePath := filepath.Join(c.dir, key+".jpg")

	// Write to a unique temp file outside the lock so concurrent cache misses
	// can write in parallel without blocking each other or readers.
	tmp, err := os.CreateTemp(c.dir, "*.tmp")
	if err != nil {
		return
	}
	tmpPath := tmp.Name()
	_, writeErr := tmp.Write(data)
	closeErr := tmp.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(tmpPath)
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Atomic promotion: rename is a single syscall on the same filesystem.
	if err := os.Rename(tmpPath, filePath); err != nil {
		os.Remove(tmpPath)
		return
	}

	incoming := int64(len(data))
	maxBytes := maxSizeMB * 1024 * 1024
	if maxSizeMB > 0 {
		for len(c.entries) > 0 && c.totalSize+incoming > maxBytes {
			c.evictOldest()
		}
	}

	if existing, ok := c.entries[key]; ok {
		c.totalSize -= existing.size
	}

	c.entries[key] = &proxyCacheEntry{
		key:          key,
		filePath:     filePath,
		size:         incoming,
		lastAccessed: time.Now(),
	}
	c.totalSize += incoming
}

// must be called with lock held
func (c *ImageProxyCacheService) evictOldest() {
	var oldest *proxyCacheEntry
	for _, e := range c.entries {
		if oldest == nil || e.lastAccessed.Before(oldest.lastAccessed) {
			oldest = e
		}
	}
	if oldest == nil {
		return
	}
	os.Remove(oldest.filePath)
	c.totalSize -= oldest.size
	delete(c.entries, oldest.key)
}
