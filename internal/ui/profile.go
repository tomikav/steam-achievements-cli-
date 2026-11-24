package ui

// Profile view (`m`): the user's Steam profile as a poster. Name in big
// pixel letters, Steam level with its ring colour, years on Steam, the
// library in numbers, what was played these two weeks and a showcase of
// the rarest achievements unlocked.

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tomikav/steam-achievements-cli-/internal/icons"
	"github.com/tomikav/steam-achievements-cli-/internal/model"
)

type profileState struct {
	loading, loaded bool
	err             error
	p               model.Profile
	back            browseMode
	showcaseBusy    bool
	showcaseDone    bool
	want            bool
	wantShowcase    bool
}

type showcaseItem struct {
	game model.Game
	ach  model.Achievement
}

// openProfile switches to the profile view.
func (b *browser) openProfile() {
	ps := &b.profile
	if b.mode != modeProfile {
		ps.back = b.mode
		if ps.back != modeHome {
			ps.back = modeList
		}
	}
	b.mode, b.full = modeProfile, true
	if !ps.loaded && !ps.loading {
		if b.opt.LoadProfile == nil {
			ps.err = fmt.Errorf("profile is not available here")
			return
		}
		ps.loading, ps.want = true, true
	}
	if !ps.showcaseDone && !ps.showcaseBusy && b.opt.LoadAchievements != nil {
		ps.wantShowcase = true
	}
}

func (b *browser) profileKey(k string) {
	switch k {
	case "esc", "backspace", "m":
		b.mode, b.full = b.profile.back, true
	case "y", "enter":
		b.openWrapped()
	case "o":
		if b.opt.Demo {
			b.status = "demo mode: nothing to open"
			return
		}
		url := b.profile.p.ProfileURL
		if url == "" && b.opt.SteamID != "" {
			url = "https://steamcommunity.com/profiles/" + b.opt.SteamID
		}
		if url != "" && openTarget(url) == nil {
			b.status = "opened your profile in the browser"
		}
	case "R":
		b.profile.loaded, b.profile.err = false, nil
		b.openProfile()
	}
}

// showcaseGames picks the games whose achievements feed the showcase: the
// ones with the most unlocks.
func (b *browser) showcaseGames() []model.Game {
	var gs []model.Game
	for _, g := range b.games {
		if hasAch(g) && g.Achieved > 0 {
			gs = append(gs, g)
		}
	}
	sort.SliceStable(gs, func(i, j int) bool { return gs[i].Achieved > gs[j].Achieved })
	return gs[:min(len(gs), 10)]
}

// showcase is the rarest unlocked achievements among the cached lists.
func (b *browser) showcase(n int) []showcaseItem {
	var out []showcaseItem
	for _, g := range b.games {
		for _, a := range b.cache[g.AppID] {
			if a.Achieved && a.Global > 0 {
				out = append(out, showcaseItem{g, a})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ach.Global < out[j].ach.Global })
	return out[:min(len(out), n)]
}

// profileIO runs the profile loads.
func (b *browser) profileIO(events chan<- func()) {
	ps := &b.profile
	opt := b.opt
	if ps.want && opt.LoadProfile != nil {
		ps.want = false
		go func() {
			p, err := opt.LoadProfile()
			events <- func() {
				ps.loading = false
				ps.p, ps.err, ps.loaded = p, err, err == nil
				if err == nil && b.icons && p.Avatar != "" {
					b.wantAvatarBig = true
				}
			}
		}()
	}
	if ps.wantShowcase && !ps.showcaseBusy && opt.LoadAchievements != nil {
		ps.wantShowcase, ps.showcaseBusy = false, true
		var todo []int
		for _, g := range b.showcaseGames() {
			if _, ok := b.cache[g.AppID]; !ok {
				todo = append(todo, g.AppID)
			}
		}
		go func() {
			var wg sync.WaitGroup
			jobs := make(chan int)
			for w := 0; w < 4; w++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for id := range jobs {
						list, err := opt.LoadAchievements(id)
						if err == nil {
							events <- func() {
								if _, ok := b.cache[id]; !ok {
									b.cache[id] = list
								}
							}
						}
					}
				}()
			}
			for _, id := range todo {
				jobs <- id
			}
			close(jobs)
			wg.Wait()
			events <- func() { ps.showcaseBusy, ps.showcaseDone = false, true }
		}()
	}
	if b.wantAvatarBig && b.icons && ps.p.Avatar != "" {
		b.wantAvatarBig = false
		px := b.avatarPx()
		key := fmt.Sprintf("avatar@%d", px)
		if _, ok := b.big[key]; !ok {
			b.wantBig = &icons.Request{Key: key, URL: ps.p.Avatar, Size: px}
		}
	}
}

// avatar size: 6 rows tall.
func (b *browser) avatarPx() int { return min(6*b.cellH, 184) }

// levelColor is the colour of Steam's level ring for a level.
func levelColor(level int) string {
	rings := []string{"#9b9b9b", "#c02942", "#d95b43", "#fecc23", "#5ba32b", "#4e8ddb", "#7652c9", "#c252c9", "#a8425f", "#b48e5b"}
	return rings[(level/10)%len(rings)]
}

// levelBadge draws the level as a ring: three rows.
func levelBadge(level int) []string {
	c := levelColor(level)
	n := fmt.Sprint(countUp(level))
	inner := max(len(n)+2, 4)
	pad := inner - len(n)
	return []string{
		paint(c, "╭"+strings.Repeat("─", inner)+"╮"),
		paint(c, "│") + strings.Repeat(" ", pad/2) + bold(active.Text, n) + strings.Repeat(" ", pad-pad/2) + paint(c, "│"),
		paint(c, "╰"+strings.Repeat("─", inner)+"╯"),
	}
}

func yearsSince(t int64) int {
	if t == 0 {
		return 0
	}
	c := time.Unix(t, 0)
	n := time.Now()
	y := n.Year() - c.Year()
	if n.YearDay() < c.YearDay() {
		y--
	}
	return max(y, 0)
}

func (b *browser) buildProfile(f *frame) {
	ps := &b.profile
	b.header(f, paint(active.Dim, "PROFILE"))
	clear := func(from int) {
		for r := from; r < b.h-1; r++ {
			f.put(r, 0, strings.Repeat(" ", b.w))
		}
	}
	center := func(msg string) {
		clear(1)
		f.put(b.h/2, 0, padRight(strings.Repeat(" ", max((b.w-width(msg))/2, 0))+msg, b.w))
	}
	switch {
	case ps.err != nil:
		center(chip(active.Bad, active.Ink, "ERROR") + " " + paint(active.Text, ps.err.Error()) + paint(active.Dim, "   R to retry"))
		return
	case !ps.loaded:
		center(paint(active.Accent, b.spin()) + " " + paint(active.Text, "Loading your profile..."))
		return
	}
	p := ps.p
	clear(1)

	// --- hero: avatar, name, level --------------------------------------
	top := 2
	x := 3
	if b.icons {
		px := b.avatarPx()
		key := fmt.Sprintf("avatar@%d", px)
		cols := int(math.Ceil(float64(px) / float64(b.cellW)))
		rows := int(math.Ceil(float64(px) / float64(b.cellH)))
		if img, ok := b.big[key]; ok && img != "" {
			f.icons = append(f.icons, iconSpot{row: top, col: x, w: cols, h: rows, key: key, sixel: img})
			x += cols + 3
		} else if !ok {
			b.wantAvatarBig = true
			x += cols + 3
		}
	}
	right := 16 // room for the level badge
	name := fitBigText(p.Persona, b.w-x-right-4)
	for i, l := range bigText(name, active.From, active.To, animT) {
		f.put(top+i, x, l)
	}
	var who []string
	if p.RealName != "" {
		who = append(who, paint(active.Text, p.RealName))
	}
	if p.Country != "" {
		who = append(who, paint(active.Dim, p.Country))
	}
	if y := yearsSince(p.Created); p.Created > 0 {
		who = append(who, paint(active.Dim, "on Steam since ")+paint(active.Text, time.Unix(p.Created, 0).Format("Jan 2006"))+
			paint(active.Dim, fmt.Sprintf(" (%d %s)", y, plural(y, "year", "years"))))
	}
	f.put(top+4, x, strings.Join(who, paint(active.Faint, "   ·   ")))
	status := paint(active.Dim, "● offline")
	switch {
	case p.Game != "":
		status = paint(pulse(active.Good, active.Faint), "●") + " " + paint(active.Good, "playing ") + bold(active.Text, p.Game)
	case p.State > 0:
		status = paint(pulse(active.From, active.Faint), "●") + " " + paint(active.From, "online")
	case p.LastLogoff > 0:
		status = paint(active.Dim, "● last online "+ago(time.Unix(p.LastLogoff, 0)))
	}
	f.put(top+5, x, status)

	// level badge, top right
	lx := b.w - right
	f.put(top, lx-1, paint(active.Dim, "LEVEL"))
	for i, l := range levelBadge(p.Level) {
		f.put(top+1+i, lx, l)
	}
	if p.XPToNext > 0 || p.XP > 0 {
		span := p.XPToNext + (p.XP - p.XPThisLevel)
		done := p.XP - p.XPThisLevel
		frac := 0.0
		if span > 0 {
			frac = float64(done) / float64(span)
		}
		f.put(top+4, lx-1, meter(frac, 12, levelColor(p.Level)))
		f.put(top+5, lx-1, paint(active.Dim, fmt.Sprintf("%d XP to %d", p.XPToNext, p.Level+1)))
	}

	// --- tiles -----------------------------------------------------------
	s := summarize(b.games)
	unlocked := 0
	for _, g := range b.games {
		unlocked += g.Achieved
	}
	type tile struct{ label, value, sub string }
	tiles := []tile{
		{"GAMES", fmt.Sprint(countUp(len(b.games))), fmt.Sprintf("%d with achievements", s.tracked)},
		{"HOURS", fmt.Sprint(countUp(s.playtime / 60)), "in your library"},
		{"ACHIEVEMENTS", fmt.Sprint(countUp(unlocked)), "unlocked"},
		{"PERFECT", fmt.Sprint(countUp(s.perfect)), "games at 100%"},
		{"AVERAGE", fmt.Sprintf("%.1f%%", s.average*animT), "completion"},
		{"BADGES", fmt.Sprint(countUp(p.Badges)), "earned"},
	}
	tw := 22
	n := max(1, min(len(tiles), (b.w-4)/(tw+2)))
	row := top + 7
	for i, t := range tiles[:n] {
		bx := box("", []string{paint(active.Dim, t.label), gradient(t.value), paint(active.Dim, Truncate(t.sub, tw-4))}, tw-4)
		for j, l := range strings.Split(strings.TrimRight(bx, "\n"), "\n") {
			f.put(row+j, 3+i*(tw+2), l)
		}
	}
	row += 6

	// --- two columns: recent activity | showcase ------------------------
	colW := (b.w - 9) / 2
	left := []string{gradient("RECENT ACTIVITY") + paint(active.Dim, "  last two weeks"), ""}
	if len(p.Recent) == 0 {
		left = append(left, paint(active.Dim, "Nothing played in the last two weeks."))
	}
	peak := 1
	for _, g := range p.Recent {
		peak = max(peak, g.TwoWeeks)
	}
	for i, g := range p.Recent {
		if i >= 5 {
			break
		}
		c := lerp(active.From, active.To, float64(i)/4)
		left = append(left,
			bold(active.Text, Truncate(g.Name, colW-2)),
			meter(float64(g.TwoWeeks)/float64(peak), max(colW-28, 8), c)+"  "+
				paint(active.Text, formatPlaytime(g.TwoWeeks))+paint(active.Dim, "  of "+formatPlaytime(g.Forever)),
			"")
	}
	rightCol := []string{gradient("SHOWCASE") + paint(active.Dim, "  your rarest achievements"), ""}
	sc := b.showcase(5)
	switch {
	case len(sc) == 0 && (ps.showcaseBusy || ps.wantShowcase):
		rightCol = append(rightCol, paint(active.Accent, b.spin())+paint(active.Dim, " looking through your unlocks..."))
	case len(sc) == 0:
		rightCol = append(rightCol, paint(active.Dim, "No rarity data yet."))
	}
	for _, it := range sc {
		rightCol = append(rightCol,
			rarityChip(it.ach.Global)+" "+bold(active.Text, Truncate(achName(it.ach), colW-14)),
			paint(active.Accent, Truncate(it.game.Name, colW/2))+paint(active.Dim, fmt.Sprintf("   only %.1f%% of players", it.ach.Global)),
			"")
	}
	for i := 0; i < max(len(left), len(rightCol)) && row+i < b.h-2; i++ {
		if i < len(left) {
			f.put(row+i, 3, padRight(left[i], colW))
		}
		if i < len(rightCol) {
			f.put(row+i, 6+colW, rightCol[i])
		}
	}

	// --- activity strip, if there is room -------------------------------
	hr := row + max(len(left), len(rightCol)) + 1
	if act := NewActivity(b.games); !act.Empty() && hr+9 < b.h-1 {
		cur, long := act.Streaks()
		f.put(hr, 3, gradient("ACTIVITY")+paint(active.Dim, fmt.Sprintf("  streak %d, best %d", cur, long)))
		for i, l := range act.Heatmap(min(52, max((b.w-12)/2, 8))) {
			f.put(hr+1+i, 3, l)
		}
	}
}
