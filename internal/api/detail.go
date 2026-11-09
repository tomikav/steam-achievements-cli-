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
		"format":  {"json"},
	}
	var out playerStatsResponse
	if err := c.get("/ISteamUserStats/GetPlayerAchievements/v0001/", params, &out); err != nil {
		return nil, err
	}
	if out.Playerstats.Error != nil {
		return nil, fmt.Errorf("%s", out.Playerstats.Error.ErrorDesc)
	}
	ach := make([]model.Achievement, 0, len(out.Playerstats.Achievements))
	for _, a := range out.Playerstats.Achievements {
		ach = append(ach, model.Achievement{Name: a.Name, Achieved: a.Achieved == 1})
	}
	return ach, nil
}
