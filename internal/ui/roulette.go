package ui

// Roulette (`r` on the home screen): cannot decide what to play? A slot
// reel spins through your backlog and lands on one game, with the reason it
// is a good pick tonight.

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"
	"time"

	"github.com/tomikav/steam-achievements-cli-/internal/model"
)

type rouletteState struct {
	pool   []model.Game
	target int
	turns  int
	start  time.Time
	back   browseMode
	rnd    *rand.Rand
}

const spinFor = 2600 * time.Millisecond

// roulettePool is what the reel can land on: unfinished games, the ones
// with achievements left first, plus untouched games from the backlog.
func (b *browser) roulettePool() []model.Game {
	var pool []model.Game
	for _, g := range b.games {
		switch {
		case hasAch(g) && g.Achieved < g.Total:
			pool = append(pool, g)
		case !hasAch(g) && g.Playtime < 120:
			pool = append(pool, g) // barely touched backlog
		}
	}
	if len(pool) == 0 {
		pool = append(pool, b.games...)
	}
	sort.SliceStable(pool, func(i, j int) bool { return pool[i].Name < pool[j].Name })
	return pool
}

func (b *browser) openRoulette() {
	rs := &b.roulette
	if b.mode != modeRoulette {
		rs.back = b.mode
	}
	if rs.rnd == nil {
		rs.rnd = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	rs.pool = b.roulettePool()
	b.mode, b.full = modeRoulette, true
	b.spinRoulette()
}

func (b *browser) spinRoulette() {
	rs := &b.roulette
	if len(rs.pool) == 0 {
		return
	}
	// weight: games close to done and games not played lately are likelier
	weights := make([]float64, len(rs.pool))
	total := 0.0
	for i, g := range rs.pool {
		w := 1.0
		if hasAch(g) {
			w += float64(g.Achieved) / float64(g.Total) * 2
		}
		if g.LastPlayed > 0 && time.Since(time.Unix(g.LastPlayed, 0)) < 48*time.Hour {
			w *= 0.4
		}
		weights[i] = w
		total += w
	}
	x := rs.rnd.Float64() * total
	rs.target = len(rs.pool) - 1
	for i, w := range weights {
		if x < w {
			rs.target = i
			break
		}
		x -= w
	}
	rs.turns = 3 + rs.rnd.Intn(2)
	rs.start = time.Now()
}

func (b *browser) rouletteSpinning() bool {
	return b.mode == modeRoulette && time.Since(b.roulette.start) < spinFor+100*time.Millisecond
}

func (b *browser) rouletteKey(k string) {
	rs := &b.roulette
	switch k {
	case "esc", "backspace":
		b.mode, b.full = rs.back, true
	case "r", "space":
		b.spinRoulette()
	case "enter", "p":
		if !b.rouletteSpinning() && len(rs.pool) > 0 {
			b.openPlay(rs.pool[rs.target].AppID)
		}
	}
}

// reelPos is the reel position (in entries) at time t.
func (rs *rouletteState) reelPos(now time.Time) float64 {
	n := float64(len(rs.pool))
	end := float64(rs.turns)*n + float64(rs.target)
	t := float64(now.Sub(rs.start)) / float64(spinFor)
	if t >= 1 {
		return end
	}
	// quintic ease out: fast start, long satisfying slow-down
	return end * (1 - math.Pow(1-t, 5))
}

func (b *browser) buildRoulette(f *frame) {
	rs := &b.roulette
	b.header(f, paint(active.Dim, "WHAT TO PLAY"))
	for r := 1; r < b.h-1; r++ {
		f.put(r, 0, strings.Repeat(" ", b.w))
	}
	if len(rs.pool) == 0 {
		f.put(b.h/2, 4, paint(active.Dim, "Your library is empty."))
		return
	}
	now := time.Now()
	if !b.motion {
		now = rs.start.Add(spinFor) // static frames show the result
	}
	pos := rs.reelPos(now)
	spinning := now.Sub(rs.start) < spinFor
	n := len(rs.pool)

	reelW := min(56, b.w-8)
	x := 4
	if b.w >= 110 {
		x = (b.w/2 - reelW) / 2
	}
	mid := max(b.h/2-1, 6)
	f.put(mid-6, x, gradient("SURPRISE ME")+paint(active.Dim, fmt.Sprintf("   %d games in the pool", n)))
	frac := pos - math.Floor(pos)
	base := int(math.Floor(pos))
	for k := -3; k <= 3; k++ {
		idx := ((base+k)%n + n) % n
		g := rs.pool[idx]
		row := mid + k
		dist := math.Abs(float64(k) - frac)
		name := Truncate(g.Name, reelW-6)
		var line string
		switch {
		case k == 0 && !spinning:
			line = paint(active.Accent, "▶ ") + bold(active.Text, padRight(name, reelW-6)) + paint(active.Accent, " ◀")
		case dist < 0.6:
			line = "  " + bold(lerp(active.Text, active.From, 0.3), padRight(name, reelW-6)) + "  "
		default:
			line = "  " + paint(lerp(active.Dim, active.Faint, math.Min(dist/3, 1)), padRight(name, reelW-6)) + "  "
		}
		f.put(row, x, line)
	}
	edge := paint(active.Faint, strings.Repeat("─", reelW))
	f.put(mid-4, x, edge)
	f.put(mid+4, x, edge)
	if spinning {
		f.put(mid+6, x, paint(active.Accent, b.spin())+paint(active.Dim, " spinning..."))
		return
	}

	// the pick and why
	g := rs.pool[rs.target]
	cx, cw := x, reelW
	cy := mid + 6
	if b.w >= 110 {
		cx, cw, cy = b.w/2+2, b.w/2-6, mid-6
	}
	var why []string
	why = append(why, paint(active.Dim, "TONIGHT YOU PLAY"))
	reveal := animT // the name wipes in when the reel stops
	if b.motion {
		reveal = math.Min(reveal, easeOut(float64(now.Sub(rs.start)-spinFor)/float64(animDur)))
	}
	for _, l := range bigLines(g.Name, cw, 2, active.From, active.To, reveal) {
		why = append(why, l)
	}
	why = append(why, "")
	if hasAch(g) {
		left := g.Total - g.Achieved
		why = append(why, meter(float64(g.Achieved)/float64(g.Total), min(30, cw-14), active.Good)+
			paint(active.Text, fmt.Sprintf("  %d%%", g.Achieved*100/g.Total)),
			paint(active.Dim, fmt.Sprintf("%d %s left", left, plural(left, "achievement", "achievements"))))
		easy := append([]model.Achievement(nil), g.Locked...)
		sort.SliceStable(easy, func(i, j int) bool { return easy[i].Global > easy[j].Global })
		if len(easy) > 0 && easy[0].Global > 0 {
			why = append(why, "", paint(active.Dim, "easiest one left"),
				rarityChip(easy[0].Global)+" "+bold(active.Text, Truncate(achName(easy[0]), cw-12)),
				paint(active.Dim, Truncate(easy[0].Description, cw)))
		}
	} else {
		why = append(why, paint(active.Dim, "barely touched: "+formatPlaytime(g.Playtime)+" played"))
	}
	if g.LastPlayed > 0 {
		why = append(why, "", paint(active.Dim, "last played "+ago(time.Unix(g.LastPlayed, 0))))
	} else {
		why = append(why, "", paint(active.Dim, "never played"))
	}
	why = append(why, "", bold(active.Accent, "enter")+paint(active.Dim, " go   ")+bold(active.Accent, "r")+
		paint(active.Dim, " spin again   ")+bold(active.Accent, "esc")+paint(active.Dim, " back"))
	for i, l := range why {
		if cy+i < b.h-1 {
			f.put(cy+i, cx, l)
		}
	}
}
