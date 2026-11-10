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

	// MayHaveAch mirrors GetOwnedGames' has_community_visible_stats. Games
	// without it have no achievements, so they need no per-game request.
	MayHaveAch bool `json:"-"`

	// Icon is the hash of the game's 32x32 library icon.
	Icon string `json:"-"`
}

// Achievement is a single achievement of a game (used by `steam game`).
type Achievement struct {
	Name        string  `json:"api_name"`
	DisplayName string  `json:"display_name"`
	Achieved    bool    `json:"achieved"`
	Global      float64 `json:"global_percent"`
	Description string  `json:"description,omitempty"`
	UnlockTime  int64   `json:"unlock_time,omitempty"`
	Hidden      bool    `json:"hidden,omitempty"`
	Icon        string  `json:"icon_url,omitempty"`      // colour icon URL
	IconGray    string  `json:"icon_gray_url,omitempty"` // locked icon URL
}

// Friend is one entry of the user's Steam friends list.
type Friend struct {
	SteamID     string `json:"steamid"`
	Name        string `json:"name"`
	State       int    `json:"state"` // 0 offline, 1 online, 2 busy, 3 away, 4 snooze, 5/6 looking to trade/play
	Game        string `json:"game,omitempty"`
	GameID      string `json:"game_id,omitempty"`
	LastLogoff  int64  `json:"last_logoff,omitempty"`
	FriendSince int64  `json:"friend_since,omitempty"`
	Avatar      string `json:"avatar,omitempty"` // 64 px avatar URL
	ProfileURL  string `json:"profile_url,omitempty"`
}

// StoreInfo is the public store page data of a game.
type StoreInfo struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Developers  []string `json:"developers"`
	Publishers  []string `json:"publishers"`
	Genres      []string `json:"genres"`
	Released    string   `json:"released"`
	Metacritic  int      `json:"metacritic,omitempty"`
	HeaderImage string   `json:"header_image"`
	Platforms   []string `json:"platforms"`
}
