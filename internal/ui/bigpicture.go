package ui

// Home screen, laid out like Steam's Big Picture mode: a top bar with the
// sections and the clock, a large hero for the focused game with its art
// and a PLAY button, and shelves of tiles you move through with the arrows,
// the way you would with a controller.
//
//	↑ ↓   change shelf (↑ from the first shelf reaches the top bar)
//	← →   move along a shelf
//	enter open      p play now      c today's challenge

import (
	"fmt"
	"hash/fnv"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/tomikav/steam-achievements-cli-/internal/icons"
	"github.com/tomikav/steam-achievements-cli-/internal/model"
)

var shelfTitles = []string{"CONTINUE PLAYING", "ALMOST THERE", "FRIENDS ONLINE"}

// top bar entries and what they open
var navItems = []struct{ label, key string }{
	{"HOME", ""}, {"LIBRARY", "l"}, {"STORE", "s"}, {"FRIENDS", "f"}, {"PROFILE", "m"}, {"SEARCH", "ctrl+k"},
}

const (
	tileW  = 24 // columns per tile, gap included
	tileH  = 6
	shelfH = tileH + 2 // title row, tiles, one blank row
)

func (b *browser) homeKey(k string) {
	hs := &b.home
	if k != "x" {
		b.perf.confirm = false
	}
	if hs.nav {
		switch k {
		case "left", "h", "shift+tab":
			hs.navIdx = (hs.navIdx + len(navItems) - 1) % len(navItems)
		case "right", "l", "tab":
			if k == "l" {
				b.mode, b.full = modeList, true
				return
			}
			hs.navIdx = (hs.navIdx + 1) % len(navItems)
		case "down", "j", "esc":
			hs.nav = false
		case "enter":
			hs.nav = false
			b.openNav(hs.navIdx)
		}
		return
	}
	switch k {
	case "x":
		b.closeSteamKey()
		return
	case "l":
		b.mode, b.full = modeList, true
		return
	case "c":
		if g, a, ok := b.challenge(); ok {
			b.openPlay(g.AppID)
			b.status = "today's challenge: " + achName(a)
		}
		return
	case "d":
		b.back = modeHome
		b.mode, b.scroll, b.full = modeDashboard, 0, true
		return
	case "right", "tab":
		hs.row[hs.col]++
	case "left", "shift+tab", "h":
		hs.row[hs.col]--
	case "down", "j", "wheeldown":
		for n := hs.col + 1; n < len(shelfTitles); n++ {
			if b.homeLen(n) > 0 {
				hs.col = n
				break
			}
		}
	case "up", "k", "wheelup":
		moved := false
		for n := hs.col - 1; n >= 0; n-- {
			if b.homeLen(n) > 0 {
				hs.col, moved = n, true
				break
			}
		}
		if !moved {
			hs.nav = true
		}
	case "enter", "p":
		if hs.col == 2 {
			if fr := b.homeFriends(); hs.row[2] < len(fr) {
				f := fr[hs.row[2]]
				if f.ProfileURL != "" && openTarget(f.ProfileURL) == nil {
					b.status = "opened " + f.Name + "'s profile"
				}
			}
			return
		}
		if g, ok := b.homeFocus(); ok {
			b.openPlay(g.AppID)
			if k == "p" {
				b.playKey("p")
			}
		}
		return
	}
	n := b.homeLen(hs.col)
	hs.row[hs.col] = max(0, min(hs.row[hs.col], n-1))
}

func (b *browser) openNav(i int) {
	switch navItems[i].label {
	case "LIBRARY":
		b.mode, b.full = modeList, true
	case "STORE":
		b.openStore()
	case "FRIENDS":
		b.back, b.mode, b.full = modeHome, modeFriends, true
		b.loadFriendsOnce()
	case "PROFILE":
		b.openProfile()
	case "SEARCH":
		b.openPalette()
	}
}

// homeFocus is the game the hero shows: the focused tile, or for a friend
// tile the game they are playing if you own it, else the first recent game.
func (b *browser) homeFocus() (model.Game, bool) {
	hs := &b.home
	if hs.col < 2 {
		gs := b.homeGames(hs.col)
		if i := hs.row[hs.col]; i < len(gs) {
			return gs[i], true
		}
	} else if fr := b.homeFriends(); hs.row[2] < len(fr) {
		for _, g := range b.games {
			if fr[hs.row[2]].Game != "" && g.Name == fr[hs.row[2]].Game {
				return g, true
			}
		}
	}
	for c := 0; c < 2; c++ {
		if gs := b.homeGames(c); len(gs) > 0 {
			return gs[0], true
		}
	}
	if len(b.games) > 0 {
		return b.games[0], true
	}
	return model.Game{}, false
}

// tileColor gives every game its own tint inside the theme.
func tileColor(id int) string {
	h := fnv.New32a()
	fmt.Fprint(h, id)
	tints := []string{active.From, active.To, active.Accent, active.Good}
	return lerp(lerp(active.Ink, "#1b2838", 0.5), tints[h.Sum32()%uint32(len(tints))], 0.22+float64(h.Sum32()%7)/60)
}

// bg paints text on a background colour.
func bg(back, fore string, boldText bool, s string) string {
	key := "g" + back + fore
	if boldText {
		key = "G" + back + fore
	}
	return styled(key, func() lipgloss.Style {
		st := fg(fore).Background(lipgloss.Color(back))
		if boldText {
			st = st.Bold(true)
		}
		return st
	}, s)
}

// poster draws a capsule for a game without its picture: a tinted panel
// with the name in pixel letters. Works in every terminal.
func poster(name string, id, w, h int, reveal float64) []string {
	base := tileColor(id)
	words := strings.Fields(strings.ToUpper(name))
	// up to two lines of pixel text that fit
	var lines []string
	cur := ""
	for _, wd := range words {
		switch {
		case cur == "":
			cur = wd
		case bigTextWidth(cur+" "+wd) <= w-4:
			cur += " " + wd
		default:
			lines = append(lines, cur)
			cur = wd
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	maxLines := max(1, (h-1)/4)
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	var grids [][5][]bool
	for _, l := range lines {
		grids = append(grids, glyphGrid(fitBigText(l, w-4)))
	}
	textRows := len(grids)*4 - 1
	top := max((h-textRows)/2, 0)
	out := make([]string, h)
	for y := 0; y < h; y++ {
		var sb strings.Builder
		// vertical light: brighter at the top, a soft vignette at the sides
		for x := 0; x < w; x++ {
			t := float64(y)/float64(max(h-1, 1))*0.6 + math.Abs(float64(x)/float64(max(w-1, 1))-0.5)*0.5
			back := lerp(lerp(base, "#ffffff", 0.08), active.Ink, t*0.7)
			ch := " "
			fore := active.Text
			li := (y - top) / 4
			ry := (y - top) % 4
			if y >= top && li < len(grids) && ry < 3 {
				g := grids[li]
				gw := len(g[0])
				gx := x - (w-gw)/2
				if gx >= 0 && gx < gw && float64(gx) < reveal*float64(gw)+0.999 {
					topPix := g[ry*2][gx]
					botPix := ry*2+1 < 5 && g[ry*2+1][gx]
					fore = lerp(active.Text, "#ffffff", 0.5)
					switch {
					case topPix && botPix:
						ch = "█"
					case topPix:
						ch = "▀"
					case botPix:
						ch = "▄"
					}
				}
			}
			sb.WriteString(bg(back, fore, false, ch))
		}
		out[y] = sb.String()
	}
	return out
}

func (b *browser) heroH() int {
	switch {
	case b.h >= 44:
		return 14
	case b.h >= 34:
		return 11
	}
	return 8
}

func (b *browser) buildHome(f *frame) {
	hs := &b.home
	for r := 0; r < b.h-2; r++ {
		f.put(r, 0, strings.Repeat(" ", b.w))
	}
	b.buildTopBar(f)

	// --- hero ------------------------------------------------------------
	heroTop, heroH := 2, b.heroH()
	g, ok := b.homeFocus()
	artW := min(b.w*42/100, 64)
	if ok {
		drawn := false
		if b.icons {
			if spot, ready := b.headerArt(g.AppID, 2, heroTop, artW, heroH); ready {
				f.icons = append(f.icons, spot)
				drawn = true
			}
		}
		if !drawn {
			for i, l := range poster(g.Name, g.AppID, artW, heroH, animT) {
				f.put(heroTop+i, 2, l)
			}
		}
		b.buildHeroInfo(f, g, 2+artW+3, heroTop, b.w-artW-7, heroH, drawn)
	}

	// --- shelves ----------------------------------------------------------
	top := heroTop + heroH + 1
	room := (b.h - 2 - top) / shelfH
	if room < 1 {
		return
	}
	first := 0
	if hs.col >= room {
		first = hs.col - room + 1
	}
	for i := 0; i < room && first+i < len(shelfTitles); i++ {
		b.buildShelf(f, first+i, top+i*shelfH)
	}
}

func (b *browser) buildTopBar(f *frame) {
	hs := &b.home
	line := " "
	for i, it := range navItems {
		label := it.label
		switch {
		case hs.nav && i == hs.navIdx:
			line += chip(active.Accent, active.Ink, label)
		case i == 0 && !hs.nav:
			line += bold(active.To, " "+label+" ")
		default:
			line += paint(active.Dim, " "+label+" ")
		}
		line += "  "
	}
	online := len(b.homeFriends())
	right := ""
	if b.friends.loaded {
		right += paint(active.Good, "●") + paint(active.Dim, fmt.Sprintf(" %d online   ", online))
	}
	if b.perf.known && b.perf.steam > 0 {
		right += paint(active.Dim, "steam ") + paint(active.Warn, mb(b.perf.steam)) + paint(active.Dim, "   ")
	}
	right += bold(active.Text, time.Now().Format("15:04")) + " "
	gap := b.w - width(line) - width(right)
	if gap < 2 {
		line = truncateANSI(line, b.w-width(right)-2)
		gap = b.w - width(line) - width(right)
	}
	f.put(0, 0, line+strings.Repeat(" ", max(gap, 1))+right)
	f.put(1, 0, paint(active.Faint, strings.Repeat("─", b.w)))
}

// headerArt places the game's store header if it is loaded, asking for it
// otherwise.
func (b *browser) headerArt(appID, x, y, w, h int) (iconSpot, bool) {
	pxW := w * b.cellW
	pxH := pxW * 215 / 460
	if pxH > h*b.cellH {
		pxH = h * b.cellH
		pxW = pxH * 460 / 215
	}
	cols := int(math.Ceil(float64(pxW) / float64(b.cellW)))
	rows := int(math.Ceil(float64(pxH) / float64(b.cellH)))
	key := fmt.Sprintf("hero%d@%dx%d", appID, pxW, pxH)
	if img, ok := b.big[key]; ok {
		return iconSpot{row: y, col: x, w: cols, h: rows, key: key, sixel: img}, img != ""
	}
	url := fmt.Sprintf("https://cdn.cloudflare.steamstatic.com/steam/apps/%d/header.jpg", appID)
	b.wantBig = &icons.Request{Key: key, URL: url, W: pxW, H: pxH}
	return iconSpot{}, false
}

func (b *browser) buildHeroInfo(f *frame, g model.Game, x, y, w, h int, art bool) {
	var lines []string
	add := func(l ...string) { lines = append(lines, l...) }
	add(paint(active.Dim, strings.ToUpper(shelfTitles[min(b.home.col, 2)])))
	name := Truncate(g.Name, w)
	if art && bigTextWidth(g.Name) <= w && h >= 12 { // the poster already shows the name in big letters
		add(bigText(g.Name, active.From, active.To, animT)...)
	} else {
		add(bold(active.Text, name))
	}
	add("")
	if hasAch(g) {
		pct := float64(g.Achieved) / float64(g.Total)
		add(meter(pct, min(36, w-16), map[bool]string{true: active.Good, false: active.From}[pct >= 0.9995]) +
			bold(active.Text, fmt.Sprintf("  %d%%", int(pct*100*animT+0.5))) + paint(active.Dim, fmt.Sprintf("   %d / %d achievements", g.Achieved, g.Total)))
		if fc := forecast(g); fc != "" {
			add(paint(active.Dim, fc))
		}
	} else {
		add(paint(active.Dim, "no Steam achievements"))
	}
	detail := formatPlaytime(g.Playtime) + " played"
	if g.LastPlayed > 0 {
		detail += "   last played " + ago(time.Unix(g.LastPlayed, 0))
	}
	add(paint(active.Dim, detail), "")
	// buttons
	play := bg(active.Good, active.Ink, true, "  ▶  PLAY  ")
	if !b.home.nav {
		play = bg(pulse(active.Good, lerp(active.Good, "#ffffff", 0.35)), active.Ink, true, "  ▶  PLAY  ")
	}
	add(play + "   " + bg(lerp(active.Faint, active.Dim, 0.3), active.Text, false, "  ENTER  details  ") +
		"   " + paint(active.Dim, "p plays now"))
	// today's challenge, when there is room
	if cg, a, ok := b.challenge(); ok && len(lines)+4 <= h {
		add("", paint(active.Accent, "◆ TODAY'S CHALLENGE")+paint(active.Dim, "   press c"),
			truncateANSI(bold(active.Text, achName(a))+paint(active.Dim, "  in ")+paint(active.Accent, cg.Name)+
				paint(active.Dim, fmt.Sprintf("  ·  %.0f%% of players have it", a.Global)), w))
	}
	for i, l := range lines {
		if i >= h {
			break
		}
		f.put(y+i, x, truncateANSI(l, w))
	}
}

func (b *browser) buildShelf(f *frame, shelf, y int) {
	hs := &b.home
	focused := hs.col == shelf && !hs.nav
	n := b.homeLen(shelf)
	title := paint(active.Dim, shelfTitles[shelf])
	if focused {
		title = gradient(shelfTitles[shelf])
	}
	f.put(y, 2, title+paint(active.Faint, fmt.Sprintf("   %d", n)))
	if n == 0 {
		msg := "nothing here yet"
		if shelf == 2 && !b.friends.loaded {
			msg = b.spin() + " checking friends"
		}
		f.put(y+2, 4, paint(active.Dim, msg))
		return
	}
	per := max(1, (b.w-4)/tileW)
	cur := hs.row[shelf]
	start := 0
	if cur >= per {
		start = cur - per + 1
	}
	games := b.homeGames(shelf)
	friends := b.homeFriends()
	for i := 0; i < per && start+i < n; i++ {
		idx := start + i
		x := 2 + i*tileW
		sel := focused && idx == cur
		var rows []string
		if shelf == 2 {
			rows = friendTile(friends[idx], tileW-2, sel)
		} else {
			rows = b.gameTile(games[idx], tileW-2, sel)
		}
		lift := 0
		if sel {
			lift = -1 // the focused tile rises like in Big Picture
		}
		for r, l := range rows {
			f.put(y+1+r+lift+1, x, l)
		}
	}
	if start+per < n {
		f.put(y+1+tileH/2, b.w-2, paint(active.Dim, "›"))
	}
	if start > 0 {
		f.put(y+1+tileH/2, 0, paint(active.Dim, "‹"))
	}
}

func (b *browser) gameTile(g model.Game, w int, sel bool) []string {
	base := tileColor(g.AppID)
	if sel {
		base = lerp(base, active.Accent, 0.35)
	}
	fill := func(s string, fore string, boldText bool) string {
		return bg(base, fore, boldText, padRight(" "+s, w))
	}
	name := wrap(g.Name, w-2, 2)
	for len(name) < 2 {
		name = append(name, "")
	}
	rows := []string{fill("", active.Text, false), fill(name[0], active.Text, true), fill(name[1], active.Text, true)}
	if hasAch(g) {
		pct := float64(g.Achieved) / float64(g.Total)
		cells := int(math.Round(pct * float64(w-2) * animT))
		bar := bg(base, active.Good, false, " "+strings.Repeat("▀", cells)) + bg(base, lerp(base, "#000000", 0.4), false, strings.Repeat("▀", max(w-2-cells, 0))+" ")
		rows = append(rows, bar, fill(fmt.Sprintf("%d%%  ·  %d left", int(pct*100+0.5), g.Total-g.Achieved), lerp(active.Text, base, 0.25), false))
	} else {
		rows = append(rows, fill("", active.Text, false), fill(formatPlaytime(g.Playtime)+" played", lerp(active.Text, base, 0.25), false))
	}
	if sel {
		rows = append(rows, paint(active.Accent, strings.Repeat("▀", w)))
	} else {
		rows = append(rows, strings.Repeat(" ", w))
	}
	return rows
}

func friendTile(fr model.Friend, w int, sel bool) []string {
	base := lerp(active.Ink, "#1b2838", 0.6)
	if sel {
		base = lerp(base, active.Accent, 0.3)
	}
	fill := func(s, fore string, boldText bool) string {
		return bg(base, fore, boldText, padRight(" "+Truncate(s, w-2), w))
	}
	state, col := "online", active.From
	if fr.Game != "" {
		state, col = fr.Game, active.Good
	} else if fr.State == 3 || fr.State == 4 {
		state, col = "away", active.Warn
	}
	rows := []string{fill("", active.Text, false), fill(fr.Name, active.Text, true), fill("", active.Text, false),
		bg(base, col, false, padRight(" ● "+Truncate(state, w-4), w)), fill("", active.Text, false)}
	if sel {
		rows = append(rows, paint(active.Accent, strings.Repeat("▀", w)))
	} else {
		rows = append(rows, strings.Repeat(" ", w))
	}
	return rows
}
