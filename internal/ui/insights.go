package ui

// Small helpers that turn the cached unlock history into advice: today's
// challenge and how long a game will take to finish at your pace.

import (
	"fmt"
	"hash/fnv"
	"sort"
	"time"

	"github.com/tomikav/steam-achievements-cli-/internal/model"
)

// challenge picks one achievement to go for today. Everyone gets the same
// pick for the whole day: an easy-ish locked achievement (between 10% and
// 80% of players) in a game you have played, chosen by the date.
func (b *browser) challenge() (model.Game, model.Achievement, bool) {
	type cand struct {
		g model.Game
		a model.Achievement
	}
	var cs []cand
	for _, g := range b.games {
		if !hasAch(g) || g.Playtime == 0 {
			continue
		}
		for _, a := range g.Locked {
			if a.Global >= 10 && a.Global <= 80 {
				cs = append(cs, cand{g, a})
			}
		}
	}
	if len(cs) == 0 {
		return model.Game{}, model.Achievement{}, false
	}
	sort.SliceStable(cs, func(i, j int) bool {
		if cs[i].g.AppID != cs[j].g.AppID {
			return cs[i].g.AppID < cs[j].g.AppID
		}
		return cs[i].a.Name < cs[j].a.Name
	})
	h := fnv.New32a()
	fmt.Fprint(h, time.Now().Format("2006-01-02"))
	c := cs[h.Sum32()%uint32(len(cs))]
	return c.g, c.a, true
}

// forecast says when a game reaches 100% at the pace of its last 60 days.
func forecast(g model.Game) string {
	if !hasAch(g) || g.Achieved >= g.Total {
		return ""
	}
	since := time.Now().AddDate(0, 0, -60).Unix()
	recent := 0
	for _, u := range g.Unlocks {
		if u >= since {
			recent++
		}
	}
	left := g.Total - g.Achieved
	if recent == 0 {
		return fmt.Sprintf("%d left · no unlocks in the last 60 days", left)
	}
	days := int(float64(left)/(float64(recent)/60) + 0.5)
	when := ""
	switch {
	case days <= 1:
		when = "tomorrow"
	case days < 14:
		when = fmt.Sprintf("in about %d days", days)
	case days < 90:
		when = fmt.Sprintf("in about %d weeks", days/7)
	case days < 730:
		when = fmt.Sprintf("in about %d months", days/30)
	default:
		when = fmt.Sprintf("in about %d years", days/365)
	}
	return "100% " + when + " at your current pace"
}
