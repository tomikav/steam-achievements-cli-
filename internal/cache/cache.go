// Package cache stores achievement progress per AppID so repeated runs are
// instant. Entries for games not played since they were fetched are reused
// for up to MaxAge; everything else expires after TTL.
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
	// Unavailable marks a game Steam refused to report on. Such entries only
	// live for TTL, so the game is retried the next day instead of every run.
	Unavailable bool `json:"unavailable,omitempty"`
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

// MaxAge bounds how long an entry for an untouched game is trusted, so new
// achievements added by updates or DLC eventually show up.
const MaxAge = 30 * 24 * time.Hour

// Valid reports whether an entry is younger than TTL.
func Valid(e Entry) bool {
	return time.Now().Unix()-e.Fetched < int64(TTL/time.Second)
}

// Fresh reports whether a cached entry can be reused for a game last played
// at lastPlayed (unix seconds, 0 if unknown). Achievements only unlock while
// playing, so a game that was not played since the entry was fetched cannot
// have changed: those entries are kept for up to MaxAge instead of TTL.
func Fresh(e Entry, lastPlayed int64) bool {
	age := time.Now().Unix() - e.Fetched
	if age < 0 || age >= int64(MaxAge/time.Second) {
		return false
	}
	if e.Unavailable {
		return Valid(e)
	}
	if lastPlayed > 0 && lastPlayed < e.Fetched {
		return true
	}
	return Valid(e)
}
