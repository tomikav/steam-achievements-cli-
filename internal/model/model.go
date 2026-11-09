// Package model holds the shared data types used across the CLI.
package model

// Game represents a game in the user's Steam library.
type Game struct {
	AppID      int     `json:"appid"`
	Name       string  `json:"name"`
	Playtime   int     `json:"playtime_minutes"`
	Achieved   int     `json:"achievements_unlocked"`
	Total      int     `json:"achievements_total"`
	Percent    float64 `json:"completion_percent"`
	LastPlayed int64   `json:"last_played_unix"`
	HasStats   bool    `json:"has_achievements"`
}

// Achievement is a single achievement of a game (used by `steam game`).
type Achievement struct {
	Name        string  `json:"api_name"`
	DisplayName string  `json:"display_name"`
	Achieved    bool    `json:"achieved"`
	Global      float64 `json:"global_percent"`
}
