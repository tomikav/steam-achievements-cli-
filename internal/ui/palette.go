package ui

// Command palette (ctrl+k or `:`): one box that finds anything. Games,
// locked achievements, friends, themes, store searches and every action
// of the app, ranked as you type. A leading verb narrows it down:
//
//	play hades        launch a game
//	store hollow      search the Steam store
//	theme aurora      switch theme
//	hunt elden        the game's achievements
//
// Everything else is fuzzy: "hk" finds Hollow Knight.

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/tomikav/steam-achievements-cli-/internal/model"
)

type palItem struct {
	kind  string // ACTION, GAME, ACHIEVEMENT, FRIEND, THEME, STORE
	title string
	sub   string
	key   string // shortcut shown on the right
	run   func(b *browser)
	rank  int // tie breaker: lower first
}

type paletteState struct {
	open    bool
	query   string
	cursor  int
	results []palItem
	index   []palItem // games, achievements, friends; rebuilt on open
}

var palKinds = map[string]string{
	"ACTION": "#66c0f4", "GAME": "#a4d007", "ACHIEVEMENT": "#e5b143",
	"FRIEND": "#57cbde", "THEME": "#b18cff", "STORE": "#1a9fff",
}

func (b *browser) openPalette() {
	ps := &b.palette
	ps.open, ps.query, ps.cursor = true, "", 0
	ps.index = b.paletteIndex()
	b.refreshPalette()
	b.full = true
}

func (b *browser) closePalette() {
	b.palette.open = false
	b.full = true
}

// actions are the app's commands, always available.
func (b *browser) paletteActions() []palItem {
	a := func(title, sub, key string, run func(*browser)) palItem {
		return palItem{kind: "ACTION", title: title, sub: sub, key: key, run: run}
	}
	panel := func(k panelKind) func(*browser) { return func(b *browser) { b.openPanel(k) } }
	return []palItem{
		a("Home", "start screen", "esc", func(b *browser) { b.mode, b.full = modeHome, true }),
		a("Library", "all your games", "l", func(b *browser) { b.mode, b.full = modeList, true }),
		a("Store", "featured, top sellers, new, search", "s", func(b *browser) { b.openStore() }),
		a("Cart", "games you keep an eye on", "", func(b *browser) { b.openStore(); b.store.tab = tabCart }),
		a("Profile", "your Steam profile", "m", func(b *browser) { b.openProfile() }),
		a("Your year (Wrapped)", "the year in achievements", "y", func(b *browser) { b.openWrapped() }),
		a("Surprise me", "spin the roulette and pick a game", "r", func(b *browser) { b.openRoulette() }),
		a("Achievement hunt", "easiest locked achievements", "a", panel(panelHunt)),
		a("Deals", "wishlist on sale", "w", panel(panelDeals)),
		a("News", "patch notes of your games", "n", panel(panelNews)),
		a("Storage", "installed games by size", "u", panel(panelStorage)),
		a("Friends", "who is online and what they play", "f", func(b *browser) { b.mode, b.back, b.full = modeFriends, modeHome, true; b.loadFriendsOnce() }),
		a("Stats", "charts of your library", "d", func(b *browser) { b.back, b.mode, b.scroll, b.full = modeHome, modeDashboard, 0, true }),
		a("Today's challenge", "one achievement picked for today", "c", func(b *browser) { b.mode = modeHome; b.homeKey("c") }),
		a("Close Steam", "keep playing from here, Steam starts hidden", "x x", func(b *browser) { b.mode = modeHome; b.perf.confirm = true }),
		a("Help", "every key", "?", func(b *browser) { b.prev, b.mode, b.scroll, b.full = modeHome, modeHelp, 0, true }),
		a("Quit", "", "ctrl+q", func(b *browser) { b.quit = true }),
	}
}

// paletteIndex lists the things the palette can find besides actions.
func (b *browser) paletteIndex() []palItem {
	var out []palItem
	for _, g := range b.games {
		g := g
		sub := formatPlaytime(g.Playtime) + " played"
		if hasAch(g) {
			sub = fmt.Sprintf("%d/%d achievements   %s", g.Achieved, g.Total, sub)
		}
		out = append(out, palItem{kind: "GAME", title: g.Name, sub: sub, key: "enter", rank: 1,
			run: func(b *browser) { b.openPlay(g.AppID) }})
	}
	for _, g := range b.games {
		g := g
		for _, a := range g.Locked {
			sub := g.Name
			if a.Global > 0 {
				sub += fmt.Sprintf("   %.1f%% of players", a.Global)
			}
			out = append(out, palItem{kind: "ACHIEVEMENT", title: achName(a), sub: sub, rank: 3,
				run: func(b *browser) { b.openPlay(g.AppID) }})
		}
	}
	for _, f := range b.friends.list {
		f := f
		out = append(out, palItem{kind: "FRIEND", title: f.Name, sub: friendStateText(f), rank: 2,
			run: func(b *browser) {
				b.mode, b.back, b.full = modeFriends, modeHome, true
				for i, x := range b.friends.list {
					if x.SteamID == f.SteamID {
						b.friends.cursor = i
					}
				}
			}})
	}
	for _, f := range b.friends.list {
		f := f
		out = append(out, palItem{kind: "FRIEND", title: "Compare with " + f.Name, sub: "games in common, hours, achievements", rank: 3,
			run: func(b *browser) { b.openVersus(f) }})
	}
	for _, t := range themeOrder {
		t := t
		out = append(out, palItem{kind: "THEME", title: "Theme: " + t, sub: "change colours", rank: 4,
			run: func(b *browser) { _ = SetTheme(t); b.status = "theme: " + t; b.full = true }})
	}
	return out
}

func friendStateText(f model.Friend) string {
	switch {
	case f.Game != "":
		return "playing " + f.Game
	case f.State > 0:
		return "online"
	}
	return "offline"
}

// fuzzy scores how well q matches s: -1 when it does not. Matches at word
// starts and runs of consecutive letters score higher; a prefix wins.
func fuzzy(q, s string) int {
	if q == "" {
		return 0
	}
	ls := strings.ToLower(s)
	if i := strings.Index(ls, q); i >= 0 {
		score := 1000 - i*4 - (len(ls) - len(q))
		if i == 0 || !unicode.IsLetter(rune(ls[i-1])) {
			score += 400
		}
		return score
	}
	score, si, run := 0, 0, 0
	prev := ' '
	for _, qc := range q {
		found := false
		for si < len(ls) {
			c := rune(ls[si])
			start := !unicode.IsLetter(prev) && !unicode.IsDigit(prev)
			prev = c
			si++
			if c == qc {
				found = true
				run++
				score += 10 + run*6
				if start {
					score += 30
				}
				break
			}
			run = 0
		}
		if !found {
			return -1
		}
	}
	return score - len(ls)/4
}

var palVerbs = map[string]string{
	"play": "play", "launch": "play", "run": "play", "start": "play",
	"store": "store", "buy": "store", "shop": "store", "find": "store",
	"theme": "theme", "hunt": "hunt", "friend": "friend",
}

func (b *browser) refreshPalette() {
	ps := &b.palette
	q := strings.ToLower(strings.TrimSpace(ps.query))
	verb := ""
	if f := strings.Fields(q); len(f) > 1 {
		if v, ok := palVerbs[f[0]]; ok {
			verb = v
			q = strings.TrimSpace(strings.TrimPrefix(q, f[0]))
		}
	}
	type scored struct {
		it    palItem
		score int
	}
	var out []scored
	consider := func(it palItem) {
		if s := fuzzy(q, it.title); s >= 0 {
			if it.kind == "ACTION" {
				s += 50
			}
			out = append(out, scored{it, s})
		}
	}
	switch verb {
	case "play":
		for _, it := range ps.index {
			if it.kind == "GAME" {
				it := it
				open := it.run
				it.sub, it.key = "launch now", "enter"
				it.run = func(b *browser) { open(b); b.playKey("enter") }
				consider(it)
			}
		}
	case "store":
		raw := strings.TrimSpace(ps.query[strings.Index(ps.query, " ")+1:])
		out = append(out, scored{palItem{kind: "STORE", title: "Search the store for \"" + raw + "\"", key: "enter",
			run: func(b *browser) { b.storeSearch(raw) }}, 1 << 20})
		for _, it := range ps.index {
			if it.kind == "GAME" {
				consider(it)
			}
		}
	case "theme", "friend":
		for _, it := range ps.index {
			if strings.EqualFold(it.kind, verb) {
				consider(it)
			}
		}
	case "hunt":
		for _, it := range ps.index {
			if it.kind == "GAME" || it.kind == "ACHIEVEMENT" {
				consider(it)
			}
		}
	default:
		for _, it := range b.paletteActions() {
			consider(it)
		}
		if q != "" {
			for _, it := range ps.index {
				consider(it)
			}
			// always offer the store as the last resort
			raw := strings.TrimSpace(ps.query)
			out = append(out, scored{palItem{kind: "STORE", title: "Search the store for \"" + raw + "\"", key: "",
				run: func(b *browser) { b.storeSearch(raw) }}, -1})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		return out[i].it.rank < out[j].it.rank
	})
	ps.results = ps.results[:0]
	for i, s := range out {
		if i >= 60 {
			break
		}
		ps.results = append(ps.results, s.it)
	}
	ps.cursor = min(ps.cursor, max(len(ps.results)-1, 0))
}

func (b *browser) paletteKey(k string) {
	ps := &b.palette
	switch k {
	case "esc", "ctrl+k":
		b.closePalette()
	case "enter":
		if ps.cursor < len(ps.results) {
			it := ps.results[ps.cursor]
			b.closePalette()
			it.run(b)
		}
	case "down", "tab", "ctrl+n":
		ps.cursor = min(ps.cursor+1, max(len(ps.results)-1, 0))
	case "up", "shift+tab", "ctrl+p":
		ps.cursor = max(ps.cursor-1, 0)
	case "backspace":
		if r := []rune(ps.query); len(r) > 0 {
			ps.query = string(r[:len(r)-1])
			ps.cursor = 0
			b.refreshPalette()
		}
	default:
		if k == "space" {
			k = " "
		}
		if len([]rune(k)) == 1 {
			ps.query += k
			ps.cursor = 0
			b.refreshPalette()
		}
	}
}

// drawPalette dims the view behind it and draws the box on top.
func (b *browser) drawPalette(f *frame) {
	ps := &b.palette
	for r := range f.rows {
		for i := range f.rows[r] {
			f.rows[r][i].text = paint(active.Faint, stripANSI(f.rows[r][i].text))
		}
	}
	f.icons = nil
	w := min(86, b.w-6)
	x := (b.w - w) / 2
	top := max(2, b.h/8)
	maxRows := min(10, b.h-top-6)
	border := func(s string) string { return paint(active.From, s) }
	row := top
	put := func(s string) {
		f.put(row, x, border("│")+" "+padRight(truncateANSI(s, w-4), w-4)+" "+border("│"))
		row++
	}
	f.put(row, x, border("╭"+strings.Repeat("─", w-2)+"╮"))
	row++
	caret := paint(pulse(active.Accent, active.Faint), "▏")
	q := paint(active.Text, ps.query) + caret
	if ps.query == "" {
		q = caret + paint(active.Dim, "type a game, an achievement, a friend, or: play / store / theme ...")
	}
	put(bold(active.Accent, "›") + " " + q)
	f.put(row, x, border("├"+strings.Repeat("─", w-2)+"┤"))
	row++
	start := 0
	if ps.cursor >= maxRows {
		start = ps.cursor - maxRows + 1
	}
	for i := 0; i < maxRows; i++ {
		idx := start + i
		if idx >= len(ps.results) {
			put("")
			continue
		}
		it := ps.results[idx]
		kc := palKinds[it.kind]
		kind := paint(kc, padRight(it.kind, 12))
		title := paint(active.Text, it.title)
		if idx == ps.cursor {
			title = bold(active.To, it.title)
			kind = chip(kc, active.Ink, padRight(it.kind, 11))
		}
		right := ""
		if it.key != "" {
			right = paint(active.Dim, it.key)
		}
		body := kind + " " + title
		if it.sub != "" {
			body += paint(active.Dim, "  "+it.sub)
		}
		avail := w - 4 - width(right) - 2
		put(padRight(truncateANSI(body, avail), avail) + "  " + right)
	}
	f.put(row, x, border("├"+strings.Repeat("─", w-2)+"┤"))
	row++
	put(paint(active.Dim, fmt.Sprintf("%d results   ", len(ps.results))) + bold(active.Accent, "↑↓") + paint(active.Dim, " move   ") +
		bold(active.Accent, "enter") + paint(active.Dim, " go   ") + bold(active.Accent, "esc") + paint(active.Dim, " close"))
	f.put(row, x, border("╰"+strings.Repeat("─", w-2)+"╯"))
}

// stripANSI removes escape sequences.
func stripANSI(s string) string {
	if !strings.Contains(s, "\x1b") {
		return s
	}
	var out strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			j := i + 1
			if j < len(s) && s[j] == '[' {
				j++
				for j < len(s) && (s[j] < '@' || s[j] > '~') {
					j++
				}
			}
			i = j
			continue
		}
		out.WriteByte(s[i])
	}
	return out.String()
}
