// Package config persists the Steam API key and SteamID on disk and merges
// them with environment variables. Environment variables always win.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config is what we persist for the user.
type Config struct {
	APIKey  string `json:"api_key"`
	SteamID string `json:"steam_id"`
}

// Env var names honored by this tool.
const (
	EnvAPIKey = "STEAM_API_KEY"
	EnvSteamID = "STEAM_ID"
)

// FilePath returns the location of the config file.
func FilePath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "steam-achievements-cli", "config.json"), nil
}

// Load reads the config file if it exists. Missing file is not an error.
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
	err = json.Unmarshal(data, &cfg)
	return cfg, err
}

// Save writes the config file, creating the directory if needed.
func Save(cfg Config) error {
	path, err := FilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// Resolve merges config file values with environment variables
// (environment variables take precedence).
func Resolve() (Config, error) {
	cfg, err := Load()
	if err != nil {
		return cfg, err
	}
	if v := os.Getenv(EnvAPIKey); v != "" {
		cfg.APIKey = v
	}
	if v := os.Getenv(EnvSteamID); v != "" {
		cfg.SteamID = v
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
	return k[:4] + "…" + k[len(k)-4:]
}

// String returns a human friendly, secret-safe summary.
func (c Config) String() string {
	return fmt.Sprintf("api_key: %s\nsteam_id: %s", MaskKey(c.APIKey), c.displayID())
}

func (c Config) displayID() string {
	if c.SteamID == "" {
		return "(not set)"
	}
	return c.SteamID
}
