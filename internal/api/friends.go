package api

import (
	"net/url"
	"strings"

	"github.com/tomikav/steam-achievements-cli-/internal/model"
)

type friendListResponse struct {
	Friendslist struct {
		Friends []struct {
			SteamID     string `json:"steamid"`
			FriendSince int64  `json:"friend_since"`
		} `json:"friends"`
	} `json:"friendslist"`
}

type fullSummary struct {
	SteamID    string `json:"steamid"`
	Persona    string `json:"personaname"`
	State      int    `json:"personastate"`
	Game       string `json:"gameextrainfo"`
	GameID     string `json:"gameid"`
	LastLogoff int64  `json:"lastlogoff"`
	Avatar     string `json:"avatarmedium"`
	ProfileURL string `json:"profileurl"`
}

// Friends returns the user's friends with their current status. Steam only
// returns the list when the user's "Friends List" privacy setting is Public;
// otherwise the error has KindPrivateFriends.
func (c *Client) Friends() ([]model.Friend, error) {
	var fl friendListResponse
	params := url.Values{"key": {c.Key}, "steamid": {c.SteamID}, "relationship": {"friend"}, "format": {"json"}}
	// Single attempt: a private list answers 401 and retrying will not help.
	c.waitCooldown()
	if err := c.getOnce("/ISteamUser/GetFriendList/v1/", params, &fl); err != nil {
		if KindOf(err) == KindForbidden {
			return nil, &Error{Kind: KindPrivateFriends}
		}
		return nil, err
	}
	since := map[string]int64{}
	var ids []string
	for _, f := range fl.Friendslist.Friends {
		since[f.SteamID] = f.FriendSince
		ids = append(ids, f.SteamID)
	}
	var out []model.Friend
	for start := 0; start < len(ids); start += 100 { // API limit per call
		chunk := ids[start:min(start+100, len(ids))]
		var resp struct {
			Response struct {
				Players []fullSummary `json:"players"`
			} `json:"response"`
		}
		p := url.Values{"key": {c.Key}, "steamids": {strings.Join(chunk, ",")}, "format": {"json"}}
		if err := c.get("/ISteamUser/GetPlayerSummaries/v2/", p, &resp); err != nil {
			return nil, err
		}
		for _, s := range resp.Response.Players {
			out = append(out, model.Friend{
				SteamID: s.SteamID, Name: s.Persona, State: s.State, Game: s.Game, GameID: s.GameID,
				LastLogoff: s.LastLogoff, FriendSince: since[s.SteamID], Avatar: s.Avatar, ProfileURL: s.ProfileURL,
			})
		}
	}
	return out, nil
}
