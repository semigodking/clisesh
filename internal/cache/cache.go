// Package cache stores generated session descriptions per provider in a
// versioned JSON file with atomic writes. A version mismatch or a corrupt
// file loads as an empty cache (self-healing), never a fatal error.
package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Version is the cache schema version. Bump to invalidate all existing
// cached descriptions after a format change.
const Version = 1

const versionKey = "__version__"

// Cache holds descriptions keyed by session ID for a single provider.
type Cache struct {
	path    string
	entries map[string]string
}

// Open loads (or initializes) the cache for a provider under cacheDir.
// A missing file, version mismatch, or corrupt content yields an empty cache.
func Open(cacheDir, provider string) *Cache {
	c := &Cache{
		path:    filepath.Join(cacheDir, provider+"-descriptions.json"),
		entries: map[string]string{},
	}
	data, err := os.ReadFile(c.path)
	if err != nil {
		return c // missing file: empty cache
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return c // corrupt: empty cache
	}
	// Version check.
	var ver int
	if v, ok := raw[versionKey]; ok {
		_ = json.Unmarshal(v, &ver)
	}
	if ver != Version {
		return c // stale: empty cache
	}
	for k, v := range raw {
		if k == versionKey {
			continue
		}
		var s string
		if err := json.Unmarshal(v, &s); err == nil {
			c.entries[k] = s
		}
	}
	return c
}

// Get returns the cached description for id.
func (c *Cache) Get(id string) (string, bool) {
	s, ok := c.entries[id]
	return s, ok
}

// Set stores a description for id.
func (c *Cache) Set(id, desc string) { c.entries[id] = desc }

// Delete removes an entry (no-op if absent).
func (c *Cache) Delete(id string) { delete(c.entries, id) }

// Len returns the number of cached descriptions.
func (c *Cache) Len() int { return len(c.entries) }

// Path returns the on-disk path of this cache.
func (c *Cache) Path() string { return c.path }

// Save writes the cache atomically (temp file + rename). Creates the cache
// directory if needed.
func (c *Cache) Save() error {
	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating cache dir: %w", err)
	}

	out := make(map[string]any, len(c.entries)+1)
	out[versionKey] = Version
	for k, v := range c.entries {
		out[k] = v
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".tmp-*-"+filepath.Base(c.path))
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, c.path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
