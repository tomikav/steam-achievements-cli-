package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/tomikav/steam-achievements-cli-/internal/model"
)

func featureBrowser(w, h int) *browser {
	items := []model.StoreItem{
		{AppID: 9001, Name: "Lanternfall", Discount: 60, Cents: 799, Final: "$7.99", Original: "$19.99", Platforms: "wl"},
		{AppID: 9002, Name: "Arena of Echoes", Free: true, Final: "Free"},
		{AppID: 300, Name: "ELDEN RING", Cents: 5999, Final: "$59.99"},
	}
	var saved []model.CartItem
	opt := BrowseOptions{Games: demoLike(), Owner: "tomika",
		LoadStoreFront: func() (map[string][]model.StoreItem, error) {
			return map[string][]model.StoreItem{"specials": items, "top_sellers": items[:2]}, nil
		},
		SearchStore: func(t string) ([]model.StoreItem, error) { return items[:1], nil },
		LoadStore: func(id int) (model.StoreInfo, error) {
			return model.StoreInfo{Name: "x", Description: "A game.", Genres: []string{"Indie"}, Reviews: 12345}, nil
		},
		LoadProfile: func() (model.Profile, error) {
			return model.Profile{Persona: "tomika", Level: 47, XP: 100, XPToNext: 50, Created: time.Now().AddDate(-5, 0, 0).Unix(),
				Recent: []model.RecentGame{{Name: "Hades", TwoWeeks: 300, Forever: 3000}}}, nil
		},
		LoadCart: func() []model.CartItem { return saved },
		SaveCart: func(c []model.CartItem) { saved = c },
	}
	b := newBrowser(opt, w, h)
	b.setFriends([]model.Friend{{SteamID: "1", Name: "nightowl", State: 1, Game: "Hades"}}, nil)
	return b
}

func checkWidth(t *testing.T, name string, b *browser) {
	t.Helper()
	for i, l := range flatten(b.build(), b.w) {
		if width(l) > b.w {
			t.Fatalf("%s@%d: row %d is %d wide", name, b.w, i, width(l))
		}
	}
}

func TestCtrlQ(t *testing.T) {
	b := featureBrowser(120, 40)
	b.mode = modeList
	b.handleKey("/")
	b.handleKey("q") // typed into the search
	if b.quit || b.query != "q" {
		t.Fatalf("q while searching must type: quit=%v query=%q", b.quit, b.query)
	}
	b.handleKey("ctrl+q")
	if !b.quit {
		t.Fatal("ctrl+q quits even while typing")
	}
	if evs, _ := parseInput([]byte{0x11, 0x0b}); len(evs) != 2 || evs[0].key != "ctrl+q" || evs[1].key != "ctrl+k" {
		t.Fatalf("decode: %+v", evs)
	}
}

func TestPalette(t *testing.T) {
	if fuzzy("hk", "Hollow Knight") < 0 || fuzzy("zz", "Hollow Knight") >= 0 {
		t.Fatal("fuzzy basics")
	}
	if fuzzy("hol", "Hollow Knight") <= fuzzy("hol", "The Whole Thing") {
		t.Fatal("prefix should win")
	}
	b := featureBrowser(120, 40)
	b.handleKey("ctrl+k")
	if !b.palette.open {
		t.Fatal("ctrl+k opens the palette")
	}
	for _, c := range "hades" {
		b.handleKey(string(c))
	}
	if len(b.palette.results) == 0 || b.palette.results[0].title != "Hades" {
		t.Fatalf("first result: %+v", b.palette.results[:1])
	}
	checkWidth(t, "palette", b)
	b.handleKey("enter")
	if b.palette.open || b.mode != modePlay {
		t.Fatalf("enter opens the game, mode %v", b.mode)
	}
	// verbs
	b.handleKey(":")
	for _, c := range "store lantern" {
		k := string(c)
		if c == ' ' {
			k = "space"
		}
		b.handleKey(k)
	}
	if b.palette.results[0].kind != "STORE" {
		t.Fatalf("store verb: %+v", b.palette.results[0])
	}
	b.handleKey("enter")
	if b.mode != modeStore || b.store.tab != tabSearch || b.store.wantSearch != "lantern" {
		t.Fatalf("store search: mode %v tab %d want %q", b.mode, b.store.tab, b.store.wantSearch)
	}
	b.handleKey(":")
	for _, c := range "theme aur" {
		k := string(c)
		if c == ' ' {
			k = "space"
		}
		b.handleKey(k)
	}
	b.handleKey("enter")
	if active.Name != "aurora" {
		t.Fatalf("theme verb: %s", active.Name)
	}
	_ = SetTheme("steam")
}

func TestStore(t *testing.T) {
	for _, w := range []int{80, 120, 170} {
		b := featureBrowser(w, 42)
		b.handleKey("s")
		if b.mode != modeStore || !b.store.wantFront {
			t.Fatal("s opens the store and loads it")
		}
		checkWidth(t, "store-loading", b)
		lists, _ := b.opt.LoadStoreFront()
		b.store.loading, b.store.loaded, b.store.lists, b.store.wantFront = false, true, lists, false
		checkWidth(t, "store", b)
		info, _ := b.opt.LoadStore(9001)
		b.store.details[9001] = &info
		checkWidth(t, "store-detail", b)
		b.handleKey("c")
		if len(b.store.cart) != 1 || b.store.cart[0].AppID != 9001 {
			t.Fatalf("cart: %+v", b.store.cart)
		}
		b.handleKey("end") // ELDEN RING, owned
		b.handleKey("c")
		if len(b.store.cart) != 1 {
			t.Fatal("owned games do not go in the cart")
		}
		b.handleKey("b")
		if !strings.Contains(b.status, "already own") {
			t.Fatalf("buy owned: %q", b.status)
		}
		b.handleKey("6")
		checkWidth(t, "cart", b)
		if b.storeItems()[0].Name != "Lanternfall" {
			t.Fatal("cart tab lists the cart")
		}
		b.handleKey("x")
		if len(b.store.cart) != 0 {
			t.Fatal("x removes")
		}
		b.handleKey("/")
		for _, c := range "moss" {
			b.handleKey(string(c))
		}
		if b.store.query != "moss" || b.mode != modeStore {
			t.Fatal("typing goes into the search")
		}
		checkWidth(t, "store-typing", b)
		b.handleKey("enter")
		if b.store.wantSearch != "moss" {
			t.Fatal("enter searches")
		}
		b.handleKey("enter") // toggle detail on an empty result list must not panic
		b.handleKey("esc")
		if b.mode != modeHome {
			t.Fatalf("esc returns home, got %v", b.mode)
		}
	}
}

func TestProfileWrappedRoulette(t *testing.T) {
	for _, w := range []int{80, 120, 170} {
		b := featureBrowser(w, 44)
		b.handleKey("m")
		if b.mode != modeProfile || !b.profile.want {
			t.Fatal("m opens the profile")
		}
		checkWidth(t, "profile-loading", b)
		p, _ := b.opt.LoadProfile()
		b.profile.p, b.profile.loaded, b.profile.loading, b.profile.want = p, true, false, false
		checkWidth(t, "profile", b)

		b.handleKey("y")
		if b.mode != modeWrapped {
			t.Fatal("y opens wrapped")
		}
		for i := 0; i < wrappedSlides+2; i++ {
			checkWidth(t, "wrapped", b)
			b.handleKey("right")
		}
		if b.wrapped.slide != wrappedSlides-1 {
			t.Fatalf("slide %d", b.wrapped.slide)
		}
		b.handleKey("esc")
		b.handleKey("esc")
		if b.mode != modeHome {
			t.Fatalf("back home, got %v", b.mode)
		}

		b.handleKey("r")
		if b.mode != modeRoulette || len(b.roulette.pool) == 0 {
			t.Fatal("r spins the roulette")
		}
		b.roulette.start = time.Now().Add(-spinFor - time.Second) // landed
		checkWidth(t, "roulette", b)
		want := b.roulette.pool[b.roulette.target].AppID
		if got := b.roulette.reelPos(time.Now()); int(got)%len(b.roulette.pool) != b.roulette.target {
			t.Fatalf("reel stops on the target: %v", got)
		}
		b.handleKey("enter")
		if b.mode != modePlay || b.play.appID != want {
			t.Fatal("enter opens the picked game")
		}
	}
}

func TestWrappedCompute(t *testing.T) {
	y := time.Now().Year()
	at := func(m time.Month, d, h int) int64 { return time.Date(y, m, d, h, 0, 0, 0, time.Local).Unix() }
	games := []model.Game{
		{AppID: 1, Name: "A", HasStats: true, Total: 3, Achieved: 3, Unlocks: []int64{at(1, 1, 2), at(1, 2, 2), at(1, 3, 1)}},
		{AppID: 2, Name: "B", HasStats: true, Total: 10, Achieved: 1, Unlocks: []int64{at(1, 5, 23), time.Date(y-1, 5, 5, 0, 0, 0, 0, time.Local).Unix()}},
	}
	cache := map[int][]model.Achievement{1: {{Name: "x", Achieved: true, Global: 2.5, UnlockTime: at(1, 1, 2)}}}
	w := ComputeWrapped(games, y, cache)
	if w.Unlocked != 4 || w.Streak != 3 || w.TopGame.AppID != 1 || len(w.Perfect) != 1 || w.Rarest == nil || w.BestMonth != 0 {
		t.Fatalf("%+v", w)
	}
	if w.Archetype != "THE NIGHT OWL" {
		t.Fatalf("archetype %s", w.Archetype)
	}
	svg := WrappedSVG(w, "tom & jerry")
	if !strings.Contains(svg, "tom &amp; jerry") || !strings.HasPrefix(svg, "<svg") {
		t.Fatal("svg escapes text")
	}
}

func TestBigText(t *testing.T) {
	rows := bigText("Lv 42!", "#000000", "#ffffff", 1)
	if len(rows) != 3 || width(rows[0]) != bigTextWidth("Lv 42!") {
		t.Fatalf("big text: %d rows, %d wide, want %d", len(rows), width(rows[0]), bigTextWidth("Lv 42!"))
	}
	if got := fitBigText("a very long name indeed", 20); bigTextWidth(got) > 20 {
		t.Fatal("fit")
	}
}

func TestBigPictureHome(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 34}, {170, 46}} {
		b := featureBrowser(size[0], size[1])
		checkWidth(t, "home", b)
		g0, _ := b.homeFocus()
		b.handleKey("right")
		g1, _ := b.homeFocus()
		if g0.AppID == g1.AppID {
			t.Fatal("right moves along the shelf and the hero follows")
		}
		b.handleKey("down")
		if b.home.col != 1 {
			t.Fatalf("down goes to the next shelf: %d", b.home.col)
		}
		checkWidth(t, "home-shelf2", b)
		b.handleKey("down")
		if b.home.col != 2 {
			t.Fatal("friends shelf")
		}
		checkWidth(t, "home-friends", b)
		b.handleKey("up")
		b.handleKey("up")
		b.handleKey("up")
		if !b.home.nav {
			t.Fatal("up from the first shelf reaches the top bar")
		}
		b.handleKey("right")
		b.handleKey("right") // STORE
		checkWidth(t, "home-nav", b)
		b.handleKey("enter")
		if b.mode != modeStore {
			t.Fatalf("enter on STORE opens the store, got %v", b.mode)
		}
		b.handleKey("esc")
		if b.mode != modeHome || b.home.nav {
			t.Fatal("back home with focus on the shelves")
		}
	}
}

func TestPosterAndTiles(t *testing.T) {
	for _, l := range poster("Hollow Knight Silksong", 7, 40, 10, 1) {
		if width(l) != 40 {
			t.Fatalf("poster row %d wide", width(l))
		}
	}
	b := featureBrowser(120, 34)
	for _, l := range b.gameTile(b.games[0], tileW-2, true) {
		if width(l) != tileW-2 {
			t.Fatalf("tile row %d wide", width(l))
		}
	}
}

func TestChallengeAndForecast(t *testing.T) {
	b := featureBrowser(120, 34)
	g1, a1, ok := b.challenge()
	g2, a2, _ := b.challenge()
	if !ok || g1.AppID != g2.AppID || a1.Name != a2.Name || a1.Global < 10 || a1.Global > 80 {
		t.Fatalf("challenge: %v %+v", ok, a1)
	}
	b.handleKey("c")
	if b.mode != modePlay || b.play.appID != g1.AppID {
		t.Fatal("c opens the challenge's game")
	}
	now := time.Now()
	g := model.Game{HasStats: true, Total: 20, Achieved: 10}
	for i := 0; i < 10; i++ {
		g.Unlocks = append(g.Unlocks, now.AddDate(0, 0, -i*6).Unix()) // 10 in 60 days
	}
	if fc := forecast(g); !strings.Contains(fc, "8 weeks") {
		t.Fatalf("forecast: %q", fc)
	}
	g.Unlocks = []int64{now.AddDate(-1, 0, 0).Unix()}
	if fc := forecast(g); !strings.Contains(fc, "no unlocks") {
		t.Fatalf("stale forecast: %q", fc)
	}
}

func TestVersus(t *testing.T) {
	b := featureBrowser(150, 40)
	b.opt.LoadVersus = func(f model.Friend) (Versus, error) {
		return Versus{Games: []VersusGame{
			{AppID: 300, Name: "ELDEN RING", Mine: 6000, Theirs: 3000, MyPct: 80, TheirPct: 40},
			{AppID: 301, Name: "Hades", Mine: 100, Theirs: 9000, MyPct: 10, TheirPct: 90},
			{AppID: 302, Name: "Hollow Knight", Mine: 50, Theirs: 50, MyPct: -1, TheirPct: 20},
		}}, nil
	}
	b.handleKey("f")
	b.handleKey("v")
	if b.mode != modeVersus || !b.versus.want {
		t.Fatal("v compares with the selected friend")
	}
	checkWidth(t, "versus-loading", b)
	d, _ := b.opt.LoadVersus(b.versus.friend)
	b.versus.data, b.versus.loading, b.versus.want = d, false, false
	if m, th, _, _ := d.score(); m != 1 || th != 1 {
		t.Fatalf("score %d %d", m, th)
	}
	checkWidth(t, "versus", b)
	b.handleKey("down")
	b.handleKey("enter")
	if b.mode != modePlay || b.play.appID != 301 {
		t.Fatal("enter opens the shared game")
	}
	for _, w := range []int{80, 120} {
		b.w = w
		b.mode = modeVersus
		checkWidth(t, "versus-narrow", b)
	}
}
