package ui

// Home screen: the first thing `steam` shows. Banner, a greeting with the
// library summary, and three cards to jump straight into something:
// recently played games, games closest to 100%, and friends online.

import (
	"fmt"
	"runtime"
	"sort"

	"github.com/tomikav/steam-achievements-cli-/internal/model"
)

var homeCards = []string{"CONTINUE PLAYING", "ALMOST THERE", "FRIENDS ONLINE"}

type homeState struct {
	col    int    // focused shelf
	row    [3]int // focused tile in each shelf
	nav    bool   // focus is on the top bar
	navIdx int
}

// homeItems returns the entries of one card.
func (b *browser) homeGames(card int) []model.Game {
	var out []model.Game
	for _, g := range b.games {
		switch card {
		case 0:
			if g.LastPlayed > 0 {
				out = append(out, g)
			}
		case 1:
			if hasAch(g) && g.Percent > 0 && g.Percent < 99.95 {
				out = append(out, g)
			}
		}
	}
	if card == 0 {
		sort.SliceStable(out, func(i, j int) bool { return out[i].LastPlayed > out[j].LastPlayed })
	} else {
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].Percent != out[j].Percent {
				return out[i].Percent > out[j].Percent
			}
			return out[i].Total-out[i].Achieved < out[j].Total-out[j].Achieved
		})
	}
	return out
}

func (b *browser) homeFriends() []model.Friend {
	var out []model.Friend
	for _, f := range b.friends.list { // already sorted: in game, online, away
		if f.State > 0 || f.Game != "" {
			out = append(out, f)
		}
	}
	return out
}

func (b *browser) homeLen(card int) int {
	if card == 2 {
		return len(b.homeFriends())
	}
	return len(b.homeGames(card))
}

func (b *browser) perfLine() string {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	cli := paint(active.Dim, "this CLI ") + bold(active.Good, mb(int64(m.Sys)))
	p := &b.perf
	switch {
	case p.confirm:
		return chip(active.Bad, active.Ink, "PRESS X AGAIN TO CLOSE STEAM") + "  " + paint(active.Dim, "games still launch from here")
	case p.closing:
		return paint(active.Accent, b.spin()) + paint(active.Dim, "  closing the Steam client...")
	case !p.known:
		return cli
	case p.steam == 0:
		return paint(active.Good, "Steam client closed") + paint(active.Dim, "   ") + cli + paint(active.Dim, "   games start Steam hidden")
	}
	return paint(active.Dim, "Steam client ") + bold(active.Warn, mb(p.steam)) + paint(active.Dim, " RAM   ") + cli +
		paint(active.Dim, "   ") + bold(active.Accent, "x") + paint(active.Dim, " closes Steam")
}

func mb(n int64) string {
	if n >= 1<<30 {
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	}
	return fmt.Sprintf("%d MB", n>>20)
}

// closeSteamKey closes the Steam client on the second x.
func (b *browser) closeSteamKey() {
	p := &b.perf
	if !p.confirm {
		p.confirm = true
		return
	}
	p.confirm = false
	if b.opt.CloseSteam == nil {
		b.status = "demo mode: Steam was not closed"
		return
	}
	if err := b.opt.CloseSteam(); err != nil {
		b.status = "could not close Steam: " + err.Error()
		return
	}
	p.closing = true
	b.status = "Steam is closing; games launched from here start it hidden"
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
