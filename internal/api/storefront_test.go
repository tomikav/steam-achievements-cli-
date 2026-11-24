package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStoreFrontAndSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/featuredcategories":
			_, _ = w.Write([]byte(`{"specials":{"id":"cat_specials","items":[
				{"id":10,"name":"Sale Game","discounted":true,"discount_percent":75,"original_price":1999,"final_price":499,"currency":"USD","header_image":"h.jpg","windows_available":true,"linux_available":true},
				{"id":10,"name":"Sale Game","final_price":499},
				{"id":11,"name":"Free Thing","original_price":null,"final_price":0,"currency":"USD"}]},
				"coming_soon":{"items":[{"id":12,"name":"Soon","final_price":0}]},
				"status":1}`))
		case "/api/storesearch/":
			if r.URL.Query().Get("term") != "hollow knight" {
				t.Errorf("term = %q", r.URL.Query().Get("term"))
			}
			_, _ = w.Write([]byte(`{"total":3,"items":[
				{"type":"app","name":"Hollow Knight","id":367520,"price":{"currency":"USD","initial":1499,"final":749},"metascore":"87","platforms":{"windows":true,"mac":true,"linux":true}},
				{"type":"app","name":"HK &amp; Friends","id":2,"metascore":""},
				{"type":"bundle","name":"Bundle","id":3}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	old := StoreURL
	StoreURL = srv.URL
	defer func() { StoreURL = old }()

	lists, err := StoreFront()
	if err != nil {
		t.Fatal(err)
	}
	sp := lists[SectionSpecials]
	if len(sp) != 2 || sp[0].Final != "$4.99" || sp[0].Original != "$19.99" || sp[0].Platforms != "wl" || !sp[1].Free {
		t.Fatalf("specials: %+v", sp)
	}
	if soon := lists[SectionSoon]; len(soon) != 1 || !soon[0].Soon || soon[0].Free {
		t.Fatalf("coming soon must not read as free: %+v", soon)
	}

	res, err := StoreSearch("hollow knight")
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 || res[0].Discount != 50 || res[0].Metascore != 87 || res[0].Platforms != "wml" || res[1].Name != "HK & Friends" || !res[1].Free {
		t.Fatalf("search: %+v", res)
	}
}

func TestProfile(t *testing.T) {
	fakeSteam(t, map[string]func(http.ResponseWriter, *http.Request){
		"/ISteamUser/GetPlayerSummaries/v2/": status(200, `{"response":{"players":[{"steamid":"1","personaname":"tomika","avatarfull":"https://x/a.jpg",
			"timecreated":1500000000,"loccountrycode":"CO","personastate":1,"gameextrainfo":"Hades"}]}}`),
		"/IPlayerService/GetBadges/v1/": status(200, `{"response":{"badges":[{},{},{}],"player_xp":1200,"player_level":12,
			"player_xp_needed_to_level_up":100,"player_xp_needed_current_level":1100}}`),
		"/IPlayerService/GetRecentlyPlayedGames/v1/": status(403, `forbidden`),
	})
	p, err := New(testKey, testID).Profile()
	if err != nil {
		t.Fatal(err)
	}
	if p.Persona != "tomika" || p.Level != 12 || p.Badges != 3 || p.XPToNext != 100 || p.Avatar != "https://x/a.jpg" || p.Game != "Hades" || len(p.Recent) != 0 {
		t.Fatalf("%+v", p)
	}
}
