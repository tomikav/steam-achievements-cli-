package ui

import (
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/tomikav/steam-achievements-cli-/internal/model"
)

func TestParseInput(t *testing.T) {
	in := []byte("j\x1b[A\x1b[B\x1b[5~\x1b[Z\r\x7f/\x1b[<64;10;5M\x1b[<0;3;7M\x1b[<0;3;7m\x03é")
	evs, used := parseInput(in)
	if used != len(in) {
		t.Fatalf("used %d of %d", used, len(in))
	}
	var got []string
	for _, e := range evs {
		got = append(got, e.key)
	}
	want := "j up down pgup shift+tab enter backspace / wheelup click ctrl+c é"
	if strings.Join(got, " ") != want {
		t.Fatalf("got  %q\nwant %q", strings.Join(got, " "), want)
	}
	if evs[9].row != 7 {
		t.Errorf("click row = %d", evs[9].row)
	}
}

func TestParseInputIncomplete(t *testing.T) {
	evs, used := parseInput([]byte("a\x1b[1"))
	if len(evs) != 1 || used != 1 {
		t.Fatalf("evs=%v used=%d", evs, used)
	}
	evs, _ = parseInput([]byte("\x1b"))
	if len(evs) != 1 || evs[0].key != "esc" {
		t.Fatalf("lone escape: %v", evs)
	}
}

func testGames() []model.Game {
	mk := func(id int, name string, a, total, play int) model.Game {
		return model.Game{AppID: id, Name: name, Achieved: a, Total: total, Playtime: play, HasStats: true, LastPlayed: int64(id)}
	}
	return []model.Game{
		mk(1, "Alpha", 10, 10, 50),
		mk(2, "Bravo", 9, 10, 500),
		mk(3, "Charlie", 0, 20, 5),
		mk(4, "Delta", 5, 10, 100),
		mk(5, "Echo Knight", 19, 20, 10),
		{AppID: 6, Name: "No Stats"},
	}
}

func names(b *browser) string {
	var n []string
	for _, i := range b.view {
		n = append(n, b.games[i].Name)
	}
	return strings.Join(n, ",")
}

func TestBrowserTabsSearchSort(t *testing.T) {
	b := newListBrowser(BrowseOptions{Games: testGames()}, 120, 30)
	if names(b) != "Alpha,Echo Knight,Bravo,Delta,Charlie,No Stats" {
		t.Fatalf("default order: %s", names(b))
	}
	b.handleKey("3") // ALMOST: 75..99
	if names(b) != "Echo Knight,Bravo" {
		t.Fatalf("almost tab: %s", names(b))
	}
	b.handleKey("4")
	if names(b) != "Alpha" {
		t.Fatalf("perfect tab: %s", names(b))
	}
	b.handleKey("5")
	if names(b) != "Charlie" {
		t.Fatalf("not started tab: %s", names(b))
	}
	b.handleKey("1")
	for _, k := range []string{"/", "k", "n", "i"} {
		b.handleKey(k)
	}
	if names(b) != "Echo Knight" || !b.search {
		t.Fatalf("search: %s", names(b))
	}
	b.handleKey("esc")
	if b.query != "" || len(b.view) != 6 {
		t.Fatalf("esc should clear the search")
	}
	b.handleKey("s") // remaining
	if names(b) != "Echo Knight,Bravo,Delta,Charlie,Alpha,No Stats" {
		t.Fatalf("remaining sort: %s", names(b))
	}
	b.handleKey("s") // playtime
	b.handleKey("r")
	if names(b) != "No Stats,Charlie,Echo Knight,Alpha,Delta,Bravo" {
		t.Fatalf("reversed playtime: %s", names(b))
	}
}

func TestBrowserKeepsSelectionAcrossSort(t *testing.T) {
	b := newListBrowser(BrowseOptions{Games: testGames()}, 120, 30)
	b.handleKey("down")
	b.handleKey("down") // Bravo
	g, _ := b.selected()
	b.handleKey("s")
	if g2, _ := b.selected(); g2.AppID != g.AppID {
		t.Fatalf("selection changed from %s to %s", g.Name, g2.Name)
	}
}

func TestBrowserScrolling(t *testing.T) {
	var games []model.Game
	for i := 0; i < 100; i++ {
		games = append(games, model.Game{AppID: i + 1, Name: "Game", Achieved: i % 10, Total: 10, HasStats: true})
	}
	b := newListBrowser(BrowseOptions{Games: games}, 120, 20) // 14 visible rows
	for i := 0; i < 30; i++ {
		b.handleKey("down")
	}
	if b.cursor != 30 || b.offset != 30-b.listHeight()+1 {
		t.Fatalf("cursor=%d offset=%d", b.cursor, b.offset)
	}
	b.handleKey("end")
	if b.cursor != 99 {
		t.Fatalf("end: %d", b.cursor)
	}
	b.handleKey("home")
	if b.cursor != 0 || b.offset != 0 {
		t.Fatalf("home: %d %d", b.cursor, b.offset)
	}
}

func TestBrowserAchievements(t *testing.T) {
	b := newListBrowser(BrowseOptions{Games: testGames()}, 120, 30)
	b.handleKey("enter")
	if b.mode != modePlay || !b.ach.loading || b.wantAch != 1 {
		t.Fatalf("enter should open the game view and request achievements: mode=%v want=%d", b.mode, b.wantAch)
	}
	b.setAchievements(1, []model.Achievement{
		{Name: "a", DisplayName: "Easy locked", Global: 80},
		{Name: "b", DisplayName: "Hard locked", Global: 2},
		{Name: "c", DisplayName: "Common", Achieved: true, Global: 90},
		{Name: "d", DisplayName: "Rare", Achieved: true, Global: 1},
	}, nil)
	var got []string
	for _, i := range b.ach.view {
		got = append(got, b.ach.list[i].DisplayName)
	}
	if strings.Join(got, ",") != "Easy locked,Hard locked,Rare,Common" {
		t.Fatalf("smart order: %v", got)
	}
	b.handleKey("right") // locked, from the game view
	if len(b.ach.view) != 2 {
		t.Fatalf("locked tab: %d", len(b.ach.view))
	}
	b.handleKey("enter")
	if b.mode != modeGame {
		t.Fatal("enter in the game view should open the full list")
	}
	b.handleKey("esc")
	if b.mode != modePlay {
		t.Fatal("esc should return to the game view")
	}
	b.handleKey("esc")
	if b.mode != modeList {
		t.Fatal("esc should go back to the library")
	}
	b.handleKey("enter")
	if b.ach.loading {
		t.Fatal("second open should use the cache")
	}
}

// TestSnapshots writes ANSI renderings of each screen when SNAPSHOT_DIR is
// set, for visual review. It always checks that every row fits the width.
func TestSnapshots(t *testing.T) {
	games := []model.Game{}
	names := []string{"The Elder Scrolls V: Skyrim Special Edition", "ELDEN RING", "Marvel's Spider-Man 2", "Apex Legends", "TEKKEN 8",
		"ARC Raiders", "DARK SOULS III", "Terraria", "Brawlhalla", "Cyberpunk 2077", "Hotline Miami", "Subnautica: Below Zero",
		"The Forest", "Enter the Gungeon", "HELLDIVERS 2", "High On Life", "Hollow Knight: Silksong", "Stardew Valley", "Portal", "Hades"}
	for i, n := range names {
		total := 20 + i*3
		games = append(games, model.Game{AppID: 100 + i, Name: n, Total: total, Achieved: total * (100 - i*5) / 100,
			Playtime: 3000 + i*700, LastPlayed: 1790000000 - int64(i)*86400*9, HasStats: true})
	}
	games[19].Achieved = 0
	for _, w := range []int{150, 96} {
		hb := newBrowser(BrowseOptions{Games: games, Owner: "tomika"}, w, 40)
		hb.setFriends([]model.Friend{{SteamID: "1", Name: "nightowl", State: 1, Game: "Neon Overdrive"}, {SteamID: "2", Name: "pixel_witch", State: 1}, {SteamID: "3", Name: "Captain Cactus", State: 3}}, nil)
		b := newListBrowser(BrowseOptions{Games: games, Owner: "tomika"}, w, 32)
		shots := map[string]frame{"list": b.build(), "home": hb.build()}
		b.handleKey("down")
		b.handleKey("enter")
		var ach []model.Achievement
		for i := 0; i < 30; i++ {
			ach = append(ach, model.Achievement{Name: string(rune('a' + i)), DisplayName: []string{"Master of the Lost Sock", "Speedrun the Crumbs", "Beat the Final Form", "Find Every Secret"}[i%4] + " " + string(rune('A'+i%26)),
				Description: "Defeat the final boss using only the starting weapon, without taking damage.", Achieved: i%3 != 0, Global: float64((i*37)%97) + 0.4, UnlockTime: 1780000000 - int64(i)*86400})
		}
		b.setAchievements(b.ach.appID, ach, nil)
		b.handleKey("enter")
		shots["game"] = b.build()
		b.handleKey("esc")
		shots["gameview"] = b.build()
		b.handleKey("esc")
		b.handleKey("d")
		shots["dash"] = b.build()
		b.handleKey("esc")
		b.handleKey("?")
		shots["help"] = b.build()
		b.handleKey("esc")
		b.handleKey("f")
		b.setFriends([]model.Friend{{SteamID: "1", Name: "nightowl", State: 1, Game: "Neon Overdrive"}, {SteamID: "2", Name: "pixel_witch", State: 1},
			{SteamID: "3", Name: "Captain Cactus", State: 3}, {SteamID: "5", Name: "laundrylord", LastLogoff: 1731000000}}, nil)
		shots["friends"] = b.build()
		b.handleKey("esc")
		b.handleKey("p")
		b.play.store = &model.StoreInfo{Name: "ELDEN RING", Description: "THE NEW FANTASY ACTION RPG. Rise, Tarnished, and be guided by grace to brandish the power of the Elden Ring and become an Elden Lord in the Lands Between.",
			Developers: []string{"FromSoftware, Inc."}, Publishers: []string{"FromSoftware, Inc.", "Bandai Namco Entertainment"}, Genres: []string{"Action", "RPG"},
			Released: "24 Feb, 2022", Metacritic: 94, Platforms: []string{"windows"}}
		b.play.installKnown, b.play.install.Installed, b.play.install.Dir, b.play.install.SizeBytes = true, true, `D:\SteamLibrary\steamapps\common\ELDEN RING`, 60<<30
		b.play.running, b.play.startedAt = true, time.Now().Add(-37*time.Minute)
		b.play.session = []model.Achievement{{DisplayName: "Elden Lord"}}
		shots["play"] = b.build()

		for name, f := range shots {
			lines := flatten(f, w)
			for i, l := range lines {
				if width(l) > w {
					t.Errorf("%s@%d row %d is %d wide", name, w, i, width(l))
				}
			}
			if dir := os.Getenv("SNAPSHOT_DIR"); dir != "" {
				_ = os.WriteFile(dir+"/"+name+"_"+itoa(w)+".txt", []byte(strings.Join(lines, "\n")), 0o644)
			}
		}
	}
}

func itoa(i int) string { return strings.TrimSpace(strings.Repeat(" ", 0) + fmtInt(i)) }

func fmtInt(i int) string {
	s := ""
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	return s
}

// flatten places a frame's segments into plain rows (icons are ignored).
func flatten(f frame, w int) []string {
	out := make([]string, len(f.rows))
	for r, segs := range f.rows {
		sort.SliceStable(segs, func(i, j int) bool { return segs[i].col < segs[j].col })
		line := ""
		for _, s := range segs {
			if pad := s.col - width(line); pad > 0 {
				line += strings.Repeat(" ", pad)
			}
			line += s.text
		}
		out[r] = line
	}
	return out
}

func TestFriendsOrder(t *testing.T) {
	b := newListBrowser(BrowseOptions{Games: testGames()}, 120, 30)
	b.handleKey("f")
	if b.mode != modeFriends || !b.wantFriends {
		t.Fatal("f should open friends and request them")
	}
	b.setFriends([]model.Friend{
		{SteamID: "1", Name: "zed", State: 0, LastLogoff: 5},
		{SteamID: "2", Name: "amy", State: 1},
		{SteamID: "3", Name: "bob", State: 1, Game: "Hades"},
		{SteamID: "4", Name: "cat", State: 3},
		{SteamID: "5", Name: "dan", State: 0, LastLogoff: 9},
	}, nil)
	var got []string
	for _, f := range b.friends.list {
		got = append(got, f.Name)
	}
	if strings.Join(got, ",") != "bob,amy,cat,dan,zed" {
		t.Fatalf("order: %v", got)
	}
	b.handleKey("esc")
	if b.mode != modeList {
		t.Fatal("esc should return to the list")
	}
}

func TestPlayView(t *testing.T) {
	b := newListBrowser(BrowseOptions{Games: testGames()}, 140, 34)
	b.handleKey("p")
	if b.mode != modePlay || b.play.appID != 1 || b.wantStore != 1 || b.wantInstall != 1 || b.wantAch != 1 {
		t.Fatalf("p should open the game view and request its data: %+v", b.play)
	}
	base := []model.Achievement{{Name: "a", Achieved: true}, {Name: "b"}, {Name: "c"}}
	b.applyRefresh(1, base)
	b.setAchievements(1, base, nil)
	if len(b.play.session) != 0 {
		t.Fatal("the first read is the baseline, not new unlocks")
	}
	b.updateRunning(1)
	if !b.play.running {
		t.Fatal("running id should mark the game as running")
	}
	next := []model.Achievement{{Name: "a", Achieved: true}, {Name: "b", DisplayName: "Big One", Achieved: true}, {Name: "c"}}
	b.applyRefresh(1, next)
	b.setAchievements(1, next, nil)
	if len(b.play.session) != 1 || !strings.Contains(b.status, "Big One") {
		t.Fatalf("new unlock not detected: %v %q", b.play.session, b.status)
	}
	if g, _ := b.playGame(); g.Achieved != 2 || g.Total != 3 {
		t.Fatalf("game counts not updated: %+v", g)
	}
	b.updateRunning(0)
	if b.play.running || b.wantAch != 1 {
		t.Fatal("stopping should trigger a final refresh")
	}
	b.handleKey("c")
	if b.wantClose != 0 {
		t.Fatal("closing a stopped game must not do anything")
	}
	b.handleKey("enter")
	if b.mode != modeGame {
		t.Fatal("enter should open the full achievement list")
	}
	b.handleKey("esc")
	if b.mode != modePlay {
		t.Fatal("esc from the achievement list should return to the game view")
	}
}

func TestCellSizeReply(t *testing.T) {
	evs, _ := parseInput([]byte("\x1b[6;20;10t"))
	if len(evs) != 1 || evs[0].key != "cellsize" || evs[0].row != 20 || evs[0].col != 10 {
		t.Fatalf("%+v", evs)
	}
}

func TestExtrasHidden(t *testing.T) {
	games := append(testGames(),
		model.Game{AppID: 90, Name: "Deadlock"},
		model.Game{AppID: 91, Name: "Cool Game Demo"},
		model.Game{AppID: 92, Name: "Some Game Dedicated Server"},
		model.Game{AppID: 93, Name: "Some Game Soundtrack"})
	b := newListBrowser(BrowseOptions{Games: games}, 120, 30)
	n := names(b)
	if !strings.Contains(n, "Deadlock") || strings.Contains(n, "Demo") || strings.Contains(n, "Server") || strings.Contains(n, "Soundtrack") {
		t.Fatalf("filter: %s", n)
	}
	b.handleKey("6")
	if names(b) != "Deadlock,No Stats" && names(b) != "No Stats,Deadlock" {
		t.Fatalf("no-achievements tab: %s", names(b))
	}
}

// newListBrowser starts in the library instead of the home screen.
func newListBrowser(opt BrowseOptions, w, h int) *browser {
	b := newBrowser(opt, w, h)
	b.mode = modeList
	return b
}

func TestHome(t *testing.T) {
	games := testGames()
	for i := range games {
		games[i].LastPlayed = int64(1000 + i)
	}
	b := newBrowser(BrowseOptions{Games: games, LoadFriends: func() ([]model.Friend, error) { return nil, nil }}, 150, 40)
	if b.mode != modeHome || !b.wantFriends {
		t.Fatal("should start on the home screen and load friends")
	}
	recent := b.homeGames(0)
	if recent[0].Name != "No Stats" {
		t.Fatalf("continue playing order: %v", recent[0].Name)
	}
	almost := b.homeGames(1)
	if almost[0].Name != "Echo Knight" || len(almost) != 3 {
		t.Fatalf("almost there: %v", almost)
	}
	b.handleKey("right")
	b.handleKey("enter")
	if b.mode != modePlay || b.play.appID != 5 {
		t.Fatalf("enter on the almost card should open Echo Knight, got mode %v app %d", b.mode, b.play.appID)
	}
	b.handleKey("esc")
	if b.mode != modeHome {
		t.Fatal("esc should go back home")
	}
	b.handleKey("l")
	if b.mode != modeList {
		t.Fatal("l should open the library")
	}
	b.handleKey("esc")
	if b.mode != modeHome {
		t.Fatal("esc in the library goes home")
	}
	b.setFriends([]model.Friend{{SteamID: "1", Name: "a", State: 1}, {SteamID: "2", Name: "b"}}, nil)
	if len(b.homeFriends()) != 1 {
		t.Fatal("only online friends on the home card")
	}
}

// TestBoxesAligned checks that every box's corners line up with its sides:
// all rows of a panel must end in the same column.
func TestBoxesAligned(t *testing.T) {
	b := newBrowser(BrowseOptions{Games: testGames(), Owner: "x"}, 150, 40)
	check := func(name string) {
		lines := flatten(b.build(), 150)
		for i, l := range lines {
			plain := []rune(stripCodes(l))
			for j, r := range plain {
				if r == '╮' || r == '╯' || r == '┤' {
					// the character above or below must be a vertical border
					// in the same column
					for _, k := range []int{i - 1, i + 1} {
						if k < 0 || k >= len(lines) {
							continue
						}
						other := []rune(stripCodes(lines[k]))
						if j < len(other) && (other[j] == '│' || other[j] == '╮' || other[j] == '╯' || other[j] == '┤') {
							goto ok
						}
					}
					t.Errorf("%s: corner %q at row %d col %d has no matching side", name, r, i, j)
				ok:
				}
			}
		}
	}
	check("home")
	b.mode = modeList
	check("list")
	b.openPlay(1)
	b.setAchievements(1, []model.Achievement{{Name: "a", DisplayName: "A", Global: 3}}, nil)
	check("play")
}

func stripCodes(s string) string {
	var out []rune
	in := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			in = true
		case in && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'):
			in = false
		case !in:
			out = append(out, r)
		}
	}
	return string(out)
}
