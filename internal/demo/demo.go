// Package demo provides fictional data so anyone can try the CLI without a
// Steam API key. It is always clearly labelled as a simulation.
package demo

import (
	"fmt"
	"time"

	"github.com/tomikav/steam-achievements-cli-/internal/model"
)

// Games returns a small fictional library covering every completion tier.
func Games() []model.Game {
	mk := func(id int, name string, minutes, achieved, total, lastPlayed int64) model.Game {
		g := model.Game{
			AppID:      int(id),
			Name:       name,
			Playtime:   int(minutes),
			Achieved:   int(achieved),
			Total:      int(total),
			LastPlayed: lastPlayed,
			HasStats:   true,
		}
		if total > 0 {
			g.Percent = float64(achieved) / float64(total) * 100
		}
		return g
	}
	games := []model.Game{
		mk(101, "Neon Overdrive", 41200, 62, 62, 1737600000),
		mk(102, "Galactic Cheese Simulator", 220, 1, 8, 1700000000),
		mk(103, "Soulsborne: Laundry Edition", 38900, 51, 63, 1737000000),
		mk(104, "Pixel Farmer Deluxe", 8600, 32, 35, 1732000000),
		mk(105, "Rustbelt Rally", 19700, 28, 46, 1735000000),
		mk(106, "Deep Sea Diplomacy", 5400, 21, 24, 1729000000),
		mk(107, "Tower of Regret", 30500, 79, 93, 1738000000),
		mk(108, "Captain Cactus", 1600, 2, 15, 1690000000),
		mk(109, "Quantum Bakery", 12300, 37, 52, 1734000000),
		mk(110, "Void Cartographer", 27800, 55, 60, 1737500000),
		mk(111, "Mecha Gardener", 9100, 18, 40, 1731000000),
		mk(112, "Bardcore Rhythm Saga", 51200, 96, 96, 1737200000),
		mk(113, "Opossum Racing League", 3400, 9, 51, 1725000000),
		mk(114, "Catacomb Bookkeeper", 14300, 45, 58, 1736000000),
		mk(115, "Skyfisherman", 6700, 25, 33, 1730000000),
		mk(116, "Grand Theft Sandwich", 5400, 11, 43, 1728000000),
		mk(117, "Chrono Librarian", 26900, 48, 70, 1736500000),
		mk(118, "Ultra Drama Engine", 8800, 15, 66, 1731500000),
		mk(119, "Space Taxidermist", 20100, 30, 44, 1732500000),
		mk(120, "Legend of the Lost Sock", 15400, 64, 74, 1735500000),
		mk(121, "Birdlaw Attorney", 3500, 7, 32, 1726000000),
		mk(122, "Molasses Runner", 11400, 41, 55, 1734500000),
	}
	// Unlock history and locked lists, so the activity map and the hunt
	// view have something to show. Spread over the last year, denser
	// recently, with a short streak ending today.
	now := time.Now()
	seed := uint32(7)
	rnd := func(n int) int {
		seed = seed*1664525 + 1013904223
		return int(seed>>8) % n
	}
	shift := now.Unix() - 3*3600 - 1738000000 // the latest game was played today
	for i := range games {
		g := &games[i]
		g.LastPlayed += shift
		for k := 0; k < g.Achieved; k++ {
			days := rnd(365)
			if rnd(3) == 0 {
				days = rnd(60)
			}
			g.Unlocks = append(g.Unlocks, now.AddDate(0, 0, -days).Unix())
		}
		for _, a := range Achievements(*g) {
			if !a.Achieved {
				g.Locked = append(g.Locked, a)
			}
		}
	}
	for d := 0; d < 6; d++ {
		games[d%len(games)].Unlocks = append(games[d%len(games)].Unlocks, now.AddDate(0, 0, -d).Unix())
	}
	return games
}

// Deals returns a fictional wishlist and store specials.
func Deals() []model.Deal {
	return []model.Deal{
		{AppID: 201, Name: "Hollow Lantern", Discount: 75, Final: "$4.99", Original: "$19.99", Wishlist: true, Available: true},
		{AppID: 202, Name: "Starfall Tactics", Discount: 50, Final: "$14.99", Original: "$29.99", Wishlist: true, Available: true},
		{AppID: 203, Name: "Moss Kingdom", Final: "$24.99", Wishlist: true, Available: true},
		{AppID: 204, Name: "Rail Baron 2", Final: "$9.99", Wishlist: true, Available: true},
		{AppID: 205, Name: "Echo Divers", Discount: 90, Final: "$1.99", Original: "$19.99", Available: true},
		{AppID: 206, Name: "Pocket Armada", Discount: 60, Final: "$7.99", Original: "$19.99", Available: true},
		{AppID: 207, Name: "Kiln & Kettle", Discount: 35, Final: "$12.99", Original: "$19.99", Available: true},
	}
}

// News returns fictional news for the demo library.
func News() []model.News {
	now := time.Now()
	day := func(d int) int64 { return now.AddDate(0, 0, -d).Unix() }
	return []model.News{
		{AppID: 107, Game: "Tower of Regret", Title: "Patch 1.8: the Regret Floor is now optional", Date: day(1), Feed: "Community Announcements",
			Summary: "Balance changes to the late floors, a new photo mode and fixes for the save bug some players hit after the last update."},
		{AppID: 103, Game: "Soulsborne: Laundry Edition", Title: "Free DLC: The Lint Trap", Date: day(3), Feed: "Community Announcements",
			Summary: "A new area with four bosses and six new achievements is available today for every owner of the game."},
		{AppID: 110, Game: "Void Cartographer", Title: "Winter roadmap", Date: day(6), Feed: "Developer blog",
			Summary: "What the team is working on until March: co-op mapping, controller remapping and Steam Deck verification."},
		{AppID: 101, Game: "Neon Overdrive", Title: "Speedrun event this weekend", Date: day(9), Feed: "Community Announcements",
			Summary: "Join the community race on Saturday. The three fastest runs get their ghost added to the game."},
	}
}

// Install is one fictional installed game for the storage view.
type Install struct {
	AppID int
	Name  string
	Size  int64
}

// Installed returns fictional installed games.
func Installed() []Install {
	gb := int64(1 << 30)
	return []Install{
		{101, "Neon Overdrive", 38 * gb}, {103, "Soulsborne: Laundry Edition", 61 * gb}, {107, "Tower of Regret", 22 * gb},
		{108, "Captain Cactus", 3 * gb}, {113, "Opossum Racing League", 14 * gb}, {121, "Birdlaw Attorney", gb / 2},
	}
}

// Achievements returns a deterministic fictional achievement list for a game.
func Achievements(g model.Game) []model.Achievement {
	prefixes := []string{"Complete Chapter", "Find All", "Master the", "Speedrun", "Beat the Boss:", "Collect 100", "Unlock Every", "Witness the"}
	suffixes := []string{"Intro", "Secrets", "Lore", "Any%", "Final Form", "Crumbs", "Endings", "Sunrise"}
	n := g.Total
	if n <= 0 {
		n = 10
	}
	out := make([]model.Achievement, 0, n)
	for i := 0; i < n; i++ {
		// Deterministic pseudo pattern: unlocks cluster in the first chunk.
		achieved := i < g.Achieved
		global := float64(85-i*3) / 1.0
		if global < 2 {
			global = 2 + float64((i*7)%11)
		}
		out = append(out, model.Achievement{
			Name:        fmt.Sprintf("ACH_%03d", i+1),
			DisplayName: fmt.Sprintf("%s %s %d", prefixes[i%len(prefixes)], suffixes[(i*3)%len(suffixes)], i+1),
			Achieved:    achieved,
			Global:      global,
			Description: descriptions[i%len(descriptions)],
			UnlockTime:  unlockTime(achieved, i),
		})
	}
	return out
}

var descriptions = []string{
	"Finish the opening chapter without losing a single life.",
	"Discover every hidden room in the old observatory.",
	"Defeat the final boss using only the starting weapon.",
	"Collect all 100 crumbs scattered across the kingdom.",
	"Complete the game on the hardest difficulty.",
	"Watch the sunrise from the top of the tallest tower.",
}

func unlockTime(achieved bool, i int) int64 {
	if !achieved {
		return 0
	}
	return 1735000000 - int64(i)*86400*3
}

// Friends returns a fictional friends list for the demo browser.
func Friends() []model.Friend {
	return []model.Friend{
		{SteamID: "1", Name: "nightowl", State: 1, Game: "Neon Overdrive"},
		{SteamID: "2", Name: "pixel_witch", State: 1},
		{SteamID: "3", Name: "Captain Cactus", State: 3},
		{SteamID: "4", Name: "speedrun_sam", State: 1, Game: "Tower of Regret"},
		{SteamID: "5", Name: "laundrylord", State: 0, LastLogoff: 1731000000},
		{SteamID: "6", Name: "opossum", State: 0, LastLogoff: 1700000000},
		{SteamID: "7", Name: "bard_core", State: 2},
	}
}

// Store returns fictional store details for the demo game view.
func Store(appID int) model.StoreInfo {
	for _, g := range Games() {
		if g.AppID == appID {
			return model.StoreInfo{
				Name:        g.Name,
				Description: "A fictional game from the demo library. In your real library this space shows the store description, the header art and where the game is installed.",
				Developers:  []string{"Demo Studio"},
				Publishers:  []string{"Demo Publishing"},
				Genres:      []string{"Action", "Adventure", "Indie"},
				Released:    "14 Feb, 2024",
				Metacritic:  70 + appID%25,
				Platforms:   []string{"windows", "linux"},
			}
		}
	}
	return model.StoreInfo{}
}
