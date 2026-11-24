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

	// Unlocks holds the unlock time of every unlocked achievement and Locked
	// the achievements still to get. Both feed the activity map and the hunt.
	Unlocks []int64       `json:"-"`
	Locked  []Achievement `json:"-"`
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
	// Price and extras, for the store view.
	Price          string   `json:"price,omitempty"`
	Original       string   `json:"original,omitempty"`
	Discount       int      `json:"discount,omitempty"`
	Cents          int      `json:"cents,omitempty"`
	Currency       string   `json:"currency,omitempty"`
	Free           bool     `json:"free,omitempty"`
	Reviews        int      `json:"reviews,omitempty"`
	Achievements   int      `json:"achievements,omitempty"`
	Categories     []string `json:"categories,omitempty"`
	ComingSoon     bool     `json:"coming_soon,omitempty"`
	ControllerSupp string   `json:"controller,omitempty"`
}

// Deal is a game with its current price, from the wishlist or the store's
// specials.
type Deal struct {
	AppID     int    `json:"appid"`
	Name      string `json:"name"`
	Discount  int    `json:"discount_percent"`
	Final     string `json:"final_price"`    // formatted, e.g. "$4.99"
	Original  string `json:"original_price"` // formatted, empty when not on sale
	Wishlist  bool   `json:"wishlist"`
	Free      bool   `json:"free,omitempty"`
	Available bool   `json:"available"`
}

// News is one entry of a game's news feed.
type News struct {
	AppID   int    `json:"appid"`
	Game    string `json:"game"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	Date    int64  `json:"date"`
	Summary string `json:"summary"`
	Feed    string `json:"feed"`
}

// StoreItem is one game in a store listing.
type StoreItem struct {
	AppID     int    `json:"appid"`
	Name      string `json:"name"`
	Discount  int    `json:"discount"`
	Final     string `json:"final"`    // formatted price, "Free" for free games
	Original  string `json:"original"` // formatted, only when discounted
	Cents     int    `json:"cents"`    // final price in cents, for totals
	Currency  string `json:"currency"`
	Free      bool   `json:"free"`
	Image     string `json:"image"`
	Platforms string `json:"platforms"` // "wml" subset: windows, mac, linux
	Metascore int    `json:"metascore,omitempty"`
	Soon      bool   `json:"soon,omitempty"` // not released yet
}

// CartItem is a game the user keeps in the local cart, with the price it
// had when it was added.
type CartItem struct {
	StoreItem
	Added      int64 `json:"added"`
	AddedCents int   `json:"added_cents"`
}

// RecentGame is a game played in the last two weeks.
type RecentGame struct {
	AppID    int    `json:"appid"`
	Name     string `json:"name"`
	TwoWeeks int    `json:"two_weeks"` // minutes
	Forever  int    `json:"forever"`   // minutes
	Icon     string `json:"icon,omitempty"`
}

// Profile is the public Steam profile of the user.
type Profile struct {
	SteamID     string       `json:"steamid"`
	Persona     string       `json:"persona"`
	RealName    string       `json:"real_name,omitempty"`
	ProfileURL  string       `json:"profile_url"`
	Avatar      string       `json:"avatar"` // full size
	Country     string       `json:"country,omitempty"`
	Created     int64        `json:"created,omitempty"`
	State       int          `json:"state"`
	Game        string       `json:"game,omitempty"`
	LastLogoff  int64        `json:"last_logoff,omitempty"`
	Level       int          `json:"level"`
	XP          int          `json:"xp"`
	XPToNext    int          `json:"xp_to_next"`
	XPThisLevel int          `json:"xp_this_level"` // XP needed to reach the current level
	Badges      int          `json:"badges"`
	Recent      []RecentGame `json:"recent,omitempty"`
}
