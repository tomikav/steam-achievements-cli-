// Package api is a thin client over the Steam Web API.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/tomikav/steam-achievements-cli-/internal/model"
)

const baseURL = "https://api.steampowered.com"

// Client talks to the Steam Web API on behalf of one user.
type Client struct {
	Key     string
	SteamID string
	HTTP    *http.Client
}

// New builds a client with a sane default timeout.
func New(key, steamID string) *Client {
	return &Client{Key: key, SteamID: steamID, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) get(endpoint string, params url.Values, out any) error {
	req, err := http.NewRequest(http.MethodGet, baseURL+endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("Steam rejected the request (403): check that your API key is valid, that the SteamID is correct, and that your profile's game details are public (Privacy Settings > Game details)")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Steam API returned HTTP %d for %s", resp.StatusCode, endpoint)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// --- Owned games -----------------------------------------------------------

type ownedGamesResponse struct {
	Response struct {
		GameCount int `json:"game_count"`
		Games     []struct {
			AppID      int    `json:"appid"`
			Name       string `json:"name"`
			Playtime   int    `json:"playtime_forever"`
			ImgIcon    string `json:"img_icon_url"`
			LastPlayed int64  `json:"rtime_last_played"`
		} `json:"games"`
	} `json:"response"`
}

// OwnedGames returns every game in the user's library (without stats yet).
func (c *Client) OwnedGames() ([]model.Game, error) {
	params := url.Values{
		"key":                    {c.Key},
		"steamid":                {c.SteamID},
		"include_appinfo":        {"true"},
		"include_played_free_games": {"true"},
		"format":                 {"json"},
	}
	var out ownedGamesResponse
	if err := c.get("/IPlayerService/GetOwnedGames/v0001/", params, &out); err != nil {
		return nil, err
	}
	games := make([]model.Game, 0, len(out.Response.Games))
	for _, g := range out.Response.Games {
		games = append(games, model.Game{
			AppID:      g.AppID,
			Name:       g.Name,
			Playtime:   g.Playtime,
			LastPlayed: g.LastPlayed,
		})
	}
	return games, nil
}

// --- Achievement progress ---------------------------------------------------

type playerStatsResponse struct {
	Playerstats struct {
		GameName string `json:"gameName"`
		Error    *struct {
			ErrorCode int    `json:"errorcode"`
			ErrorDesc string `json:"errordesc"`
		} `json:"error"`
		Achievements []struct {
			Name     string `json:"name"`
			Achieved int    `json:"achieved"`
		} `json:"achievements"`
	} `json:"playerstats"`
}

// AchievementProgress returns (achieved, total, err) for one game. Games with
// no achievement system return an error (treated as "no stats" upstream).
func (c *Client) AchievementProgress(appID int) (achieved, total int, err error) {
	params := url.Values{
		"key":     {c.Key},
		"steamid": {c.SteamID},
		"appid":   {fmt.Sprint(appID)},
		"format":  {"json"},
	}
	var out playerStatsResponse
	if err := c.get("/ISteamUserStats/GetPlayerAchievements/v0001/", params, &out); err != nil {
		return 0, 0, err
	}
	if out.Playerstats.Error != nil {
		return 0, 0, fmt.Errorf("%s", out.Playerstats.Error.ErrorDesc)
	}
	total = len(out.Playerstats.Achievements)
	for _, a := range out.Playerstats.Achievements {
		if a.Achieved == 1 {
			achieved++
		}
	}
	return achieved, total, nil
}

// FetchProgress fills in achievement counts for games concurrently and calls
// cb with (done, total, game) after each result. Safe for concurrent use.
func (c *Client) FetchProgress(games []model.Game, workers int, cb func(done, total int, g model.Game)) {
	if workers < 1 {
		workers = 1
	}
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	var mu sync.Mutex
	done := 0
	total := len(games)
	for i := range games {
		wg.Add(1)
		go func(g *model.Game) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			a, t, err := c.AchievementProgress(g.AppID)
			mu.Lock()
			done++
			d := done
			mu.Unlock()
			if err == nil {
				g.Achieved = a
				g.Total = t
				g.HasStats = true
			}
			if cb != nil {
				cb(d, total, *g)
			}
		}(&games[i])
	}
	wg.Wait()
}

// --- Vanity URL --------------------------------------------------------------

type vanityResponse struct {
	Response struct {
		Success int    `json:"success"`
		SteamID string `json:"steamid"`
		Message string `json:"message"`
	} `json:"response"`
}

// ResolveVanity turns a custom URL name into a SteamID64.
func (c *Client) ResolveVanity(name string) (string, error) {
	params := url.Values{"key": {c.Key}, "vanityurl": {name}, "format": {"json"}}
	var out vanityResponse
	if err := c.get("/ISteamUser/ResolveVanityURL/v0001/", params, &out); err != nil {
		return "", err
	}
	if out.Response.Success != 1 || out.Response.SteamID == "" {
		return "", fmt.Errorf("could not resolve vanity URL %q", name)
	}
	return out.Response.SteamID, nil
}

// --- Schema (for the detail view) --------------------------------------------

type schemaResponse struct {
	Game             gameSchema `json:"game"`
	AvailableStats   available  `json:"availableGameStats"`
}
type gameSchema struct {
	GameName string `json:"gameName"`
}
type available struct {
	Achievements []struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
	} `json:"achievements"`
}
type globalResponse struct {
	Achievementpercentages struct {
		Achievements []struct {
			Name  string  `json:"name"`
			Percent float64 `json:"percent"`
		} `json:"achievements"`
	} `json:"achievementpercentages"`
}

// GameDetail returns the display names and global percentages of every
// achievement of a game, keyed by API name.
func (c *Client) GameDetail(appID int) (gameName string, defs map[string]model.Achievement, err error) {
	defs = map[string]model.Achievement{}

	var schema schemaResponse
	schemaParams := url.Values{"key": {c.Key}, "appid": {fmt.Sprint(appID)}, "format": {"json"}}
	if err := c.get("/ISteamUserStats/GetSchemaForGame/v2/", schemaParams, &schema); err == nil {
		gameName = schema.Game.GameName
		for _, a := range schema.AvailableStats.Achievements {
			defs[a.Name] = model.Achievement{Name: a.Name, DisplayName: a.DisplayName}
		}
	}

	var glob globalResponse
	globParams := url.Values{"key": {c.Key}, "gameid": {fmt.Sprint(appID)}, "format": {"json"}}
	if err := c.get("/ISteamUserStats/GetGlobalAchievementPercentagesForApp/v0002/", globParams, &glob); err == nil {
		for _, a := range glob.Achievementpercentages.Achievements {
			d, ok := defs[a.Name]
			if !ok {
				continue
			}
			d.Global = a.Percent
			defs[a.Name] = d
		}
	}

	if len(defs) == 0 {
		return gameName, defs, fmt.Errorf("could not load achievement schema for app %d", appID)
	}
	return gameName, defs, nil
}

// MergeDetail attaches schema/global info to the player's achievement state.
func (c *Client) MergeDetail(appID int, ach []model.Achievement, defs map[string]model.Achievement) []model.Achievement {
	for i, a := range ach {
		if d, ok := defs[a.Name]; ok {
			ach[i].DisplayName = strings.TrimSpace(d.DisplayName)
			ach[i].Global = d.Global
		}
	}
	return ach
}
