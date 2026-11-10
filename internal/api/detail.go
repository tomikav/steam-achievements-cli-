package api

import (
	"fmt"
	"net/url"

	"github.com/tomikav/steam-achievements-cli-/internal/model"
)

// PlayerAchievements returns the player's per-achievement state for one game.
func (c *Client) PlayerAchievements(appID int) ([]model.Achievement, error) {
	params := url.Values{
		"key":     {c.Key},
		"steamid": {c.SteamID},
		"appid":   {fmt.Sprint(appID)},
		"l":       {"english"},
		"format":  {"json"},
	}
	var out playerStatsResponse
	if err := c.get("/ISteamUserStats/GetPlayerAchievements/v1/", params, &out); err != nil {
		if KindOf(err) == KindForbidden {
			return nil, c.Diagnose(err)
		}
		return nil, err
	}
	if err := out.err(); err != nil {
		return nil, err
	}
	ach := make([]model.Achievement, 0, len(out.Playerstats.Achievements))
	for _, a := range out.Playerstats.Achievements {
		ach = append(ach, model.Achievement{Name: a.APIName, DisplayName: a.Name, Description: a.Description,
			Achieved: a.Achieved == 1, UnlockTime: a.UnlockTime})
	}
	return ach, nil
}
