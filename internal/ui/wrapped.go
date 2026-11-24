package ui

// Wrapped: your year on Steam as a short animated slideshow, and a share
// card exported as SVG. Everything is computed locally from the unlock
// times the cache already keeps.

import (
	"fmt"
	"html"
	"sort"
	"strings"
	"time"

	"github.com/tomikav/steam-achievements-cli-/internal/model"
)

// Wrapped is the year in numbers.
type Wrapped struct {
	Year       int
	Unlocked   int
	PerMonth   [12]int
	BestMonth  int // 0-11, -1 when empty
	BusiestDay time.Time
	BusiestN   int
	PerHour    [24]int
	PerWeekday [7]int // Monday first
	Streak     int
	GamesTouch int
	TopGame    model.Game
	TopGameN   int
	Perfect    []string // games completed this year
	Rarest     *showcaseItem
	Hours      int // whole library, all time
	MostPlayed model.Game
	Archetype  string
	Blurb      string
}

// ComputeWrapped builds the year summary. cache may be nil; it only feeds
// the rarest achievement.
func ComputeWrapped(games []model.Game, year int, cache map[int][]model.Achievement) Wrapped {
	w := Wrapped{Year: year, BestMonth: -1}
	days := map[string]int{}
	perGame := map[int]int{}
	for _, g := range games {
		w.Hours += g.Playtime
		if g.Playtime > w.MostPlayed.Playtime {
			w.MostPlayed = g
		}
		last := int64(0)
		for _, u := range g.Unlocks {
			last = max(last, u)
			t := time.Unix(u, 0)
			if t.Year() != year {
				continue
			}
			w.Unlocked++
			w.PerMonth[t.Month()-1]++
			w.PerHour[t.Hour()]++
			w.PerWeekday[(int(t.Weekday())+6)%7]++
			days[t.Format("2006-01-02")]++
			perGame[g.AppID]++
		}
		if hasAch(g) && g.Achieved >= g.Total && last > 0 && time.Unix(last, 0).Year() == year {
			w.Perfect = append(w.Perfect, g.Name)
		}
	}
	w.Hours /= 60
	w.GamesTouch = len(perGame)
	for _, g := range games {
		if n := perGame[g.AppID]; n > w.TopGameN || n == w.TopGameN && n > 0 && g.Playtime > w.TopGame.Playtime {
			w.TopGame, w.TopGameN = g, n
		}
	}
	for m, n := range w.PerMonth {
		if n > 0 && (w.BestMonth < 0 || n > w.PerMonth[w.BestMonth]) {
			w.BestMonth = m
		}
	}
	for d, n := range days {
		t, _ := time.ParseInLocation("2006-01-02", d, time.Local)
		if n > w.BusiestN || n == w.BusiestN && t.Before(w.BusiestDay) {
			w.BusiestDay, w.BusiestN = t, n
		}
	}
	// longest streak inside the year
	run := 0
	start := time.Date(year, 1, 1, 0, 0, 0, 0, time.Local)
	for d := start; d.Year() == year && !d.After(time.Now()); d = d.AddDate(0, 0, 1) {
		if days[d.Format("2006-01-02")] > 0 {
			run++
			w.Streak = max(w.Streak, run)
		} else {
			run = 0
		}
	}
	// rarest unlocked this year, from the achievement lists already loaded
	for _, g := range games {
		for _, a := range cache[g.AppID] {
			if a.Achieved && a.Global > 0 && time.Unix(a.UnlockTime, 0).Year() == year {
				if w.Rarest == nil || a.Global < w.Rarest.ach.Global {
					it := showcaseItem{g, a}
					w.Rarest = &it
				}
			}
		}
	}
	sort.Strings(w.Perfect)
	w.Archetype, w.Blurb = archetype(w)
	return w
}

// archetype names the way you play from when you unlock achievements.
func archetype(w Wrapped) (string, string) {
	if w.Unlocked == 0 {
		return "THE COLLECTOR", "Your year is just getting started."
	}
	night, morning, weekend := 0, 0, w.PerWeekday[5]+w.PerWeekday[6]
	for h, n := range w.PerHour {
		switch {
		case h < 5:
			night += n
		case h >= 5 && h < 11:
			morning += n
		}
	}
	switch {
	case len(w.Perfect) >= 3:
		return "THE COMPLETIONIST", fmt.Sprintf("%d games taken all the way to 100%%.", len(w.Perfect))
	case night*100/w.Unlocked >= 30:
		return "THE NIGHT OWL", fmt.Sprintf("%d%% of your unlocks happened after midnight.", night*100/w.Unlocked)
	case morning*100/w.Unlocked >= 30:
		return "THE EARLY BIRD", fmt.Sprintf("%d%% of your unlocks came before 11 am.", morning*100/w.Unlocked)
	case weekend*100/w.Unlocked >= 45:
		return "THE WEEKEND WARRIOR", fmt.Sprintf("%d%% of your unlocks landed on Saturday or Sunday.", weekend*100/w.Unlocked)
	case w.GamesTouch >= 15:
		return "THE EXPLORER", fmt.Sprintf("You earned achievements in %d different games.", w.GamesTouch)
	case w.TopGameN*100/w.Unlocked >= 40:
		return "THE DEVOTED", fmt.Sprintf("%d%% of your unlocks were in a single game.", w.TopGameN*100/w.Unlocked)
	}
	return "THE STEADY HUNTER", "A little every week, all year long."
}

// --- slideshow ---------------------------------------------------------------------

type wrappedState struct {
	slide int
	back  browseMode
	data  Wrapped
}

const wrappedSlides = 8

func (b *browser) openWrapped() {
	ws := &b.wrapped
	if b.mode != modeWrapped {
		ws.back = b.mode
	}
	ws.slide = 0
	b.mode, b.full = modeWrapped, true
	if !b.profile.showcaseDone && !b.profile.showcaseBusy && b.opt.LoadAchievements != nil {
		b.profile.wantShowcase = true // feeds the rarest unlock
	}
}

func (b *browser) wrappedKey(k string) {
	ws := &b.wrapped
	switch k {
	case "esc", "backspace", "y":
		b.mode, b.full = ws.back, true
	case "right", "l", "space", "enter", "down", "j", "tab":
		if ws.slide < wrappedSlides-1 {
			ws.slide++
			b.full = true
		}
	case "left", "h", "up", "k", "shift+tab":
		if ws.slide > 0 {
			ws.slide--
			b.full = true
		}
	case "e":
		ws.data = ComputeWrapped(b.games, time.Now().Year(), b.cache)
		if b.opt.ExportWrapped == nil {
			b.status = "export is not available here"
			return
		}
		path, err := b.opt.ExportWrapped(WrappedSVG(ws.data, b.opt.Owner))
		if err != nil {
			b.status = "could not save the card: " + err.Error()
		} else {
			b.status = "share card saved to " + path
		}
	}
}

// bigTextScaled draws s with every pixel doubled: five rows tall.
func bigTextScaled(s, from, to string, reveal float64) []string {
	s = strings.ToUpper(s)
	var grid [5][]bool
	for i, r := range s {
		g, ok := glyphs[r]
		if !ok {
			g = glyphs['?']
		}
		for y := 0; y < 5; y++ {
			if i > 0 {
				grid[y] = append(grid[y], false)
			}
			for _, c := range g[y] {
				grid[y] = append(grid[y], c == '#')
			}
		}
	}
	w := len(grid[0])
	shown := int(float64(w)*reveal + 0.999)
	out := make([]string, 5)
	for y := 0; y < 5; y++ {
		var sb strings.Builder
		for x := 0; x < w; x++ {
			col := lerp(from, to, float64(x)/float64(max(w-1, 1)))
			if grid[y][x] && x < shown {
				sb.WriteString(paint(col, "██"))
			} else {
				sb.WriteString("  ")
			}
		}
		out[y] = sb.String()
	}
	return out
}

func (b *browser) buildWrapped(f *frame) {
	ws := &b.wrapped
	ws.data = ComputeWrapped(b.games, time.Now().Year(), b.cache)
	w := ws.data
	for r := 0; r < b.h-1; r++ {
		f.put(r, 0, strings.Repeat(" ", b.w))
	}
	// progress dots, top centre
	var dots strings.Builder
	for i := 0; i < wrappedSlides; i++ {
		if i == ws.slide {
			dots.WriteString(paint(active.To, "━━━━"))
		} else if i < ws.slide {
			dots.WriteString(paint(active.From, "━━━━"))
		} else {
			dots.WriteString(paint(active.Faint, "━━━━"))
		}
		dots.WriteString(" ")
	}
	f.put(1, max((b.w-wrappedSlides*5)/2, 0), dots.String())

	var lines []string
	add := func(l ...string) { lines = append(lines, l...) }
	big := func(s string) {
		s = strings.ToUpper(s)
		if bigTextWidth(s)*2 <= b.w-8 {
			add(bigTextScaled(s, active.From, active.To, animT)...)
		} else {
			add(bigLines(s, b.w-8, 2, active.From, active.To, animT)...)
		}
	}
	label := func(s string) { add(paint(active.Dim, strings.Join(strings.Split(strings.ToUpper(s), ""), " ")), "") }
	monthNames := []string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}

	switch ws.slide {
	case 0:
		label("your year on steam")
		big(fmt.Sprint(w.Year))
		hey := "Here is how your year of achievement hunting went."
		if b.opt.Owner != "" {
			hey = "Hey " + b.opt.Owner + ". " + hey
		}
		add("", paint(active.Text, hey))
		add("", paint(active.Dim, "→ or space to continue"))
	case 1:
		label("achievements unlocked")
		big(fmt.Sprint(countUp(w.Unlocked)))
		add("", paint(active.Text, fmt.Sprintf("across %d %s", w.GamesTouch, plural(w.GamesTouch, "game", "games"))), "")
		short := []string{"J", "F", "M", "A", "M", "J", "J", "A", "S", "O", "N", "D"}
		add(columns(w.PerMonth[:], short, 6, 4)...)
	case 2:
		label("your biggest month")
		if w.BestMonth < 0 {
			big("SOON")
			add("", paint(active.Dim, "No unlocks this year yet."))
			break
		}
		big(monthNames[w.BestMonth])
		add("", bold(active.Text, fmt.Sprint(countUp(w.PerMonth[w.BestMonth])))+paint(active.Dim, " achievements that month"))
		if w.BusiestN > 0 {
			add("", paint(active.Dim, "busiest single day: ")+bold(active.Text, w.BusiestDay.Format("Monday, January 2"))+
				paint(active.Dim, fmt.Sprintf(", %d in one day", w.BusiestN)))
		}
	case 3:
		label("your game of the year")
		if w.TopGameN == 0 {
			big("TBD")
			break
		}
		big(w.TopGame.Name)
		add("", bold(active.Text, fmt.Sprint(countUp(w.TopGameN)))+paint(active.Dim, " achievements this year   ")+
			bold(active.Text, formatPlaytime(w.TopGame.Playtime))+paint(active.Dim, " played in total"))
		if hasAch(w.TopGame) {
			add("", meter(float64(w.TopGame.Achieved)/float64(w.TopGame.Total), min(48, b.w-20), active.Good)+
				paint(active.Text, fmt.Sprintf("  %d / %d", w.TopGame.Achieved, w.TopGame.Total)))
		}
	case 4:
		label("you are")
		big(strings.TrimPrefix(w.Archetype, "THE "))
		add("", paint(active.Text, w.Blurb), "")
		hours := make([]string, 24)
		for i := range hours {
			if i%6 == 0 {
				hours[i] = fmt.Sprint(i)
			}
		}
		add(paint(active.Dim, "unlocks by hour of the day"))
		add(columns(w.PerHour[:], hours, 4, 2)[:5]...)
		add(paint(active.Dim, padRight("0           6           12          18", 48)))
	case 5:
		label("longest streak")
		big(fmt.Sprintf("%d %s", countUp(w.Streak), plural(w.Streak, "DAY", "DAYS")))
		add("", paint(active.Text, "days in a row with at least one achievement"))
	case 6:
		label("rarest unlock")
		if w.Rarest == nil {
			big("???")
			add("", paint(active.Dim, "Open a few games or your profile so rarity data loads, then come back."))
			break
		}
		big(fmt.Sprintf("%.1f%%", w.Rarest.ach.Global))
		add("", rarityChip(w.Rarest.ach.Global)+" "+bold(active.Text, achName(w.Rarest.ach))+paint(active.Dim, " in ")+paint(active.Accent, w.Rarest.game.Name))
		add("", paint(active.Dim, "of players have this one. You do."))
	case 7:
		label("that was " + fmt.Sprint(w.Year))
		big(w.Archetype)
		stat := func(v, k string) string { return bold(active.To, v) + " " + paint(active.Dim, k) }
		add("", stat(fmt.Sprint(w.Unlocked), "achievements")+"     "+stat(fmt.Sprint(w.GamesTouch), "games")+"     "+
			stat(fmt.Sprint(w.Streak), "day streak")+"     "+stat(fmt.Sprint(len(w.Perfect)), "perfect"))
		if w.TopGameN > 0 {
			add("", paint(active.Dim, "game of the year  ")+bold(active.Text, w.TopGame.Name))
		}
		add("", "", bold(active.Accent, "e")+paint(active.Dim, " save a share card (SVG)    ")+bold(active.Accent, "esc")+paint(active.Dim, " back"))
	}
	// centre the block vertically and horizontally
	top := max((b.h-len(lines))/2, 3)
	for i, l := range lines {
		if top+i >= b.h-1 {
			break
		}
		f.put(top+i, max((b.w-width(l))/2, 0), l)
	}
}

// WrappedSVG renders a 1200x630 share card in the Steam palette.
func WrappedSVG(w Wrapped, owner string) string {
	esc := html.EscapeString
	var sb strings.Builder
	sb.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="630" viewBox="0 0 1200 630" font-family="'Motiva Sans','Segoe UI',Arial,sans-serif">
<defs>
<linearGradient id="bg" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="#171a21"/><stop offset="1" stop-color="#1b2838"/></linearGradient>
<linearGradient id="ac" x1="0" y1="0" x2="1" y2="0"><stop offset="0" stop-color="#66c0f4"/><stop offset="1" stop-color="#1a9fff"/></linearGradient>
</defs>
<rect width="1200" height="630" fill="url(#bg)"/>
<rect x="0" y="0" width="1200" height="6" fill="url(#ac)"/>
`)
	fmt.Fprintf(&sb, `<text x="64" y="92" fill="#8f98a0" font-size="22" letter-spacing="6">MY %d ON STEAM</text>
<text x="64" y="132" fill="#c7d5e0" font-size="28" font-weight="600">%s</text>
<text x="64" y="250" fill="url(#ac)" font-size="88" font-weight="800">%s</text>
<text x="64" y="296" fill="#c7d5e0" font-size="24">%s</text>
`, w.Year, esc(owner), esc(w.Archetype), esc(w.Blurb))
	tiles := []struct{ v, k string }{
		{fmt.Sprint(w.Unlocked), "achievements"}, {fmt.Sprint(w.GamesTouch), "games"},
		{fmt.Sprint(w.Streak), "day streak"}, {fmt.Sprint(len(w.Perfect)), "perfect games"},
	}
	for i, t := range tiles {
		x := 64 + i*272
		fmt.Fprintf(&sb, `<rect x="%d" y="350" width="248" height="120" rx="6" fill="#2a475e" fill-opacity="0.55"/>
<text x="%d" y="420" fill="#ffffff" font-size="52" font-weight="700">%s</text>
<text x="%d" y="452" fill="#8f98a0" font-size="20">%s</text>
`, x, x+24, t.v, x+24, t.k)
	}
	// unlocks per month
	peak := 1
	for _, n := range w.PerMonth {
		peak = max(peak, n)
	}
	for m, n := range w.PerMonth {
		h := n * 70 / peak
		fmt.Fprintf(&sb, `<rect x="%d" y="%d" width="22" height="%d" rx="2" fill="#66c0f4" fill-opacity="%.2f"/>`+"\n",
			820+m*28, 580-h, max(h, 2), 0.35+0.65*float64(n)/float64(peak))
	}
	if w.TopGameN > 0 {
		fmt.Fprintf(&sb, `<text x="64" y="530" fill="#8f98a0" font-size="20">GAME OF THE YEAR</text>
<text x="64" y="566" fill="#c7d5e0" font-size="30" font-weight="600">%s</text>
`, esc(w.TopGame.Name))
	}
	sb.WriteString(`<text x="1136" y="612" fill="#4b5d6e" font-size="16" text-anchor="end">steam-achievements-cli</text>
</svg>
`)
	return sb.String()
}

// RenderWrapped is the non-interactive version for `steam wrapped`.
func RenderWrapped(w Wrapped, owner string, tw int) string {
	var b strings.Builder
	b.WriteString("\n")
	for _, l := range bigText(fmt.Sprintf("%d", w.Year), active.From, active.To, 1) {
		b.WriteString("  " + l + "\n")
	}
	who := "your year on Steam"
	if owner != "" {
		who += ", " + owner
	}
	b.WriteString("  " + paint(active.Dim, who) + "\n\n")
	for _, l := range bigText(fitBigText(w.Archetype, tw-6), active.From, active.To, 1) {
		b.WriteString("  " + l + "\n")
	}
	b.WriteString("  " + paint(active.Text, w.Blurb) + "\n\n")
	kv := func(k, v string) {
		b.WriteString("  " + paint(active.Dim, padRight(k, 22)) + bold(active.Text, v) + "\n")
	}
	kv("achievements", fmt.Sprint(w.Unlocked))
	kv("games with unlocks", fmt.Sprint(w.GamesTouch))
	kv("longest streak", fmt.Sprintf("%d days", w.Streak))
	if w.BestMonth >= 0 {
		kv("best month", time.Month(w.BestMonth+1).String()+fmt.Sprintf(" (%d)", w.PerMonth[w.BestMonth]))
	}
	if w.TopGameN > 0 {
		kv("game of the year", w.TopGame.Name)
	}
	kv("perfect games", fmt.Sprint(len(w.Perfect)))
	b.WriteString("\n  " + paint(active.Dim, "per month  ") + sparkline(w.PerMonth[:], active.From, active.To) + "\n")
	return b.String()
}
