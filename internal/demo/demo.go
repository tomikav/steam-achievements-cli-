// Package demo provides fictional data so anyone can try the CLI without a
// Steam API key. It is always clearly labelled as a simulation.
package demo

import (
	"fmt"

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
	return []model.Game{
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
