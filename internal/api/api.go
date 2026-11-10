// Package api is a thin client over the Steam Web API.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/tomikav/steam-achievements-cli-/internal/model"
)

// BaseURL is the Steam Web API root. It is a variable so tests can point the
// client at a local server.
var BaseURL = "https://api.steampowered.com"

// UserAgent is sent with every request.
var UserAgent = "steam-achievements-cli"

// Client talks to the Steam Web API on behalf of one user.
type Client struct {
	Key     string
	SteamID string
	HTTP    *http.Client

	// Shared cooldown: when Steam starts refusing, every worker pauses
	// together instead of each one hammering the API on its own schedule.
	coolMu    sync.Mutex
	coolUntil time.Time
}

func (c *Client) waitCooldown() {
	c.coolMu.Lock()
	d := time.Until(c.coolUntil)
	c.coolMu.Unlock()
	if d > 0 {
		time.Sleep(d)
	}
}

func (c *Client) setCooldown(d time.Duration) {
	c.coolMu.Lock()
	if t := time.Now().Add(d); t.After(c.coolUntil) {
		c.coolUntil = t
	}
	c.coolMu.Unlock()
}

// New builds a client tuned for many small parallel requests to one host.
func New(key, steamID string) *Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	// The default keeps only 2 idle connections per host, which forces a new
	// TLS handshake for most requests when several workers run in parallel.
	tr.MaxIdleConns = 64
	tr.MaxIdleConnsPerHost = 64
	tr.MaxConnsPerHost = 0
	tr.IdleConnTimeout = 90 * time.Second
	tr.ForceAttemptHTTP2 = true
	return &Client{Key: key, SteamID: steamID, HTTP: &http.Client{Timeout: 20 * time.Second, Transport: tr}}
}

// --- errors ----------------------------------------------------------------

// Kind classifies a failure so the CLI can print an actionable fix.
type Kind int

const (
	KindUnknown        Kind = iota
	KindForbidden           // HTTP 401/403, cause not yet diagnosed
	KindInvalidKey          // the API key itself is rejected
	KindPrivateProfile      // profile visibility is not public
	KindPrivateGames        // profile is public but game details are not
	KindBadSteamID          // the SteamID does not exist / vanity not found
	KindRateLimited         // HTTP 429
	KindNetwork             // DNS, TLS, timeout, proxy...
	KindNoStats             // the game has no achievement system
)

// Error is returned by every client call that fails.
type Error struct {
	Kind     Kind
	Status   int
	Endpoint string
	Msg      string
	Detail   string // Steam's own explanation, when it sent one
}

func (e *Error) Error() string {
	var b strings.Builder
	if e.Msg != "" {
		b.WriteString(e.Msg)
	} else {
		switch e.Kind {
		case KindForbidden:
			b.WriteString("Steam denied access")
		case KindInvalidKey:
			b.WriteString("Steam rejected the API key")
		case KindPrivateProfile:
			b.WriteString("the Steam profile is not public")
		case KindPrivateGames:
			b.WriteString("the profile's game details are not public")
		case KindBadSteamID:
			b.WriteString("SteamID not found")
		case KindRateLimited:
			b.WriteString("Steam is rate limiting requests")
		case KindNoStats:
			b.WriteString("game has no achievements")
		default:
			b.WriteString("Steam API request failed")
		}
	}
	if e.Status != 0 {
		fmt.Fprintf(&b, " (HTTP %d)", e.Status)
	}
	if e.Detail != "" {
		b.WriteString(": " + e.Detail)
	}
	return b.String()
}

// KindOf extracts the Kind of err, or KindUnknown.
func KindOf(err error) Kind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return KindUnknown
}

// --- transport ---------------------------------------------------------------

// Retry policy. Steam answers bursts of traffic with 403 or 429 instead of
// a consistent rate-limit signal, so transient refusals are retried with
// backoff. Refusals that state a reason ("Profile is not public") are final.
var (
	maxAttempts = 4
	backoff     = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}
)

func (c *Client) get(endpoint string, params url.Values, out any) error {
	var err error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		c.waitCooldown()
		err = c.getOnce(endpoint, params, out)
		if !retryable(err) {
			return err
		}
		c.setCooldown(backoff[min(attempt, len(backoff)-1)])
	}
	return err
}

func retryable(err error) bool {
	var e *Error
	if !errors.As(err, &e) {
		return false
	}
	switch e.Kind {
	case KindRateLimited:
		return true
	case KindForbidden:
		return !e.NotPublic()
	case KindUnknown:
		return e.Status >= 500
	}
	return false
}

// NotPublic reports whether Steam said the data is hidden by privacy settings.
func (e *Error) NotPublic() bool {
	return strings.Contains(strings.ToLower(e.Detail), "not public")
}

func (c *Client) getOnce(endpoint string, params url.Values, out any) error {
	req, err := http.NewRequest(http.MethodGet, BaseURL+endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return &Error{Kind: KindUnknown, Endpoint: endpoint, Msg: "could not build request"}
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		// *url.Error embeds the full URL, which contains the API key. Keep
		// only the underlying cause.
		var ue *url.Error
		cause := err
		if errors.As(err, &ue) {
			cause = ue.Err
		}
		return &Error{Kind: KindNetwork, Endpoint: endpoint, Msg: "could not reach api.steampowered.com: " + cause.Error()}
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return &Error{Kind: KindForbidden, Status: resp.StatusCode, Endpoint: endpoint, Detail: readDetail(resp.Body)}
	case resp.StatusCode == http.StatusTooManyRequests:
		return &Error{Kind: KindRateLimited, Status: resp.StatusCode, Endpoint: endpoint}
	case resp.StatusCode == http.StatusBadRequest:
		// GetPlayerAchievements answers 400 for games without stats.
		return &Error{Kind: KindNoStats, Status: resp.StatusCode, Endpoint: endpoint}
	default:
		return &Error{Kind: KindUnknown, Status: resp.StatusCode, Endpoint: endpoint,
			Msg: "unexpected response from " + endpoint}
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return &Error{Kind: KindUnknown, Endpoint: endpoint, Msg: "could not decode response from " + endpoint}
	}
	return nil
}

var (
	tagRe   = regexp.MustCompile(`<[^>]*>`)
	spaceRe = regexp.MustCompile(`\s+`)
)

// readDetail extracts a short, human readable reason from an error body.
// Steam sends JSON for some endpoints and a small HTML page for others.
func readDetail(r io.Reader) string {
	body, _ := io.ReadAll(io.LimitReader(r, 8192))
	var js struct {
		Playerstats struct {
			Error string `json:"error"`
		} `json:"playerstats"`
	}
	if json.Unmarshal(body, &js) == nil && js.Playerstats.Error != "" {
		return js.Playerstats.Error
	}
	text := tagRe.ReplaceAllString(string(body), " ")
	text = strings.TrimSpace(spaceRe.ReplaceAllString(text, " "))
	text = strings.TrimPrefix(text, "Forbidden ")
	if text == "Forbidden" {
		return ""
	}
	if len(text) > 160 {
		text = text[:157] + "..."
	}
	return text
}

// --- player summary ------------------------------------------------------------

// Visibility values reported by GetPlayerSummaries (communityvisibilitystate).
// The API only distinguishes "public" from "not visible to this key".
const VisibilityPublic = 3

// Summary is the subset of GetPlayerSummaries the CLI uses.
type Summary struct {
	SteamID    string `json:"steamid"`
	Persona    string `json:"personaname"`
	ProfileURL string `json:"profileurl"`
	Visibility int    `json:"communityvisibilitystate"`
}

// Public reports whether the profile is visible to the Web API.
func (s Summary) Public() bool { return s.Visibility == VisibilityPublic }

type summariesResponse struct {
	Response struct {
		Players []Summary `json:"players"`
	} `json:"response"`
}

// PlayerSummary fetches the profile of c.SteamID. It works on private
// profiles too, so it is the cleanest way to test whether the key is valid.
func (c *Client) PlayerSummary() (Summary, error) {
	params := url.Values{"key": {c.Key}, "steamids": {c.SteamID}, "format": {"json"}}
	var out summariesResponse
	if err := c.get("/ISteamUser/GetPlayerSummaries/v2/", params, &out); err != nil {
		if KindOf(err) == KindForbidden {
			// Retries are exhausted at this point, so the refusal is persistent.
			return Summary{}, &Error{Kind: KindInvalidKey, Status: statusOf(err), Endpoint: "/ISteamUser/GetPlayerSummaries/v2/"}
		}
		return Summary{}, err
	}
	if len(out.Response.Players) == 0 {
		return Summary{}, &Error{Kind: KindBadSteamID, Msg: fmt.Sprintf("no Steam profile with SteamID %s", c.SteamID)}
	}
	return out.Response.Players[0], nil
}

// Diagnose explains why a library call failed by probing the key and the
// profile. It returns a classified *Error, falling back to orig.
func (c *Client) Diagnose(orig error) error {
	s, err := c.PlayerSummary()
	if err != nil {
		return err
	}
	if !s.Public() {
		return &Error{Kind: KindPrivateProfile}
	}
	if KindOf(orig) == KindForbidden {
		return &Error{Kind: KindPrivateGames}
	}
	return orig
}

func statusOf(err error) int {
	var e *Error
	if errors.As(err, &e) {
		return e.Status
	}
	return 0
}

// --- owned games -------------------------------------------------------------

type ownedGamesResponse struct {
	Response struct {
		GameCount int `json:"game_count"`
		Games     []struct {
			AppID      int    `json:"appid"`
			Name       string `json:"name"`
			Playtime   int    `json:"playtime_forever"`
			LastPlayed int64  `json:"rtime_last_played"`
			HasStats   bool   `json:"has_community_visible_stats"`
			Icon       string `json:"img_icon_url"`
		} `json:"games"`
	} `json:"response"`
}

// OwnedGames returns every game in the user's library (without stats yet).
// Failures are diagnosed, so a 403 comes back as KindInvalidKey,
// KindPrivateProfile or KindPrivateGames.
func (c *Client) OwnedGames() ([]model.Game, error) {
	params := url.Values{
		"key":                       {c.Key},
		"steamid":                   {c.SteamID},
		"include_appinfo":           {"true"},
		"include_played_free_games": {"true"},
		"format":                    {"json"},
	}
	var out ownedGamesResponse
	if err := c.get("/IPlayerService/GetOwnedGames/v1/", params, &out); err != nil {
		if KindOf(err) == KindForbidden {
			return nil, c.Diagnose(err)
		}
		return nil, err
	}
	// A private library is not an HTTP error: Steam answers 200 with an empty
	// response object. Tell that apart from a genuinely empty account.
	if len(out.Response.Games) == 0 {
		s, err := c.PlayerSummary()
		if err != nil {
			return nil, err
		}
		if !s.Public() {
			return nil, &Error{Kind: KindPrivateProfile}
		}
		if out.Response.GameCount == 0 {
			return nil, &Error{Kind: KindPrivateGames}
		}
	}
	games := make([]model.Game, 0, len(out.Response.Games))
	for _, g := range out.Response.Games {
		games = append(games, model.Game{
			AppID:      g.AppID,
			Name:       g.Name,
			Playtime:   g.Playtime,
			LastPlayed: g.LastPlayed,
			MayHaveAch: g.HasStats,
			Icon:       g.Icon,
		})
	}
	return games, nil
}

// --- achievement progress ------------------------------------------------------

type playerStatsResponse struct {
	Playerstats struct {
		GameName     string          `json:"gameName"`
		Success      *bool           `json:"success"`
		Error        json.RawMessage `json:"error"`
		Achievements []struct {
			Name     string `json:"name"`
			Achieved int    `json:"achieved"`
		} `json:"achievements"`
	} `json:"playerstats"`
}

func (p playerStatsResponse) err() error {
	if p.Playerstats.Success != nil && !*p.Playerstats.Success {
		msg := strings.Trim(string(p.Playerstats.Error), `"`)
		if msg == "" {
			msg = "Steam reported an error"
		}
		return &Error{Kind: KindNoStats, Msg: msg}
	}
	return nil
}

// AchievementProgress returns (achieved, total, err) for one game.
func (c *Client) AchievementProgress(appID int) (achieved, total int, err error) {
	params := url.Values{
		"key":     {c.Key},
		"steamid": {c.SteamID},
		"appid":   {fmt.Sprint(appID)},
		"format":  {"json"},
	}
	var out playerStatsResponse
	if err := c.get("/ISteamUserStats/GetPlayerAchievements/v1/", params, &out); err != nil {
		return 0, 0, err
	}
	if err := out.err(); err != nil {
		return 0, 0, err
	}
	total = len(out.Playerstats.Achievements)
	for _, a := range out.Playerstats.Achievements {
		if a.Achieved == 1 {
			achieved++
		}
	}
	return achieved, total, nil
}

// Skip describes one game whose achievements could not be read.
type Skip struct {
	AppID  int
	Name   string
	Reason string
}

// ProgressResult summarizes a FetchProgress run.
type ProgressResult struct {
	Forbidden int // requests answered 401/403 after retries
	NotPublic int // subset of Forbidden where Steam said "not public"
	Failed    int // network errors, 5xx, rate limits
	// Skipped lists the games that failed for reasons other than privacy,
	// with Steam's error, so the CLI can name them.
	Skipped []Skip
	// Definitive holds the AppIDs whose answer is safe to cache: either real
	// counts or a definitive "this game has no achievements".
	Definitive map[int]bool
}

// FetchProgress fills in achievement counts for games concurrently and calls
// cb after each result. definitive tells whether that result is safe to
// cache. cb may be called from several goroutines at once.
func (c *Client) FetchProgress(games []model.Game, workers int, cb func(done, total int, g model.Game, definitive bool)) ProgressResult {
	res := ProgressResult{Definitive: make(map[int]bool, len(games))}
	idx := make([]int, len(games))
	for i := range idx {
		idx[i] = i
	}
	var mu sync.Mutex
	done := 0
	c.fetchPerGame(games, idx, workers, &res, &mu, &done, len(games), cb)
	return res
}

func (c *Client) fetchPerGame(games []model.Game, idx []int, workers int, res *ProgressResult, mu *sync.Mutex, done *int, total int, cb func(done, total int, g model.Game, definitive bool)) {
	if workers < 1 {
		workers = 1
	}
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for _, i := range idx {
		wg.Add(1)
		go func(g *model.Game) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			a, t, err := c.AchievementProgress(g.AppID)
			mu.Lock()
			*done++
			d := *done
			definitive := false
			switch KindOf(err) {
			case KindUnknown:
				if err == nil {
					g.Achieved, g.Total, g.HasStats = a, t, t > 0
					res.Definitive[g.AppID] = true
					definitive = true
				} else {
					res.Failed++
					res.Skipped = append(res.Skipped, Skip{g.AppID, g.Name, err.Error()})
				}
			case KindNoStats:
				res.Definitive[g.AppID] = true
				definitive = true
			case KindForbidden:
				res.Forbidden++
				var e *Error
				if errors.As(err, &e) && e.NotPublic() {
					res.NotPublic++
				} else {
					res.Skipped = append(res.Skipped, Skip{g.AppID, g.Name, err.Error()})
				}
			default:
				res.Failed++
				res.Skipped = append(res.Skipped, Skip{g.AppID, g.Name, err.Error()})
			}
			mu.Unlock()
			if cb != nil {
				cb(d, total, *g, definitive)
			}
		}(&games[i])
	}
	wg.Wait()
}

// --- vanity URL ------------------------------------------------------------------

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
	if err := c.get("/ISteamUser/ResolveVanityURL/v1/", params, &out); err != nil {
		if KindOf(err) == KindForbidden {
			return "", &Error{Kind: KindInvalidKey, Status: statusOf(err)}
		}
		return "", err
	}
	if out.Response.Success != 1 || out.Response.SteamID == "" {
		return "", &Error{Kind: KindBadSteamID, Msg: fmt.Sprintf("no Steam profile with custom URL %q", name)}
	}
	return out.Response.SteamID, nil
}

// --- schema (detail view) ----------------------------------------------------------

type schemaResponse struct {
	Game struct {
		GameName       string `json:"gameName"`
		AvailableStats struct {
			Achievements []struct {
				Name        string `json:"name"`
				DisplayName string `json:"displayName"`
			} `json:"achievements"`
		} `json:"availableGameStats"`
	} `json:"game"`
}

type globalResponse struct {
	Achievementpercentages struct {
		Achievements []struct {
			Name    string          `json:"name"`
			Percent json.RawMessage `json:"percent"`
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
		for _, a := range schema.Game.AvailableStats.Achievements {
			defs[a.Name] = model.Achievement{Name: a.Name, DisplayName: a.DisplayName}
		}
	}

	var glob globalResponse
	globParams := url.Values{"gameid": {fmt.Sprint(appID)}, "format": {"json"}}
	if err := c.get("/ISteamUserStats/GetGlobalAchievementPercentagesForApp/v2/", globParams, &glob); err == nil {
		for _, a := range glob.Achievementpercentages.Achievements {
			d, ok := defs[a.Name]
			if !ok {
				continue
			}
			// Steam has sent this field both as a number and as a string.
			var pct float64
			if json.Unmarshal(a.Percent, &pct) != nil {
				var s string
				if json.Unmarshal(a.Percent, &s) == nil {
					fmt.Sscan(s, &pct)
				}
			}
			d.Global = pct
			defs[a.Name] = d
		}
	}

	if len(defs) == 0 {
		return gameName, defs, &Error{Kind: KindNoStats, Msg: fmt.Sprintf("could not load the achievement list for app %d", appID)}
	}
	return gameName, defs, nil
}

// MergeDetail attaches schema/global info to the player's achievement state.
func (c *Client) MergeDetail(ach []model.Achievement, defs map[string]model.Achievement) []model.Achievement {
	for i, a := range ach {
		if d, ok := defs[a.Name]; ok {
			ach[i].DisplayName = strings.TrimSpace(d.DisplayName)
			ach[i].Global = d.Global
		}
		if ach[i].DisplayName == "" {
			ach[i].DisplayName = a.Name
		}
	}
	return ach
}
