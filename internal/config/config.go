// Package config persists the Steam API key and SteamID on disk and merges
// them with environment variables. Environment variables always win.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Config is what we persist for the user.
type Config struct {
	APIKey  string `json:"api_key"`
	SteamID string `json:"steam_id"`
	Theme   string `json:"theme,omitempty"`

	// Where each credential came from after Resolve. Not persisted.
	KeySource string `json:"-"`
	IDSource  string `json:"-"`
}

// Env var names honored by this tool.
const (
	EnvAPIKey  = "STEAM_API_KEY"
	EnvSteamID = "STEAM_ID"
)

// Credential sources reported by Resolve.
const (
	SourceNone = "not set"
	SourceFile = "config file"
)

var (
	keyRe       = regexp.MustCompile(`^[0-9A-Fa-f]{32}$`)
	steamID64Re = regexp.MustCompile(`^\d{17}$`)
)

// ValidKey reports whether k looks like a Steam Web API key (32 hex chars).
func ValidKey(k string) bool { return keyRe.MatchString(k) }

// IsSteamID64 reports whether id is a numeric SteamID64 (17 digits).
func IsSteamID64(id string) bool { return steamID64Re.MatchString(id) }

// Normalize trims whitespace and quotes that often sneak in through copy and
// paste or `setx`, and reduces profile URLs to the ID or vanity name.
func Normalize(v string) string {
	v = strings.TrimSpace(v)
	v = strings.Trim(v, `"'`)
	v = strings.TrimSpace(v)
	for _, p := range []string{"https://steamcommunity.com/profiles/", "https://steamcommunity.com/id/",
		"http://steamcommunity.com/profiles/", "http://steamcommunity.com/id/",
		"steamcommunity.com/profiles/", "steamcommunity.com/id/"} {
		if strings.HasPrefix(v, p) {
			v = strings.TrimSuffix(strings.TrimPrefix(v, p), "/")
			break
		}
	}
	return v
}

// FilePath returns the location of the config file.
func FilePath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "steam-achievements-cli", "config.json"), nil
}

// Load reads the config file if it exists. A missing file is not an error.
func Load() (Config, error) {
	cfg := Config{}
	path, err := FilePath()
	if err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	// Tolerate a UTF-8 BOM, which Notepad and PowerShell 5 like to add.
	data = []byte(strings.TrimPrefix(string(data), "\uFEFF"))
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("config file %s is not valid JSON: %v", path, err)
	}
	cfg.APIKey = Normalize(cfg.APIKey)
	cfg.SteamID = Normalize(cfg.SteamID)
	return cfg, nil
}

// Save writes the config file, creating the directory if needed.
func Save(cfg Config) error {
	path, err := FilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// Resolve merges config file values with environment variables
// (environment variables take precedence) and records each value's source.
func Resolve() (Config, error) {
	cfg, err := Load()
	if err != nil {
		return cfg, err
	}
	cfg.KeySource, cfg.IDSource = SourceNone, SourceNone
	if cfg.APIKey != "" {
		cfg.KeySource = SourceFile
	}
	if cfg.SteamID != "" {
		cfg.IDSource = SourceFile
	}
	if v := Normalize(os.Getenv(EnvAPIKey)); v != "" {
		cfg.APIKey, cfg.KeySource = v, "env "+EnvAPIKey
	}
	if v := Normalize(os.Getenv(EnvSteamID)); v != "" {
		cfg.SteamID, cfg.IDSource = v, "env "+EnvSteamID
	}
	return cfg, nil
}

// MaskKey hides most of the API key for display purposes.
func MaskKey(k string) string {
	if k == "" {
		return "(not set)"
	}
	if len(k) <= 8 {
		return "********"
	}
	return k[:4] + "..." + k[len(k)-4:]
}
