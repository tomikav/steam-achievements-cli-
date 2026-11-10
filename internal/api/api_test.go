package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tomikav/steam-achievements-cli-/internal/model"
)

const (
	testKey = "0123456789ABCDEF0123456789ABCDEF"
	testID  = "76561198000000000"
)

// fakeSteam serves canned responses keyed by endpoint path.
func fakeSteam(t *testing.T, routes map[string]func(w http.ResponseWriter, r *http.Request)) {
	t.Helper()
	oldBackoff := backoff
	backoff = []time.Duration{time.Millisecond}
	t.Cleanup(func() { backoff = oldBackoff })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h, ok := routes[r.URL.Path]; ok {
			h(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	old := BaseURL
	BaseURL = srv.URL
	t.Cleanup(func() { BaseURL = old; srv.Close() })
}

func status(code int, body string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}
}

func summary(visibility string) func(http.ResponseWriter, *http.Request) {
	return status(200, `{"response":{"players":[{"steamid":"`+testID+`","personaname":"tester","communityvisibilitystate":`+visibility+`}]}}`)
}

const (
	pathOwned     = "/IPlayerService/GetOwnedGames/v1/"
	pathSummaries = "/ISteamUser/GetPlayerSummaries/v2/"
	pathPlayer    = "/ISteamUserStats/GetPlayerAchievements/v1/"
)

func TestOwnedGamesInvalidKey(t *testing.T) {
	fakeSteam(t, map[string]func(http.ResponseWriter, *http.Request){
		pathOwned:     status(403, "Forbidden"),
		pathSummaries: status(403, "Forbidden"),
	})
	_, err := New(testKey, testID).OwnedGames()
	if KindOf(err) != KindInvalidKey {
		t.Fatalf("want KindInvalidKey, got %v (%v)", KindOf(err), err)
	}
}

func TestOwnedGamesPrivateProfileEmptyBody(t *testing.T) {
	// Steam answers 200 with an empty object for private libraries.
	fakeSteam(t, map[string]func(http.ResponseWriter, *http.Request){
		pathOwned:     status(200, `{"response":{}}`),
		pathSummaries: summary("1"),
	})
	_, err := New(testKey, testID).OwnedGames()
	if KindOf(err) != KindPrivateProfile {
		t.Fatalf("want KindPrivateProfile, got %v (%v)", KindOf(err), err)
	}
}

func TestOwnedGamesPrivateGameDetails(t *testing.T) {
	fakeSteam(t, map[string]func(http.ResponseWriter, *http.Request){
		pathOwned:     status(200, `{"response":{}}`),
		pathSummaries: summary("3"),
	})
	_, err := New(testKey, testID).OwnedGames()
	if KindOf(err) != KindPrivateGames {
		t.Fatalf("want KindPrivateGames, got %v (%v)", KindOf(err), err)
	}
}

func TestOwnedGamesForbiddenButKeyValid(t *testing.T) {
	fakeSteam(t, map[string]func(http.ResponseWriter, *http.Request){
		pathOwned:     status(403, "Forbidden"),
		pathSummaries: summary("1"),
	})
	_, err := New(testKey, testID).OwnedGames()
	if KindOf(err) != KindPrivateProfile {
		t.Fatalf("want KindPrivateProfile, got %v (%v)", KindOf(err), err)
	}
}

func TestOwnedGamesSendsExpectedParams(t *testing.T) {
	fakeSteam(t, map[string]func(http.ResponseWriter, *http.Request){
		pathOwned: func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			for k, want := range map[string]string{"key": testKey, "steamid": testID, "include_appinfo": "true", "format": "json"} {
				if got := q.Get(k); got != want {
					t.Errorf("param %s = %q, want %q", k, got, want)
				}
			}
			_, _ = w.Write([]byte(`{"response":{"game_count":1,"games":[{"appid":10,"name":"Counter-Strike","playtime_forever":60}]}}`))
		},
	})
	games, err := New(testKey, testID).OwnedGames()
	if err != nil || len(games) != 1 || games[0].Name != "Counter-Strike" {
		t.Fatalf("unexpected result: %v %+v", err, games)
	}
}

func TestFetchProgressClassifies(t *testing.T) {
	fakeSteam(t, map[string]func(http.ResponseWriter, *http.Request){
		pathPlayer: func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Query().Get("appid") {
			case "1":
				_, _ = w.Write([]byte(`{"playerstats":{"success":true,"achievements":[{"name":"A","achieved":1},{"name":"B","achieved":0}]}}`))
			case "2":
				w.WriteHeader(400)
				_, _ = w.Write([]byte(`{"playerstats":{"error":"Requested app has no stats","success":false}}`))
			case "3":
				w.WriteHeader(403)
			default:
				w.WriteHeader(500)
			}
		},
	})
	games := []model.Game{{AppID: 1}, {AppID: 2}, {AppID: 3}, {AppID: 4}}
	res := New(testKey, testID).FetchProgress(games, 2, nil)
	if games[0].Achieved != 1 || games[0].Total != 2 || !games[0].HasStats {
		t.Errorf("game 1 = %+v", games[0])
	}
	if games[1].HasStats {
		t.Errorf("game 2 should have no stats")
	}
	if res.Forbidden != 1 || res.Failed != 1 {
		t.Errorf("forbidden=%d failed=%d, want 1 and 1", res.Forbidden, res.Failed)
	}
	if !res.Definitive[1] || !res.Definitive[2] || res.Definitive[3] || res.Definitive[4] {
		t.Errorf("definitive = %v", res.Definitive)
	}
}

func TestNetworkErrorDoesNotLeakKey(t *testing.T) {
	old := BaseURL
	BaseURL = "http://127.0.0.1:1"
	defer func() { BaseURL = old }()
	_, err := New(testKey, testID).OwnedGames()
	if err == nil {
		t.Fatal("expected an error")
	}
	if KindOf(err) != KindNetwork {
		t.Errorf("want KindNetwork, got %v", KindOf(err))
	}
	if strings.Contains(err.Error(), testKey) {
		t.Errorf("error message leaks the API key: %v", err)
	}
}

func TestResolveVanity(t *testing.T) {
	fakeSteam(t, map[string]func(http.ResponseWriter, *http.Request){
		"/ISteamUser/ResolveVanityURL/v1/": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("vanityurl") == "tomika" {
				_, _ = w.Write([]byte(`{"response":{"steamid":"` + testID + `","success":1}}`))
				return
			}
			_, _ = w.Write([]byte(`{"response":{"success":42,"message":"No match"}}`))
		},
	})
	c := New(testKey, "")
	if id, err := c.ResolveVanity("tomika"); err != nil || id != testID {
		t.Fatalf("resolve: %v %q", err, id)
	}
	if _, err := c.ResolveVanity("nobody"); KindOf(err) != KindBadSteamID {
		t.Fatalf("want KindBadSteamID, got %v", err)
	}
}

func TestForbiddenDetail(t *testing.T) {
	fakeSteam(t, map[string]func(http.ResponseWriter, *http.Request){
		pathPlayer: status(403, `{"playerstats":{"error":"Profile is not public","success":false}}`),
	})
	_, _, err := New(testKey, testID).AchievementProgress(1)
	if KindOf(err) != KindForbidden || !strings.Contains(err.Error(), "Profile is not public") {
		t.Fatalf("got %v", err)
	}
}

func TestTransient403IsRetried(t *testing.T) {
	var calls atomic.Int32
	fakeSteam(t, map[string]func(http.ResponseWriter, *http.Request){
		pathSummaries: func(w http.ResponseWriter, r *http.Request) {
			if calls.Add(1) < 3 {
				w.WriteHeader(403)
				return
			}
			summary("3")(w, r)
		},
	})
	s, err := New(testKey, testID).PlayerSummary()
	if err != nil || !s.Public() {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if calls.Load() != 3 {
		t.Errorf("calls = %d, want 3", calls.Load())
	}
}

func TestNotPublicIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	fakeSteam(t, map[string]func(http.ResponseWriter, *http.Request){
		pathPlayer: func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			status(403, `{"playerstats":{"error":"Profile is not public","success":false}}`)(w, r)
		},
	})
	_, _, _ = New(testKey, testID).AchievementProgress(1)
	if calls.Load() != 1 {
		t.Errorf("calls = %d, want 1", calls.Load())
	}
}

func TestFriends(t *testing.T) {
	fakeSteam(t, map[string]func(http.ResponseWriter, *http.Request){
		"/ISteamUser/GetFriendList/v1/": status(200, `{"friendslist":{"friends":[{"steamid":"1","friend_since":10},{"steamid":"2","friend_since":20}]}}`),
		pathSummaries: func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("steamids") != "1,2" {
				t.Errorf("steamids = %q", r.URL.Query().Get("steamids"))
			}
			_, _ = w.Write([]byte(`{"response":{"players":[{"steamid":"1","personaname":"Ana","personastate":1,"gameextrainfo":"Hades"},{"steamid":"2","personaname":"Bo","personastate":0}]}}`))
		},
	})
	fr, err := New(testKey, testID).Friends()
	if err != nil || len(fr) != 2 || fr[0].Game != "Hades" || fr[1].FriendSince != 20 {
		t.Fatalf("friends: %v %+v", err, fr)
	}
}

func TestFriendsPrivate(t *testing.T) {
	fakeSteam(t, map[string]func(http.ResponseWriter, *http.Request){
		"/ISteamUser/GetFriendList/v1/": status(401, ""),
	})
	if _, err := New(testKey, testID).Friends(); KindOf(err) != KindPrivateFriends {
		t.Fatalf("want KindPrivateFriends, got %v", err)
	}
}

func TestStoreDetails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("appids") != "620" {
			t.Errorf("appids = %q", r.URL.Query().Get("appids"))
		}
		_, _ = w.Write([]byte(`{"620":{"success":true,"data":{"name":"Portal 2","short_description":"Sequel &amp; <b>more</b> puzzles.",
			"developers":["Valve"],"publishers":["Valve"],"header_image":"https://x/h.jpg","genres":[{"description":"Action"},{"description":"Adventure"}],
			"release_date":{"date":"18 Apr, 2011"},"metacritic":{"score":95},"platforms":{"windows":true,"mac":true,"linux":false}}}}`))
	}))
	defer srv.Close()
	old := StoreURL
	StoreURL = srv.URL
	defer func() { StoreURL = old }()
	info, err := StoreDetails(620)
	if err != nil {
		t.Fatal(err)
	}
	if info.Description != "Sequel & more puzzles." || info.Metacritic != 95 || len(info.Genres) != 2 || strings.Join(info.Platforms, ",") != "windows,mac" {
		t.Fatalf("%+v", info)
	}
}

func TestPlayerAchievementsRealPayload(t *testing.T) {
	fakeSteam(t, map[string]func(http.ResponseWriter, *http.Request){
		pathPlayer: func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("l") != "english" {
				t.Errorf("language not requested")
			}
			_, _ = w.Write([]byte(`{"playerstats":{"steamID":"1","gameName":"G","achievements":[
				{"apiname":"ACH_WIN","achieved":1,"unlocktime":1700000000,"name":"Winner","description":"Win once"},
				{"apiname":"ACH_LOSE","achieved":0,"unlocktime":0,"name":"Loser","description":""}],"success":true}}`))
		},
		"/ISteamUserStats/GetSchemaForGame/v2/": status(200, `{"game":{"gameName":"G","availableGameStats":{"achievements":[
			{"name":"ACH_WIN","displayName":"Winner","description":"Win once","icon":"https://x/win.jpg","icongray":"https://x/wing.jpg","hidden":0},
			{"name":"ACH_LOSE","displayName":"Loser","hidden":1,"icon":"https://x/l.jpg","icongray":"https://x/lg.jpg"}]}}}`),
		"/ISteamUserStats/GetGlobalAchievementPercentagesForApp/v2/": status(200, `{"achievementpercentages":{"achievements":[
			{"name":"ACH_WIN","percent":"12.5"},{"name":"ACH_LOSE","percent":3.25}]}}`),
	})
	c := New(testKey, testID)
	ach, err := c.PlayerAchievements(1)
	if err != nil {
		t.Fatal(err)
	}
	_, defs, err := c.GameDetail(1)
	if err != nil {
		t.Fatal(err)
	}
	ach = c.MergeDetail(ach, defs)
	if ach[0].Name != "ACH_WIN" || ach[0].DisplayName != "Winner" || ach[0].Description != "Win once" ||
		ach[0].Global != 12.5 || ach[0].Icon != "https://x/win.jpg" || !ach[0].Achieved || ach[0].UnlockTime != 1700000000 {
		t.Fatalf("first: %+v", ach[0])
	}
	if ach[1].Global != 3.25 || !ach[1].Hidden || ach[1].IconGray != "https://x/lg.jpg" {
		t.Fatalf("second: %+v", ach[1])
	}
}
