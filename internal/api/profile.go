package api

import (
	"net/url"
	"sync"

	"github.com/tomikav/steam-achievements-cli-/internal/model"
)

// Profile gathers the user's public profile: summary, Steam level and
// badges, and the games played in the last two weeks. Only the summary is
// required; the rest is best effort because it follows privacy settings.
func (c *Client) Profile() (model.Profile, error) {
	var (
		wg       sync.WaitGroup
		sum      fullSummary
		sumErr   error
		badges   badgesResponse
		recent   recentResponse
		ok1, ok2 bool
		mu       sync.Mutex
		gotSumm  bool
	)
	wg.Add(3)
	go func() {
		defer wg.Done()
		var out struct {
			Response struct {
				Players []fullSummary `json:"players"`
			} `json:"response"`
		}
		err := c.get("/ISteamUser/GetPlayerSummaries/v2/", url.Values{"key": {c.Key}, "steamids": {c.SteamID}, "format": {"json"}}, &out)
		mu.Lock()
		defer mu.Unlock()
		sumErr = err
		if err == nil && len(out.Response.Players) > 0 {
			sum, gotSumm = out.Response.Players[0], true
		}
	}()
	go func() {
		defer wg.Done()
		err := c.getOnce("/IPlayerService/GetBadges/v1/", url.Values{"key": {c.Key}, "steamid": {c.SteamID}, "format": {"json"}}, &badges)
		mu.Lock()
		ok1 = err == nil
		mu.Unlock()
	}()
	go func() {
		defer wg.Done()
		err := c.getOnce("/IPlayerService/GetRecentlyPlayedGames/v1/", url.Values{"key": {c.Key}, "steamid": {c.SteamID}, "format": {"json"}}, &recent)
		mu.Lock()
		ok2 = err == nil
		mu.Unlock()
	}()
	wg.Wait()
	if sumErr != nil {
		return model.Profile{}, sumErr
	}
	if !gotSumm {
		return model.Profile{}, &Error{Kind: KindBadSteamID, Msg: "no Steam profile with SteamID " + c.SteamID}
	}
	p := model.Profile{
		SteamID: sum.SteamID, Persona: sum.Persona, RealName: sum.RealName, ProfileURL: sum.ProfileURL,
		Avatar: sum.AvatarFull, Country: sum.Country, Created: sum.Created, State: sum.State,
		Game: sum.Game, LastLogoff: sum.LastLogoff,
	}
	if p.Avatar == "" {
		p.Avatar = sum.Avatar
	}
	if ok1 {
		r := badges.Response
		p.Level, p.XP, p.XPToNext, p.XPThisLevel, p.Badges = r.Level, r.XP, r.XPNeeded, r.XPCurrent, len(r.Badges)
	}
	if ok2 {
		for _, g := range recent.Response.Games {
			p.Recent = append(p.Recent, model.RecentGame{AppID: g.AppID, Name: g.Name, TwoWeeks: g.TwoWeeks, Forever: g.Forever, Icon: g.Icon})
		}
	}
	return p, nil
}

type badgesResponse struct {
	Response struct {
		Badges    []struct{} `json:"badges"`
		XP        int        `json:"player_xp"`
		Level     int        `json:"player_level"`
		XPNeeded  int        `json:"player_xp_needed_to_level_up"`
		XPCurrent int        `json:"player_xp_needed_current_level"`
	} `json:"response"`
}

type recentResponse struct {
	Response struct {
		Games []struct {
			AppID    int    `json:"appid"`
			Name     string `json:"name"`
			TwoWeeks int    `json:"playtime_2weeks"`
			Forever  int    `json:"playtime_forever"`
			Icon     string `json:"img_icon_url"`
		} `json:"games"`
	} `json:"response"`
}
