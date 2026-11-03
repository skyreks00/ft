package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Cache stores raw response bodies by key.
type Cache interface {
	Get(key string) ([]byte, bool)
	Set(key string, body []byte, ttl time.Duration) error
	Delete(key string) error
}

// FileCache is an on-disk cache, one file per key, so that consecutive CLI
// invocations share responses.
type FileCache struct {
	Dir string
	now func() time.Time
}

type cacheEntry struct {
	Expires time.Time       `json:"expires"`
	Body    json.RawMessage `json:"body"`
}

// NewFileCache returns a cache stored in dir.
func NewFileCache(dir string) *FileCache {
	return &FileCache{Dir: dir, now: time.Now}
}

func (c *FileCache) path(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(c.Dir, hex.EncodeToString(sum[:])+".json")
}

// Get returns the cached body if present and not expired.
func (c *FileCache) Get(key string) ([]byte, bool) {
	b, err := os.ReadFile(c.path(key))
	if err != nil {
		return nil, false
	}
	var e cacheEntry
	if json.Unmarshal(b, &e) != nil || c.now().After(e.Expires) {
		return nil, false
	}
	return e.Body, true
}

// Set stores body for ttl. Bodies that are not valid JSON are not cached.
func (c *FileCache) Set(key string, body []byte, ttl time.Duration) error {
	if !json.Valid(body) {
		return nil
	}
	if err := os.MkdirAll(c.Dir, 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(cacheEntry{Expires: c.now().Add(ttl), Body: body})
	if err != nil {
		return err
	}
	return os.WriteFile(c.path(key), b, 0o600)
}

// Delete removes one entry.
func (c *FileCache) Delete(key string) error {
	err := os.Remove(c.path(key))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// Clear removes every cached entry.
func (c *FileCache) Clear() error {
	return os.RemoveAll(c.Dir)
}
