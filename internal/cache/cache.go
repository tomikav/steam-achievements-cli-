// Package cache stores achievement progress per AppID so repeated runs are
// instant. Entries expire after TTL hours.
package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// TTL is how long a cached achievement count stays fresh.
const TTL = 24 * time.Hour

// Entry is the cached achievement state for one game.
type Entry struct {
	Achieved int   `json:"achieved"`
	Total    int   `json:"total"`
	Fetched  int64 `json:"fetched_unix"`
}

func filePath() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "steam-achievements-cli", "progress.json"), nil
}

// Load reads the whole cache; a missing or corrupt file yields an empty map.
func Load() map[int]Entry {
	out := map[int]Entry{}
	path, err := filePath()
	if err != nil {
		return out
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	_ = json.Unmarshal(data, &out)
	return out
}

// Save writes the whole cache back to disk (best effort).
func Save(m map[int]Entry) {
	path, err := filePath()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

// Valid reports whether an entry is still fresh.
func Valid(e Entry) bool {
	return time.Now().Unix()-e.Fetched < int64(TTL/time.Second)
}
