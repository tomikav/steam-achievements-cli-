package ui

// Interactive library browser: `steam browse`.
//
// Built directly on the terminal (raw mode, alternate screen, VT input and
// SGR mouse) instead of a TUI framework, so it can draw Sixel icons that
// survive redraws. The state machine (browser) is pure and unit tested; the
// screen type only turns frames into escape sequences.

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/x/term"

	"github.com/tomikav/steam-achievements-cli-/internal/icons"
	"github.com/tomikav/steam-achievements-cli-/internal/launcher"
	"github.com/tomikav/steam-achievements-cli-/internal/model"
)

// BrowseOptions configures the interactive browser.
type BrowseOptions struct {
	Owner   string
	SteamID string
	Games   []model.Game
	Icons   bool
	Demo    bool
	// LoadAchievements returns the full achievement list of one game.
	LoadAchievements func(appID int) ([]model.Achievement, error)
	// LoadFriends returns the friends list with live status.
	LoadFriends func() ([]model.Friend, error)
	// LoadStore returns a game's public store details.
	LoadStore func(appID int) (model.StoreInfo, error)
}

// --- state --------------------------------------------------------------------

type browseMode int

const (
	modeList browseMode = iota
	modeGame
	modeDashboard
	modeHelp
	modeFriends
	modePlay
	modeHome
)

var listTabs = []string{"ALL", "IN PROGRESS", "ALMOST", "PERFECT", "NOT STARTED", "NO ACHIEVEMENTS"}
var listSorts = []string{"completion", "remaining", "playtime", "recent", "name", "size"}
var achTabs = []string{"ALL", "LOCKED", "UNLOCKED"}
var achSorts = []string{"smart", "rarity", "name", "unlocked"}
var themeOrder = []string{"steam", "hacker", "gamer", "synthwave", "sunset", "frost", "mono"}

type achState struct {
	appID   int
	loading bool
	err     error
	list    []model.Achievement
	view    []int
	tab     int
	sort    int
	cursor  int
	offset  int
}

type friendsState struct {
	loading bool
	loaded  bool
	err     error
	list    []model.Friend
	cursor  int
	offset  int
}

type browser struct {
	opt       BrowseOptions
	games     []model.Game // only games with achievements, Percent filled in
	view      []int
	tab       int
	sort      int
	desc      bool // reverse order
	query     string
	search    bool
	cursor    int
	offset    int
	mode      browseMode
	prev      browseMode
	scroll    int // dashboard / help scroll
	w, h      int
	ach       achState
	cache     map[int][]model.Achievement
	icons     bool
	small     map[string]string // sixel by key, 18 px
	big       map[string]string // sixel by key, 36 px
	status    string
	frameN    int
	lastClick struct {
		row int
		at  time.Time
	}
	// requests for the I/O loop
	wantAch         int
	wantBig         *icons.Request
	wantFriends     bool
	play            playState
	wantStore       int
	wantInstall     int
	wantClose       int
	wantRunning     bool
	wantAchIconsFor int
	gameBack        browseMode
	playBack        browseMode
	back            browseMode // where friends and the dashboard return to
	home            homeState
	cellW, cellH    int
	cellKnown       bool
	iconsStarted    bool
	wantAvatars     bool
	friends         friendsState
	wantAchIcons    bool
	quit            bool
	full            bool // next draw must clear the screen
}

func newBrowser(opt BrowseOptions, w, h int) *browser {
	b := &browser{opt: opt, w: w, h: h, cache: map[int][]model.Achievement{},
		small: map[string]string{}, big: map[string]string{}, icons: opt.Icons, full: true, cellW: 8, cellH: 16}
	for _, g := range opt.Games {
		if isExtra(g.Name) {
			continue
		}
		if hasAch(g) {
			g.Percent = float64(g.Achieved) / float64(g.Total) * 100
		}
		b.games = append(b.games, g)
	}
	b.refilter()
	b.mode = modeHome
	if opt.LoadFriends != nil {
		b.friends.loading, b.wantFriends = true, true
	}
	return b
}

// smallPx is the size of one-row icons: as tall as a cell (minus a pixel so
// nothing spills into the next row) and at most two cells wide.
func (b *browser) smallPx() int { return max(8, min(b.cellH-1, 2*b.cellW-1, 32)) }

// bigPx is the size of two-row icons.
func (b *browser) bigPx() int { return max(12, min(2*b.cellH-2, 4*b.cellW-2, 64)) }

func hasAch(g model.Game) bool { return g.HasStats && g.Total > 0 }

var extraRe = regexp.MustCompile(`(?i)(\bdemo\b|\bprologue demo\b|dedicated server|\bsoundtrack\b|\bost\b|\bsdk\b|\bbenchmark\b|\bpublic test\b|\btest server\b)`)

// isExtra reports library entries that are not games to play: demos,
// dedicated servers, soundtracks, SDKs and benchmarks.
func isExtra(name string) bool { return extraRe.MatchString(name) }

func (b *browser) inTab(g model.Game, tab int) bool {
	p := g.Percent
	if tab == 5 {
		return !hasAch(g)
	}
	if tab > 0 && !hasAch(g) {
		return false
	}
	switch tab {
	case 1:
		return p > 0 && p < 99.95
	case 2:
		return p >= 75 && p < 99.95
	case 3:
		return p >= 99.95
	case 4:
		return g.Achieved == 0
	}
	return true
}

func (b *browser) tabCount(tab int) int {
	n := 0
	for _, g := range b.games {
		if b.inTab(g, tab) {
			n++
		}
	}
	return n
}

// refilter rebuilds the visible list, keeping the selected game if possible.
func (b *browser) refilter() {
	selected := -1
	if b.cursor < len(b.view) {
		selected = b.games[b.view[b.cursor]].AppID
	}
	q := strings.ToLower(b.query)
	b.view = b.view[:0]
	for i, g := range b.games {
		if !b.inTab(g, b.tab) {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(g.Name), q) {
			continue
		}
		b.view = append(b.view, i)
	}
	less := func(x, y model.Game) bool {
		if s := listSorts[b.sort]; (s == "completion" || s == "remaining" || s == "size") && hasAch(x) != hasAch(y) {
			return hasAch(x)
		}
		switch listSorts[b.sort] {
		case "remaining":
			rx, ry := x.Total-x.Achieved, y.Total-y.Achieved
			if (rx == 0) != (ry == 0) {
				return ry == 0
			}
			if rx != ry {
				return rx < ry
			}
			return x.Percent > y.Percent
		case "playtime":
			return x.Playtime > y.Playtime
		case "recent":
			return x.LastPlayed > y.LastPlayed
		case "name":
			return strings.ToLower(x.Name) < strings.ToLower(y.Name)
		case "size":
			return x.Total > y.Total
		}
		if x.Percent != y.Percent {
			return x.Percent > y.Percent
		}
		return x.Playtime > y.Playtime
	}
	sort.SliceStable(b.view, func(i, j int) bool {
		x, y := b.games[b.view[i]], b.games[b.view[j]]
		if b.desc {
			return less(y, x)
		}
		return less(x, y)
	})
	b.cursor = 0
	for i, gi := range b.view {
		if b.games[gi].AppID == selected {
			b.cursor = i
			break
		}
	}
	b.clamp()
}

func (b *browser) listHeight() int { return max(b.h-6, 1) }

func (b *browser) clamp() {
	clampScroll(&b.cursor, &b.offset, len(b.view), b.listHeight())
}

func clampScroll(cursor, offset *int, n, height int) {
	if *cursor >= n {
		*cursor = n - 1
	}
	if *cursor < 0 {
		*cursor = 0
	}
	if *cursor < *offset {
		*offset = *cursor
	}
	if *cursor >= *offset+height {
		*offset = *cursor - height + 1
	}
	if *offset > max(n-height, 0) {
		*offset = max(n-height, 0)
	}
	if *offset < 0 {
		*offset = 0
	}
}

func (b *browser) selected() (model.Game, bool) {
	if b.cursor < len(b.view) {
		return b.games[b.view[b.cursor]], true
	}
	return model.Game{}, false
}

// --- achievements view state -----------------------------------------------------

func (b *browser) openGame() {
	g, ok := b.selected()
	if !ok {
		return
	}
	b.mode = modeGame
	b.full = true
	b.ach = achState{appID: g.AppID}
	if list, ok := b.cache[g.AppID]; ok {
		b.setAchievements(g.AppID, list, nil)
		return
	}
	b.ach.loading = true
	b.wantAch = g.AppID
}

func (b *browser) setAchievements(appID int, list []model.Achievement, err error) {
	if err == nil {
		b.cache[appID] = list
	}
	if b.ach.appID != appID {
		return
	}
	b.ach.loading = false
	b.ach.err = err
	b.ach.list = list
	b.refilterAch()
	b.wantAchIcons = true
}

func (b *browser) refilterAch() {
	a := &b.ach
	keep := ""
	if a.cursor < len(a.view) && a.view[a.cursor] < len(a.list) {
		keep = a.list[a.view[a.cursor]].Name
	}
	a.view = a.view[:0]
	for i, x := range a.list {
		if (a.tab == 1 && x.Achieved) || (a.tab == 2 && !x.Achieved) {
			continue
		}
		a.view = append(a.view, i)
	}
	sort.SliceStable(a.view, func(i, j int) bool {
		x, y := a.list[a.view[i]], a.list[a.view[j]]
		switch achSorts[a.sort] {
		case "rarity":
			return x.Global < y.Global
		case "name":
			return strings.ToLower(x.DisplayName) < strings.ToLower(y.DisplayName)
		case "unlocked":
			return x.UnlockTime > y.UnlockTime
		}
		// smart: locked first, easiest to get first; then unlocked, rarest first
		if x.Achieved != y.Achieved {
			return !x.Achieved
		}
		if !x.Achieved {
			return x.Global > y.Global
		}
		return x.Global < y.Global
	})
	a.cursor = 0
	for i, vi := range a.view {
		if a.list[vi].Name == keep {
			a.cursor = i
		}
	}
	if a.cursor == 0 {
		a.offset = 0
	}
}

// --- input ----------------------------------------------------------------------------

// handleKey applies one key and reports whether the screen changed.
func (b *browser) handleKey(k string) {
	if b.search {
		switch k {
		case "enter", "down", "up":
			b.search = false
			if k != "enter" {
				b.handleKey(k)
			}
		case "esc":
			b.search = false
			b.query = ""
			b.refilter()
		case "backspace":
			if r := []rune(b.query); len(r) > 0 {
				b.query = string(r[:len(r)-1])
				b.refilter()
			}
		case "ctrl+c":
			b.quit = true
		default:
			if utf8.RuneCountInString(k) == 1 || k == "space" {
				if k == "space" {
					k = " "
				}
				b.query += k
				b.refilter()
			}
		}
		return
	}

	switch k {
	case "q", "ctrl+c":
		b.quit = true
		return
	case "?":
		if b.mode == modeHelp {
			b.mode = b.prev
		} else {
			b.prev, b.mode, b.scroll = b.mode, modeHelp, 0
		}
		b.full = true
		return
	case "t":
		for i, n := range themeOrder {
			if n == active.Name {
				_ = SetTheme(themeOrder[(i+1)%len(themeOrder)])
				break
			}
		}
		b.status = "theme: " + active.Name
		b.full = true
		return
	case "f":
		if b.mode == modePlay {
			break
		}
		if b.mode == modeFriends {
			b.mode = b.back
		} else {
			b.back = b.mode
			if b.back != modeHome {
				b.back = modeList
			}
			b.mode = modeFriends
			if !b.friends.loaded && !b.friends.loading {
				b.friends.loading = true
				b.wantFriends = true
			}
		}
		b.full = true
		return
	case "p":
		if b.mode == modePlay {
			break
		}
		if b.mode == modeGame {
			b.openPlay(b.ach.appID)
			return
		}
		if g, ok := b.selected(); ok && b.mode == modeList {
			b.openPlay(g.AppID)
		}
		return
	case "i":
		if b.opt.Icons {
			b.icons = !b.icons
			b.full = true
			b.status = map[bool]string{true: "icons on", false: "icons off"}[b.icons]
		}
		return
	}

	switch b.mode {
	case modeHelp, modeDashboard:
		switch k {
		case "esc", "backspace", "d":
			if b.mode == modeHelp {
				b.mode = b.prev
			} else {
				b.mode = b.back
			}
			b.full = true
		case "down", "j", "wheeldown":
			b.scroll++
		case "up", "k", "wheelup":
			b.scroll = max(b.scroll-1, 0)
		case "pgdown":
			b.scroll += b.listHeight()
		case "pgup":
			b.scroll = max(b.scroll-b.listHeight(), 0)
		case "home", "g":
			b.scroll = 0
		}
	case modeGame:
		b.gameKey(k)
	case modeFriends:
		b.friendsKey(k)
	case modePlay:
		b.playKey(k)
	case modeHome:
		b.homeKey(k)
	default:
		b.listKey(k)
	}
}

func (b *browser) listKey(k string) {
	h := b.listHeight()
	switch k {
	case "down", "j", "wheeldown":
		b.cursor++
	case "up", "k", "wheelup":
		b.cursor--
	case "pgdown", "ctrl+d":
		b.cursor += h
	case "pgup", "ctrl+u":
		b.cursor -= h
	case "home", "g":
		b.cursor = 0
	case "end", "G":
		b.cursor = len(b.view) - 1
	case "right", "tab", "l":
		b.tab = (b.tab + 1) % len(listTabs)
		b.refilter()
	case "left", "shift+tab", "h":
		b.tab = (b.tab + len(listTabs) - 1) % len(listTabs)
		b.refilter()
	case "1", "2", "3", "4", "5", "6":
		b.tab = int(k[0] - '1')
		b.refilter()
	case "s":
		b.sort = (b.sort + 1) % len(listSorts)
		b.desc = false
		b.refilter()
		b.status = "sorted by " + listSorts[b.sort]
	case "r":
		b.desc = !b.desc
		b.refilter()
	case "/":
		b.search = true
	case "esc":
		if b.query != "" {
			b.query = ""
			b.refilter()
		} else {
			b.mode, b.full = modeHome, true
		}
	case "enter":
		if g, ok := b.selected(); ok {
			b.openPlay(g.AppID)
		}
	case "d":
		b.back = modeList
		b.mode, b.scroll, b.full = modeDashboard, 0, true
	case "o":
		if g, ok := b.selected(); ok {
			b.openURL(g.AppID)
		}
	}
	b.clamp()
}

func (b *browser) gameKey(k string) {
	switch k {
	case "esc", "backspace":
		b.mode, b.full = b.gameBack, true
		b.gameBack = modeList
		return
	case "o":
		b.openURL(b.ach.appID)
		return
	}
	b.achNav(k, b.listHeight()-1)
}

// achNav handles moving, filtering and sorting in an achievement list that
// shows h rows. It is shared by the full list and the game view.
func (b *browser) achNav(k string, h int) bool {
	a := &b.ach
	switch k {
	case "down", "j", "wheeldown":
		a.cursor++
	case "up", "k", "wheelup":
		a.cursor--
	case "pgdown":
		a.cursor += h
	case "pgup":
		a.cursor -= h
	case "home", "g":
		a.cursor = 0
	case "end", "G":
		a.cursor = len(a.view) - 1
	case "right", "tab", "l":
		a.tab = (a.tab + 1) % len(achTabs)
		b.refilterAch()
	case "left", "shift+tab", "h":
		a.tab = (a.tab + len(achTabs) - 1) % len(achTabs)
		b.refilterAch()
	case "1", "2", "3":
		a.tab = int(k[0] - '1')
		b.refilterAch()
	case "s":
		a.sort = (a.sort + 1) % len(achSorts)
		b.refilterAch()
		b.status = "sorted by " + achSorts[a.sort]
	default:
		return false
	}
	clampScroll(&a.cursor, &a.offset, len(a.view), max(h, 1))
	return true
}

// Friends grid geometry: 5 columns (fewer on narrow terminals), 4 rows per
// card, starting at screen row 4.
func (b *browser) friendCols() int { return max(1, min(5, (b.w-2)/26)) }

func (b *browser) friendRows() int { return max(1, (b.h-6)/4) }

func (b *browser) friendsKey(k string) {
	fr := &b.friends
	cols, rows := b.friendCols(), b.friendRows()
	switch k {
	case "esc", "backspace":
		b.mode, b.full = b.back, true
		return
	case "right", "l", "tab":
		fr.cursor++
	case "left", "h", "shift+tab":
		fr.cursor--
	case "down", "j", "wheeldown":
		fr.cursor += cols
	case "up", "k", "wheelup":
		fr.cursor -= cols
	case "pgdown":
		fr.cursor += cols * rows
	case "pgup":
		fr.cursor -= cols * rows
	case "home", "g":
		fr.cursor = 0
	case "end", "G":
		fr.cursor = len(fr.list) - 1
	case "enter", "o":
		if fr.cursor < len(fr.list) && fr.list[fr.cursor].ProfileURL != "" {
			if err := openTarget(fr.list[fr.cursor].ProfileURL); err == nil {
				b.status = "opened " + fr.list[fr.cursor].Name + "'s profile"
			}
		}
	case "R":
		fr.loading, b.wantFriends = true, true
	}
	b.clampFriends()
}

// clickFriend selects the card under the mouse; a second click opens the
// profile.
func (b *browser) clickFriend(row, col int) {
	fr := &b.friends
	cols := b.friendCols()
	cardW := (b.w - 2) / cols
	r, c := (row-4)/4, (col-1)/cardW
	if row < 4 || c < 0 || c >= cols {
		return
	}
	i := (fr.offset+r)*cols + c
	if i >= len(fr.list) {
		return
	}
	double := b.lastClick.row == i && time.Since(b.lastClick.at) < 450*time.Millisecond
	b.lastClick.row, b.lastClick.at = i, time.Now()
	fr.cursor = i
	if double {
		b.friendsKey("enter")
	}
}

// clampFriends keeps the cursor valid and its card row on screen. offset
// counts card rows.
func (b *browser) clampFriends() {
	fr := &b.friends
	cols, rows := b.friendCols(), b.friendRows()
	if fr.cursor >= len(fr.list) {
		fr.cursor = len(fr.list) - 1
	}
	if fr.cursor < 0 {
		fr.cursor = 0
	}
	r := fr.cursor / cols
	if r < fr.offset {
		fr.offset = r
	}
	if r >= fr.offset+rows {
		fr.offset = r - rows + 1
	}
}

func (b *browser) setFriends(list []model.Friend, err error) {
	fr := &b.friends
	fr.loading = false
	fr.err = err
	if err != nil {
		return
	}
	fr.loaded = true
	rank := func(f model.Friend) int {
		switch {
		case f.Game != "":
			return 0
		case f.State == 1 || f.State >= 5:
			return 1
		case f.State > 0:
			return 2
		}
		return 3
	}
	sort.SliceStable(list, func(i, j int) bool {
		ri, rj := rank(list[i]), rank(list[j])
		if ri != rj {
			return ri < rj
		}
		if ri == 3 {
			return list[i].LastLogoff > list[j].LastLogoff
		}
		return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name)
	})
	fr.list = list
	fr.cursor, fr.offset = 0, 0
	b.wantAvatars = true
}

// openTarget opens a URL (https:// or steam://) with the system handler.
func openTarget(url string) error { return launcher.Open(url) }

// click selects the row under the mouse; a second click on it opens it.
func (b *browser) click(row int) {
	top := 4
	if b.mode == modeGame || b.mode == modeFriends {
		top = 5
	}
	idx := row - top
	if idx < 0 {
		return
	}
	double := b.lastClick.row == row && time.Since(b.lastClick.at) < 450*time.Millisecond
	b.lastClick.row, b.lastClick.at = row, time.Now()
	switch b.mode {
	case modeList:
		if idx < b.listHeight() && b.offset+idx < len(b.view) {
			b.cursor = b.offset + idx
			if double {
				b.openPlay(b.games[b.view[b.cursor]].AppID)
			}
		}
	case modeGame:
		if b.ach.offset+idx < len(b.ach.view) {
			b.ach.cursor = b.ach.offset + idx
		}
	case modeFriends:
		// handled by clickFriend, which needs the column too
	}
}

func (b *browser) openURL(appID int) {
	url := fmt.Sprintf("https://store.steampowered.com/app/%d", appID)
	if b.opt.SteamID != "" {
		url = fmt.Sprintf("https://steamcommunity.com/profiles/%s/stats/%d/achievements", b.opt.SteamID, appID)
	}
	if b.opt.Demo {
		b.status = "demo mode: nothing to open"
		return
	}
	if err := openTarget(url); err != nil {
		b.status = "could not open browser: " + err.Error()
		return
	}
	b.status = "opened in your browser"
}

// --- frame building ---------------------------------------------------------------------

type seg struct {
	col  int
	text string
}

type iconSpot struct {
	row, col int
	w, h     int // cells covered; cleared before drawing
	key      string
	sixel    string
}

type frame struct {
	rows  [][]seg
	icons []iconSpot
}

func (f *frame) put(row, col int, text string) {
	if row >= 0 && row < len(f.rows) {
		f.rows[row] = append(f.rows[row], seg{col, text})
	}
}

func (b *browser) build() frame {
	f := frame{rows: make([][]seg, b.h)}
	switch b.mode {
	case modeGame:
		b.buildGame(&f)
	case modeDashboard:
		b.buildScrollText(&f, "DASHBOARD", strings.Split(strings.TrimRight(renderStats(b.games, b.opt.Owner, b.w), "\n"), "\n"))
	case modeHelp:
		b.buildScrollText(&f, "KEYS", helpLines())
	case modeFriends:
		b.buildFriends(&f)
	case modePlay:
		b.buildPlay(&f)
	case modeHome:
		b.buildHome(&f)
	default:
		b.buildList(&f)
	}
	b.buildFooter(&f)
	return f
}

func (b *browser) header(f *frame, right string) {
	left := " " + gradient("STEAM ACHIEVEMENTS")
	if b.opt.Owner != "" {
		left += "  " + paint(active.Dim, b.opt.Owner)
	} else if b.opt.Demo {
		left += "  " + paint(active.Dim, "demo library")
	}
	gap := b.w - width(left) - width(right) - 1
	f.put(0, 0, left+strings.Repeat(" ", max(gap, 1))+right)
}

func (b *browser) buildList(f *frame) {
	s := summarize(b.opt.Games)
	b.header(f, paint(active.Text, fmt.Sprint(s.tracked))+paint(active.Dim, " games  ")+
		paint(active.Text, fmt.Sprint(s.perfect))+paint(active.Dim, " perfect  ")+
		paint(active.Text, fmt.Sprintf("%.1f%%", s.average))+paint(active.Dim, " average"))

	// tabs
	var tabs strings.Builder
	tabs.WriteString(" ")
	for i, t := range listTabs {
		label := fmt.Sprintf("%s %d", t, b.tabCount(i))
		if i == b.tab {
			tabs.WriteString(chip(active.Accent, active.Ink, label))
		} else {
			tabs.WriteString(paint(active.Dim, " "+label+" "))
		}
		tabs.WriteString(" ")
	}
	f.put(1, 0, padRight(tabs.String(), b.w))

	// search + sort
	var line string
	switch {
	case b.search:
		cursor := paint(active.Accent, "█")
		line = " " + bold(active.Accent, "/") + " " + paint(active.Text, b.query) + cursor
	case b.query != "":
		line = " " + paint(active.Dim, "filter ") + bold(active.Text, b.query) + paint(active.Dim, "   esc to clear")
	default:
		line = " " + paint(active.Dim, "/ to search")
	}
	dir := "▼"
	if b.desc {
		dir = "▲"
	}
	sortLabel := paint(active.Dim, "sort ") + paint(active.Text, listSorts[b.sort]) + " " + paint(active.Accent, dir) +
		paint(active.Dim, fmt.Sprintf("   %d shown ", len(b.view)))
	f.put(2, 0, line+strings.Repeat(" ", max(b.w-width(line)-width(sortLabel), 1))+sortLabel)
	f.put(3, 0, paint(active.Faint, strings.Repeat("─", b.w)))

	paneW := 0
	if b.w >= 118 {
		paneW = 44
	}
	listW := b.w - paneW
	h := b.listHeight()

	if len(b.view) == 0 {
		msg := "No games here."
		if b.query != "" {
			msg = fmt.Sprintf("No games match %q.", b.query)
		}
		f.put(4+h/2-1, 0, padRight(strings.Repeat(" ", max((listW-len(msg))/2, 0))+paint(active.Dim, msg), listW))
	}

	iconW := 0
	if b.icons {
		iconW = 3
	}
	barW := min(max(listW/6, 8), 22)
	chipW := 11
	if listW < 90 {
		chipW = 0
	}
	nameW := listW - (2 + iconW + 1 + barW + 2 + 5 + 2 + 7 + chipW + 1)
	if nameW < 10 {
		nameW = 10
	}

	for r := 0; r < h; r++ {
		row := 4 + r
		i := b.offset + r
		if i >= len(b.view) {
			f.put(row, 0, strings.Repeat(" ", listW))
			continue
		}
		g := b.games[b.view[i]]
		sel := i == b.cursor
		marker := "  "
		name := paint(active.Text, padRight(Truncate(g.Name, nameW), nameW))
		if sel {
			marker = paint(active.Accent, "▌") + " "
			name = bold(active.To, padRight(Truncate(g.Name, nameW), nameW))
		} else if g.Percent >= 99.95 {
			name = bold(active.Text, padRight(Truncate(g.Name, nameW), nameW))
		}
		var rest string
		if hasAch(g) {
			rest = name + " " + Bar(g.Percent, barW) + "  " +
				bold(pctColor(g.Percent), padLeft(fmt.Sprintf("%.0f%%", math.Floor(g.Percent)), 5)) + "  " +
				paint(active.Dim, padRight(fmt.Sprintf("%d/%d", g.Achieved, g.Total), 7))
			if chipW > 0 {
				rest += " " + padRight(tierChip(g.Percent), chipW-1)
			}
		} else {
			rest = name + " " + paint(active.Faint, padRight("no achievements", barW)) + "  " +
				paint(active.Dim, padLeft(formatPlaytime(g.Playtime), 14))
		}
		rest = padRight(rest, listW-2-iconW)
		key := "g" + strconv.Itoa(g.AppID)
		if img, ok := b.small[key]; ok && b.icons {
			f.put(row, 0, marker)
			f.icons = append(f.icons, iconSpot{row: row, col: 2, w: 2, h: 1, key: key, sixel: img})
			f.put(row, 4, " "+rest) // includes the gap after the icon
		} else {
			f.put(row, 0, marker+strings.Repeat(" ", iconW)+rest)
		}
	}

	if paneW > 0 {
		b.buildPane(f, listW, paneW, h)
	}
}

// buildPane draws the detail card of the selected game.
func (b *browser) buildPane(f *frame, x, w, h int) {
	inner := w - 4
	lines := make([]string, h-2)
	g, ok := b.selected()
	iconRows := 0
	if ok {
		nameCol := 0
		key := "g" + strconv.Itoa(g.AppID)
		if b.icons {
			if img, ok := b.big[key]; ok {
				if img != "" {
					f.icons = append(f.icons, iconSpot{row: 5, col: x + 2, w: 4, h: 2, key: key + "@big", sixel: img})
					nameCol = 5
					iconRows = 2
				}
			} else {
				b.wantBig = &icons.Request{Key: key, AppID: g.AppID, Hash: g.Icon, Size: b.bigPx()}
			}
		}
		nameLines := wrap(g.Name, inner-nameCol, 2)
		for i := 0; i < 2; i++ {
			t := ""
			if i < len(nameLines) {
				t = bold(active.Text, nameLines[i])
			}
			lines[i] = t
		}
		pct := fmt.Sprintf("%.1f%%", g.Percent)
		stat := func(k, v string) string { return paint(active.Dim, padRight(k, 13)) + paint(active.Text, v) }
		body := []string{
			"",
			Bar(g.Percent, inner),
			gradient(pct) + "  " + tierChip(g.Percent),
			"",
			stat("unlocked", fmt.Sprintf("%d of %d", g.Achieved, g.Total)),
			stat("remaining", fmt.Sprint(g.Total-g.Achieved)),
			stat("playtime", formatPlaytime(g.Playtime)),
			stat("last played", formatDate(g.LastPlayed)),
			stat("app id", fmt.Sprint(g.AppID)),
			"",
			paint(active.Accent, "enter") + paint(active.Dim, "  open game"),
			paint(active.Accent, "p    ") + paint(active.Dim, "  game view, launch from there"),
		}
		if !hasAch(g) {
			body = []string{
				"",
				paint(active.Dim, "This game has no achievements."),
				"",
				stat("playtime", formatPlaytime(g.Playtime)),
				stat("last played", formatDate(g.LastPlayed)),
				stat("app id", fmt.Sprint(g.AppID)),
				"",
				paint(active.Accent, "enter") + paint(active.Dim, "  open game"),
			}
		}
		for i, l := range body {
			if 2+i < len(lines) {
				lines[2+i] = l
			}
		}
	}
	border := func(s string) string { return paint(lerp(active.Faint, active.Dim, 0.45), s) }
	f.put(4, x, border("╭"+strings.Repeat("─", w-2)+"╮"))
	for i := 0; i < h-2; i++ {
		row := 5 + i
		if i < iconRows {
			// leave 4 cells free for the big icon
			f.put(row, x, border("│")+" ")
			f.put(row, x+6, " "+padRight(lines[i], inner-5)+" "+border("│"))
			continue
		}
		f.put(row, x, border("│")+" "+padRight(lines[i], inner)+" "+border("│"))
	}
	f.put(4+h-1, x, border("╰"+strings.Repeat("─", w-2)+"╯"))
}

func (b *browser) buildGame(f *frame) {
	a := &b.ach
	var g model.Game
	for _, x := range b.games {
		if x.AppID == a.appID {
			g = x
		}
	}
	right := bold(pctColor(g.Percent), fmt.Sprintf("%.1f%%", g.Percent)) + " " + tierChip(g.Percent) + " "
	f.put(0, 0, " "+paint(active.Dim, "esc ← ")+gradient(Truncate(g.Name, b.w-30))+strings.Repeat(" ", max(b.w-width(" esc ← ")-width(Truncate(g.Name, b.w-30))-width(right), 1))+right)
	f.put(1, 0, " "+Bar(g.Percent, max(b.w-24, 10))+"  "+paint(active.Dim, fmt.Sprintf("%d / %d unlocked", g.Achieved, g.Total)))

	var tabs strings.Builder
	tabs.WriteString(" ")
	for i, t := range achTabs {
		n := 0
		for _, x := range a.list {
			if i == 0 || (i == 1) != x.Achieved {
				n++
			}
		}
		label := fmt.Sprintf("%s %d", t, n)
		if i == a.tab {
			tabs.WriteString(chip(active.Accent, active.Ink, label))
		} else {
			tabs.WriteString(paint(active.Dim, " "+label+" "))
		}
		tabs.WriteString(" ")
	}
	sortLabel := paint(active.Dim, "sort ") + paint(active.Text, achSorts[a.sort]) + " "
	f.put(2, 0, tabs.String()+strings.Repeat(" ", max(b.w-width(tabs.String())-width(sortLabel), 1))+sortLabel)
	f.put(3, 0, paint(active.Faint, strings.Repeat("─", b.w)))

	h := b.listHeight() - 1
	switch {
	case a.loading:
		spin := spinnerFrames[b.frameN%len(spinnerFrames)]
		f.put(4+h/2, 0, padRight(strings.Repeat(" ", max(b.w/2-14, 0))+paint(active.Accent, spin)+" "+paint(active.Text, "Loading achievements..."), b.w))
		return
	case a.err != nil:
		f.put(4+h/2, 0, padRight("  "+chip(active.Bad, active.Ink, "ERROR")+" "+paint(active.Text, a.err.Error()), b.w))
		return
	}

	paneW := 0
	if b.w >= 118 {
		paneW = 44
	}
	listW := b.w - paneW
	iconW := 0
	if b.icons {
		iconW = 3
	}
	dateW := 13
	nameW := listW - (2 + iconW + 2 + 1 + 7 + 2 + 12 + dateW + 1)
	if nameW < 12 {
		nameW = 12
	}
	f.put(4, 0, padRight(paint(active.Dim, "  "+strings.Repeat(" ", iconW)+"   "+padRight("ACHIEVEMENT", nameW)+"  GLOBAL  RARITY       UNLOCKED"), listW))
	for r := 0; r < h; r++ {
		row := 5 + r
		i := a.offset + r
		if i >= len(a.view) {
			f.put(row, 0, strings.Repeat(" ", listW))
			continue
		}
		x := a.list[a.view[i]]
		sel := i == a.cursor
		marker := "  "
		mark := paint(active.Faint, "□")
		nameText := padRight(Truncate(achName(x), nameW), nameW)
		name := paint(active.Dim, nameText)
		if x.Achieved {
			mark = paint(active.Good, "■")
			name = paint(active.Text, nameText)
		}
		if sel {
			marker = paint(active.Accent, "▌") + " "
			name = bold(active.To, nameText)
		}
		date := ""
		if x.Achieved && x.UnlockTime > 0 {
			date = time.Unix(x.UnlockTime, 0).Format("2006-01-02")
		}
		rest := mark + " " + name + "  " + rarityCols(x.Global) + "  " + paint(active.Dim, date)
		rest = padRight(rest, listW-2-iconW)
		key := achIconKey(a.appID, x)
		if img, ok := b.small[key]; ok && b.icons {
			f.put(row, 0, marker)
			f.icons = append(f.icons, iconSpot{row: row, col: 2, w: 2, h: 1, key: key, sixel: img})
			f.put(row, 4, " "+rest) // includes the gap after the icon
		} else {
			f.put(row, 0, marker+strings.Repeat(" ", iconW)+rest)
		}
	}

	if paneW > 0 {
		b.buildAchPane(f, listW, paneW, h+1)
	}
}

func achName(a model.Achievement) string {
	if a.DisplayName != "" {
		return a.DisplayName
	}
	return a.Name
}

func achIconKey(appID int, a model.Achievement) string {
	if a.Achieved {
		return "a" + strconv.Itoa(appID) + ":" + a.Name
	}
	return "a" + strconv.Itoa(appID) + ":" + a.Name + ":gray"
}

func rarityCols(global float64) string {
	if global <= 0 {
		return paint(active.Faint, padLeft("-", 6)) + "  " + strings.Repeat(" ", 12)
	}
	pct := paint(active.Dim, padLeft(fmt.Sprintf("%.1f%%", global), 6))
	switch {
	case global < 5:
		return pct + "  " + chip(tierColors["GOLD"][0], tierColors["GOLD"][1], padRight("ULTRA RARE", 10))
	case global < 15:
		return pct + "  " + chip(tierColors["SILVER"][0], tierColors["SILVER"][1], padRight("RARE", 10))
	}
	return pct + "  " + strings.Repeat(" ", 12)
}

func (b *browser) buildAchPane(f *frame, x, w, h int) {
	a := &b.ach
	inner := w - 4
	lines := make([]string, h-2)
	iconRows := 0
	if a.cursor < len(a.view) {
		ac := a.list[a.view[a.cursor]]
		nameW := inner
		if b.icons {
			if r, ok := achIconRequest(a.appID, ac, b.bigPx()); ok {
				key := r.Key + "@big"
				if img, ok := b.big[key]; ok {
					if img != "" {
						f.icons = append(f.icons, iconSpot{row: 5, col: x + 2, w: 4, h: 2, key: key, sixel: img})
						iconRows, nameW = 2, inner-5
					}
				} else {
					r.Key = key
					b.wantBig = &r
				}
			}
		}
		i := 0
		for _, l := range wrap(achName(ac), nameW, 2) {
			lines[i] = bold(active.Text, l)
			i++
		}
		i = max(i, iconRows) + 1
		desc := ac.Description
		if desc == "" && ac.Hidden {
			desc = "Hidden achievement. Steam reveals the details once it is unlocked."
		}
		if desc == "" {
			desc = "No description."
		}
		for _, l := range wrap(desc, inner, 6) {
			if i < len(lines) {
				lines[i] = paint(active.Text, l)
				i++
			}
		}
		i++
		status := chip(lerp(active.Faint, active.Dim, 0.5), active.Text, "LOCKED")
		if ac.Achieved {
			status = chip(active.Good, active.Ink, "UNLOCKED")
			if ac.UnlockTime > 0 {
				status += " " + paint(active.Dim, time.Unix(ac.UnlockTime, 0).Format("2006-01-02 15:04"))
			}
		}
		if i < len(lines) {
			lines[i] = status
			i += 2
		}
		if ac.Global > 0 && i+2 < len(lines) {
			lines[i] = paint(active.Dim, "unlocked by ") + bold(active.Text, fmt.Sprintf("%.1f%%", ac.Global)) + paint(active.Dim, " of players")
			lines[i+1] = Bar(ac.Global, inner)
			if r := strings.TrimSpace(rarityChip(ac.Global)); r != "" {
				lines[i+2] = r
			}
		}
	}
	border := func(s string) string { return paint(lerp(active.Faint, active.Dim, 0.45), s) }
	f.put(4, x, border("╭"+strings.Repeat("─", w-2)+"╮"))
	for i := 0; i < h-2; i++ {
		row := 5 + i
		if i < iconRows {
			f.put(row, x, border("│")+" ")
			f.put(row, x+6, " "+padRight(lines[i], inner-5)+" "+border("│"))
			continue
		}
		f.put(row, x, border("│")+" "+padRight(lines[i], inner)+" "+border("│"))
	}
	f.put(4+h-1, x, border("╰"+strings.Repeat("─", w-2)+"╯"))
}

func rarityChip(global float64) string {
	switch {
	case global <= 0:
		return ""
	case global < 5:
		return chip(tierColors["GOLD"][0], tierColors["GOLD"][1], "ULTRA RARE")
	case global < 15:
		return chip(tierColors["SILVER"][0], tierColors["SILVER"][1], "RARE")
	}
	return ""
}

// buildFriends draws the friends list with live status.
func (b *browser) buildFriends(f *frame) {
	fr := &b.friends
	online, playing := 0, 0
	for _, x := range fr.list {
		if x.Game != "" {
			playing++
		}
		if x.State > 0 {
			online++
		}
	}
	right := ""
	if fr.loaded {
		right = paint(active.Text, fmt.Sprint(len(fr.list))) + paint(active.Dim, " friends  ") +
			paint(active.Good, fmt.Sprint(online)) + paint(active.Dim, " online  ") +
			paint(active.Accent, fmt.Sprint(playing)) + paint(active.Dim, " in game ")
	}
	b.header(f, right)
	f.put(1, 0, " "+chip(active.Accent, active.Ink, "FRIENDS")+strings.Repeat(" ", max(b.w-11, 0)))
	f.put(2, 0, strings.Repeat(" ", b.w))
	f.put(3, 0, paint(active.Faint, strings.Repeat("─", b.w)))
	h := b.listHeight() - 1
	center := func(msg string) {
		f.put(4+h/2, 0, padRight(strings.Repeat(" ", max((b.w-width(msg))/2, 0))+msg, b.w))
	}
	switch {
	case fr.loading:
		center(paint(active.Accent, spinnerFrames[b.frameN%len(spinnerFrames)]) + " " + paint(active.Text, "Loading friends..."))
		return
	case fr.err != nil:
		msg := fr.err.Error()
		center(chip(active.Bad, active.Ink, "ERROR") + " " + paint(active.Text, msg))
		if strings.Contains(msg, "friends list is private") {
			f.put(4+h/2+2, 0, padRight(strings.Repeat(" ", max((b.w-70)/2, 0))+paint(active.Dim,
				"Steam > Edit Profile > Privacy Settings > Friends List: Public, then R to retry"), b.w))
		}
		return
	case len(fr.list) == 0:
		center(paint(active.Dim, "No friends to show."))
		return
	}
	cols, rows := b.friendCols(), b.friendRows()
	cardW := (b.w - 2) / cols
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			i := (fr.offset+r)*cols + c
			row, col := 4+r*4, 1+c*cardW
			if i >= len(fr.list) {
				for line := 0; line < 4; line++ {
					f.put(row+line, col, strings.Repeat(" ", cardW))
				}
				continue
			}
			b.friendCard(f, fr.list[i], i == fr.cursor, row, col, cardW)
		}
	}
}

// friendCard draws one card: avatar (4x2 cells) with name and status beside
// it, and the game or last-online time underneath.
func (b *browser) friendCard(f *frame, x model.Friend, sel bool, row, col, w int) {
	textCol := col + 1
	if b.icons {
		key := "F" + x.SteamID
		if img, ok := b.small[key]; ok {
			f.icons = append(f.icons, iconSpot{row: row, col: col + 1, w: 4, h: 2, key: key, sixel: img})
		}
		textCol = col + 6
	}
	tw := col + w - textCol
	if b.icons {
		if _, ok := b.small["F"+x.SteamID]; !ok {
			f.put(row, col+1, strings.Repeat(" ", 5))
			f.put(row+1, col+1, strings.Repeat(" ", 5))
		} else {
			f.put(row, col+5, " ")
			f.put(row+1, col+5, " ")
		}
	}
	marker := " "
	name := paint(active.Text, Truncate(x.Name, tw))
	if sel {
		marker = paint(active.Accent, "▌")
		name = bold(active.To, Truncate(x.Name, tw))
	}
	f.put(row, col, marker)
	f.put(row+1, col, marker)
	f.put(row, textCol, padRight(name, tw))
	f.put(row+1, textCol, padRight(friendState(x), tw))
	f.put(row+2, col, " "+padRight(friendDetail(x, w-3), w-1))
	f.put(row+3, col, strings.Repeat(" ", w))
}

func friendState(x model.Friend) string {
	dot := func(c string) string { return paint(c, "●") + " " }
	switch {
	case x.Game != "":
		return dot(active.Accent) + paint(active.Accent, "In game")
	case x.State == 1 || x.State >= 5:
		return dot(active.Good) + paint(active.Good, "Online")
	case x.State == 2:
		return dot(active.Bad) + paint(active.Warn, "Busy")
	case x.State == 3 || x.State == 4:
		return dot(active.Warn) + paint(active.Warn, "Away")
	}
	return dot(active.Faint) + paint(active.Dim, "Offline")
}

func friendDetail(x model.Friend, w int) string {
	switch {
	case x.Game != "":
		return bold(active.Text, Truncate(x.Game, w))
	case x.State == 0 && x.LastLogoff > 0:
		return paint(active.Dim, Truncate("last online "+ago(time.Unix(x.LastLogoff, 0)), w))
	}
	return ""
}

func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	case d < 730*24*time.Hour:
		return fmt.Sprintf("%d months ago", int(d.Hours()/24/30))
	}
	return fmt.Sprintf("%d years ago", int(d.Hours()/24/365))
}

func (b *browser) buildScrollText(f *frame, title string, lines []string) {
	b.header(f, paint(active.Dim, title+" "))
	f.put(1, 0, strings.Repeat(" ", b.w))
	f.put(2, 0, strings.Repeat(" ", b.w))
	f.put(3, 0, paint(active.Faint, strings.Repeat("─", b.w)))
	h := b.listHeight()
	b.scroll = min(b.scroll, max(len(lines)-h, 0))
	for r := 0; r < h; r++ {
		t := ""
		if b.scroll+r < len(lines) {
			t = lines[b.scroll+r]
		}
		f.put(4+r, 0, padRight(t, b.w))
	}
}

func (b *browser) buildFooter(f *frame) {
	f.put(b.h-2, 0, paint(active.Faint, strings.Repeat("─", b.w)))
	type hint struct{ k, v string }
	var hints []hint
	switch b.mode {
	case modeGame:
		hints = []hint{{"p", "play"}, {"←→", "filter"}, {"s", "sort"}, {"esc", "back"}, {"o", "open"}, {"q", "quit"}, {"t", "theme"}, {"↑↓", "move"}}
	case modeHome:
		hints = []hint{{"←→", "cards"}, {"↑↓", "move"}, {"enter", "open"}, {"p", "play"}, {"l", "library"}, {"f", "friends"}, {"?", "help"}, {"q", "quit"}}
	case modePlay:
		hints = []hint{{"p", "play"}, {"c", "close game"}, {"↑↓", "achievements"}, {"←→", "filter"}, {"enter", "full list"}, {"r", "refresh"}, {"o", "store"}, {"f", "folder"}, {"esc", "back"}}
	case modeFriends:
		hints = []hint{{"arrows", "move"}, {"enter", "open profile"}, {"R", "reload"}, {"esc", "back"}, {"q", "quit"}}
	case modeDashboard, modeHelp:
		hints = []hint{{"↑↓", "scroll"}, {"esc", "back"}, {"t", "theme"}, {"q", "quit"}}
	default:
		if b.search {
			hints = []hint{{"type", "to filter"}, {"enter", "done"}, {"esc", "clear"}}
		} else {
			hints = []hint{{"enter", "open game"}, {"p", "play"}, {"/", "search"}, {"←→", "tabs"}, {"s", "sort"}, {"f", "friends"}, {"q", "quit"}, {"?", "help"},
				{"d", "dashboard"}, {"t", "theme"}, {"r", "reverse"}, {"↑↓", "move"}}
		}
	}
	st := ""
	if b.status != "" {
		st = paint(active.Text, b.status) + " "
	}
	// Add hints while they fit; the least important ones are last.
	line := " "
	for _, h := range hints {
		item := bold(active.Accent, h.k) + " " + paint(active.Dim, h.v) + "   "
		if width(line)+width(item)+width(st) > b.w {
			break
		}
		line += item
	}
	if st != "" {
		line += strings.Repeat(" ", max(b.w-width(line)-width(st), 1)) + st
	}
	f.put(b.h-1, 0, padRight(line, b.w))
}

func helpLines() []string {
	k := func(key, desc string) string {
		return "    " + bold(active.Accent, padRight(key, 22)) + paint(active.Text, desc)
	}
	sec := func(t string) string { return "  " + gradient(t) }
	return []string{
		"",
		sec("LIBRARY"),
		k("up / down, j / k", "move the selection"),
		k("pgup / pgdown", "move a page"),
		k("home / end", "first / last game"),
		k("left / right, tab", "switch tab (all, in progress, almost, perfect, not started)"),
		k("1 - 5", "jump to a tab"),
		k("/", "search by name, live"),
		k("s", "change sort: completion, remaining, playtime, recent, name, size"),
		k("r", "reverse the order"),
		k("enter, double click", "open the game: details, achievements, launch"),
		k("p", "game view: details, launch, close, live tracker"),
		k("o", "open the game's achievement page in your browser"),
		k("f", "friends: who is online and what they are playing"),
		k("d", "dashboard with library statistics"),
		"",
		sec("ACHIEVEMENTS"),
		k("left / right", "all, locked, unlocked"),
		k("s", "sort: smart, rarity, name, unlock date"),
		k("p", "game view"),
		"",
		sec("GAME VIEW"),
		k("p", "launch the game through Steam"),
		k("c (twice)", "close the running game"),
		k("up / down", "browse the achievements, details below"),
		k("left / right", "all, locked, unlocked"),
		k("enter", "full-screen achievement list"),
		k("r", "re-read achievements now (automatic every 45 s while playing)"),
		k("o / f", "store page / game folder"),
		k("esc", "back to the library"),
		"",
		sec("EVERYWHERE"),
		k("t", "cycle colour theme"),
		k("i", "toggle icons"),
		k("mouse wheel, click", "scroll and select"),
		k("?", "this help"),
		k("q, ctrl+c", "quit"),
	}
}

// wrap splits s into at most n lines of width w.
func wrap(s string, w, n int) []string {
	var out []string
	line := ""
	for _, word := range strings.Fields(s) {
		switch {
		case line == "":
			line = word
		case width(line)+1+width(word) <= w:
			line += " " + word
		default:
			out = append(out, line)
			line = word
		}
	}
	if line != "" {
		out = append(out, line)
	}
	for i := range out {
		out[i] = Truncate(out[i], w)
	}
	if len(out) > n {
		out = out[:n]
		out[n-1] = Truncate(out[n-1]+" ...", w)
	}
	return out
}

// --- key decoding -------------------------------------------------------------------------

// keyEvent is a decoded key, or a mouse event when mouse is true.
type keyEvent struct {
	key   string
	mouse bool
	row   int // 1-based, for clicks; cell height for "cellsize"
	col   int // cell width for "cellsize"
}

// parseInput decodes raw terminal input into key names. It returns the
// events and the number of bytes consumed (an incomplete escape sequence at
// the end is left for the next read).
func parseInput(buf []byte) ([]keyEvent, int) {
	var out []keyEvent
	i := 0
	for i < len(buf) {
		c := buf[i]
		if c == 0x1b {
			if i+1 >= len(buf) {
				out = append(out, keyEvent{key: "esc"})
				i++
				continue
			}
			n, ev := parseEscape(buf[i:])
			if n == 0 { // incomplete
				break
			}
			if ev.key != "" {
				out = append(out, ev)
			}
			i += n
			continue
		}
		switch c {
		case '\r', '\n':
			out = append(out, keyEvent{key: "enter"})
		case '\t':
			out = append(out, keyEvent{key: "tab"})
		case 0x7f, 0x08:
			out = append(out, keyEvent{key: "backspace"})
		case 0x03:
			out = append(out, keyEvent{key: "ctrl+c"})
		case 0x04:
			out = append(out, keyEvent{key: "ctrl+d"})
		case 0x15:
			out = append(out, keyEvent{key: "ctrl+u"})
		case ' ':
			out = append(out, keyEvent{key: "space"})
		default:
			if c < 0x20 {
				i++
				continue
			}
			r, size := utf8.DecodeRune(buf[i:])
			if r == utf8.RuneError && size <= 1 && !utf8.FullRune(buf[i:]) {
				return out, i // wait for the rest of the rune
			}
			out = append(out, keyEvent{key: string(r)})
			i += size
			continue
		}
		i++
	}
	return out, i
}

func parseEscape(b []byte) (int, keyEvent) {
	if len(b) < 2 {
		return 0, keyEvent{}
	}
	switch b[1] {
	case '[', 'O':
	default:
		// alt+key or a lone escape followed by a normal key
		return 1, keyEvent{key: "esc"}
	}
	// find the final byte of the CSI / SS3 sequence
	j := 2
	for j < len(b) && (b[j] < 0x40 || b[j] > 0x7e) {
		j++
	}
	if j >= len(b) {
		if len(b) > 32 {
			return len(b), keyEvent{}
		}
		return 0, keyEvent{}
	}
	body, final := string(b[2:j]), b[j]
	n := j + 1
	if b[1] == '[' && strings.HasPrefix(body, "<") && (final == 'M' || final == 'm') {
		parts := strings.Split(body[1:], ";")
		if len(parts) == 3 {
			code, _ := strconv.Atoi(parts[0])
			row, _ := strconv.Atoi(parts[2])
			switch {
			case code == 64:
				return n, keyEvent{key: "wheelup", mouse: true}
			case code == 65:
				return n, keyEvent{key: "wheeldown", mouse: true}
			case code == 0 && final == 'M':
				col, _ := strconv.Atoi(parts[1])
				return n, keyEvent{key: "click", mouse: true, row: row, col: col}
			}
		}
		return n, keyEvent{}
	}
	if final == 't' && strings.HasPrefix(body, "6;") {
		parts := strings.Split(body, ";")
		if len(parts) == 3 {
			h, _ := strconv.Atoi(parts[1])
			w, _ := strconv.Atoi(parts[2])
			return n, keyEvent{key: "cellsize", row: h, col: w}
		}
	}
	switch final {
	case 'A':
		return n, keyEvent{key: "up"}
	case 'B':
		return n, keyEvent{key: "down"}
	case 'C':
		return n, keyEvent{key: "right"}
	case 'D':
		return n, keyEvent{key: "left"}
	case 'H':
		return n, keyEvent{key: "home"}
	case 'F':
		return n, keyEvent{key: "end"}
	case 'Z':
		return n, keyEvent{key: "shift+tab"}
	case '~':
		switch body {
		case "1", "7":
			return n, keyEvent{key: "home"}
		case "4", "8":
			return n, keyEvent{key: "end"}
		case "5":
			return n, keyEvent{key: "pgup"}
		case "6":
			return n, keyEvent{key: "pgdown"}
		case "3":
			return n, keyEvent{key: "delete"}
		}
	}
	return n, keyEvent{}
}

// --- screen -----------------------------------------------------------------------------------

type screen struct {
	out   *bufio.Writer
	drawn map[[2]int]string
}

func (s *screen) draw(f frame, full bool) {
	w := s.out
	if full {
		w.WriteString("\x1b[2J")
		s.drawn = map[[2]int]string{}
	}
	for r, segs := range f.rows {
		for _, sg := range segs {
			fmt.Fprintf(w, "\x1b[%d;%dH%s", r+1, sg.col+1, sg.text)
		}
		if len(segs) > 0 {
			w.WriteString("\x1b[0m\x1b[K")
		}
	}
	seen := map[[2]int]bool{}
	for _, ic := range f.icons {
		pos := [2]int{ic.row, ic.col}
		seen[pos] = true
		if s.drawn[pos] == ic.key {
			continue
		}
		// Blank the cells first so no stale text shows around the image.
		for dy := 0; dy < max(ic.h, 1); dy++ {
			fmt.Fprintf(w, "\x1b[%d;%dH%s", ic.row+dy+1, ic.col+1, strings.Repeat(" ", max(ic.w, 1)))
		}
		fmt.Fprintf(w, "\x1b[%d;%dH%s", ic.row+1, ic.col+1, ic.sixel)
		s.drawn[pos] = ic.key
	}
	for pos := range s.drawn {
		if !seen[pos] {
			delete(s.drawn, pos)
		}
	}
	w.WriteString("\x1b[0m")
	w.Flush()
}

// --- run loop -----------------------------------------------------------------------------------

// Browse runs the interactive browser until the user quits.
func Browse(opt BrowseOptions) error {
	inFd, outFd := os.Stdin.Fd(), os.Stdout.Fd()
	if !term.IsTerminal(inFd) || !term.IsTerminal(outFd) {
		return fmt.Errorf("steam browse needs an interactive terminal")
	}
	w, h, err := term.GetSize(outFd)
	if err != nil || w < 60 || h < 16 {
		return fmt.Errorf("the terminal is too small for the browser (need at least 60x16)")
	}
	state, err := term.MakeRaw(inFd)
	if err != nil {
		return fmt.Errorf("could not switch the terminal to raw mode: %v", err)
	}
	out := bufio.NewWriterSize(os.Stdout, 1<<16)
	restore := func() {
		out.WriteString("\x1b[?1006l\x1b[?1000l\x1b[0m\x1b[?25h\x1b[?1049l")
		out.Flush()
		_ = term.Restore(inFd, state)
	}
	defer restore()
	defer func() {
		if r := recover(); r != nil {
			restore()
			panic(r)
		}
	}()
	// alt screen, hidden cursor, SGR mouse, and ask for the cell size in
	// pixels (CSI 16 t) so images fit their cells exactly.
	out.WriteString("\x1b[?1049h\x1b[?25l\x1b[?1000h\x1b[?1006h\x1b[16t")

	b := newBrowser(opt, w, h)
	scr := &screen{out: out, drawn: map[[2]int]string{}}

	keys := make(chan keyEvent, 64)
	go func() {
		buf := make([]byte, 0, 4096)
		tmp := make([]byte, 1024)
		for {
			n, err := os.Stdin.Read(tmp)
			if err != nil {
				close(keys)
				return
			}
			buf = append(buf, tmp[:n]...)
			evs, used := parseInput(buf)
			buf = append(buf[:0], buf[used:]...)
			for _, ev := range evs {
				keys <- ev
			}
		}
	}()

	events := make(chan func(), 64)
	startTime := time.Now()
	tick := time.NewTicker(120 * time.Millisecond)
	defer tick.Stop()

	statusAt := time.Time{}
	dirty := true
	runningBusy := false
	lastPoll := time.Time{}
	for !b.quit {
		if b.wantAch != 0 && opt.LoadAchievements != nil {
			id := b.wantAch
			b.wantAch = 0
			go func() {
				list, err := opt.LoadAchievements(id)
				events <- func() {
					if err == nil {
						b.applyRefresh(id, list) // before the cache is replaced
					} else if b.play.appID == id {
						b.play.refreshing = false
					}
					b.setAchievements(id, list, err)
					if err == nil && b.play.appID == id {
						b.wantAchIconsFor = id
					}
				}
			}()
		}
		if b.wantStore != 0 {
			id := b.wantStore
			b.wantStore = 0
			if opt.LoadStore != nil {
				go func() {
					info, err := opt.LoadStore(id)
					events <- func() {
						if b.play.appID == id {
							if err != nil {
								b.play.storeErr = err
							} else {
								b.play.store = &info
							}
						}
					}
				}()
			} else {
				b.play.storeErr = fmt.Errorf("not available in demo mode")
			}
		}
		if b.wantInstall != 0 {
			id := b.wantInstall
			b.wantInstall = 0
			if !opt.Demo {
				go func() {
					in := launcher.Lookup(id)
					events <- func() {
						if b.play.appID == id {
							b.play.install, b.play.installKnown = in, true
						}
					}
				}()
			}
		}
		if b.wantClose != 0 {
			id := b.wantClose
			b.wantClose = 0
			go func() {
				err := launcher.Close(id)
				events <- func() {
					b.play.closing = false
					if err != nil {
						b.status = "could not close: " + err.Error()
						return
					}
					b.play.launching = false
					b.status = "game closed"
				}
			}()
		}
		if b.wantRunning && !runningBusy && !opt.Demo {
			b.wantRunning = false
			runningBusy = true
			go func() {
				id := launcher.RunningAppID()
				on := launcher.SteamRunning()
				events <- func() {
					runningBusy = false
					b.play.steamKnown, b.play.steamOn = true, on || id != 0
					b.updateRunning(id)
				}
			}()
		}
		if b.icons && !b.iconsStarted && (b.cellKnown || time.Since(startTime) > 500*time.Millisecond) {
			b.iconsStarted = true
			go loadIcons(gameIconRequests(b), events, b.small, b.smallPx())
		}
		if b.wantAchIconsFor != 0 && b.icons {
			id := b.wantAchIconsFor
			b.wantAchIconsFor = 0
			go loadIcons(achIconRequests(id, b.cache[id], b.smallPx()), events, b.small, b.smallPx())
		}
		if b.wantBig != nil {
			req := *b.wantBig
			b.wantBig = nil
			if _, busy := b.big[req.Key+"#loading"]; !busy {
				b.big[req.Key+"#loading"] = ""
				go func() {
					res := icons.RenderKeyed([]icons.Request{req}, 32)
					events <- func() {
						b.big[req.Key] = res[req.Key] // "" when it failed: not retried
					}
				}()
			}
		}
		if b.wantFriends && opt.LoadFriends != nil {
			b.wantFriends = false
			go func() {
				list, err := opt.LoadFriends()
				events <- func() { b.setFriends(list, err) }
			}()
		}
		if b.wantAvatars && b.icons {
			b.wantAvatars = false
			go loadIcons(avatarRequests(b.friends.list, b.smallPx(), b.bigPx()), events, b.small, b.smallPx())
		}
		if b.wantAchIcons && b.icons {
			b.wantAchIcons = false
			go loadIcons(achIconRequests(b.ach.appID, b.ach.list, b.smallPx()), events, b.small, b.smallPx())
		}
		if dirty {
			full := b.full
			b.full = false
			scr.draw(b.build(), full)
			dirty = false
		}
		select {
		case ev, ok := <-keys:
			if !ok {
				return nil
			}
			prevStatus := b.status
			if ev.key == "click" && b.mode == modeFriends {
				b.clickFriend(ev.row-1, ev.col-1)
			} else if ev.key == "cellsize" {
				if ev.col >= 4 && ev.col <= 64 && ev.row >= 8 && ev.row <= 128 {
					b.cellW, b.cellH, b.full = ev.col, ev.row, true
					b.cellKnown = true
				}
			} else if ev.key == "click" {
				b.click(ev.row - 1)
			} else {
				b.handleKey(ev.key)
			}
			if b.status != prevStatus {
				statusAt = time.Now()
			}
			dirty = true
		case f := <-events:
			prevStatus := b.status
			f()
			if b.status != prevStatus {
				statusAt = time.Now()
			}
			dirty = true
		case <-tick.C:
			b.frameN++
			if nw, nh, err := term.GetSize(outFd); err == nil && (nw != b.w || nh != b.h) && nw >= 60 && nh >= 16 {
				b.w, b.h = nw, nh
				b.clamp()
				b.full = true
				dirty = true
			}
			if b.mode == modeGame && b.ach.loading {
				dirty = true
			}
			// Game view: poll whether the game runs, refresh achievements
			// while it does, keep the session clock ticking.
			if b.mode == modePlay || b.play.running || b.play.launching {
				if time.Since(lastPoll) > 2*time.Second {
					lastPoll = time.Now()
					b.wantRunning = true
				}
				p := &b.play
				if p.running && !p.refreshing && time.Since(p.lastRefresh) > refreshEvery {
					p.refreshing = true
					b.wantAch = p.appID
				}
				if p.launching && !p.running && time.Since(p.launchedAt) > 90*time.Second {
					p.launching = false
					b.status = "Steam did not report the game as running"
					statusAt = time.Now()
				}
				if b.mode == modePlay && b.frameN%4 == 0 {
					dirty = true
				}
			}
			keep := 2500 * time.Millisecond
			if strings.HasPrefix(b.status, "ACHIEVEMENT") {
				keep = 10 * time.Second
			}
			if b.status != "" && time.Since(statusAt) > keep {
				b.status = ""
				dirty = true
			}
		}
	}
	return nil
}

// updateRunning applies the game Steam reports as running.
func (b *browser) updateRunning(id int) {
	p := &b.play
	if p.appID == 0 {
		return
	}
	switch {
	case id == p.appID && !p.running:
		p.running, p.launching = true, false
		p.startedAt = time.Now()
		p.lastRefresh = time.Now()
		b.status = "game started, tracking achievements"
	case id != p.appID && p.running:
		p.running, p.closing = false, false
		b.status = "game closed after " + clock(time.Since(p.startedAt))
		if !p.refreshing { // one last read to catch late unlocks
			p.refreshing = true
			b.wantAch = p.appID
		}
	}
}

// gameIconRequests lists every game icon, the visible ones first. It runs on
// the UI goroutine; only the downloads happen in the background.
func gameIconRequests(b *browser) []icons.Request {
	order := map[int]int{}
	for i, gi := range b.view {
		order[b.games[gi].AppID] = i
	}
	var reqs []icons.Request
	for _, g := range b.games {
		if g.Icon != "" {
			reqs = append(reqs, icons.Request{Key: "g" + strconv.Itoa(g.AppID), AppID: g.AppID, Hash: g.Icon})
		}
	}
	sort.SliceStable(reqs, func(i, j int) bool {
		oi, iok := order[reqs[i].AppID]
		oj, jok := order[reqs[j].AppID]
		if iok != jok {
			return iok
		}
		return oi < oj
	})
	return reqs
}

func achIconRequests(appID int, list []model.Achievement, size int) []icons.Request {
	var reqs []icons.Request
	for _, a := range list {
		if r, ok := achIconRequest(appID, a, size); ok {
			reqs = append(reqs, r)
		}
	}
	return reqs
}

// achIconRequest styles an achievement icon: unlocked ones in colour, with a
// gold frame when ultra rare and a silver one when rare; locked ones grey
// and darkened.
func achIconRequest(appID int, a model.Achievement, size int) (icons.Request, bool) {
	r := icons.Request{Key: achIconKey(appID, a), Size: size}
	if a.Achieved {
		r.URL = a.Icon
		switch {
		case a.Global > 0 && a.Global < 5:
			r.Frame = tierColors["GOLD"][0]
		case a.Global > 0 && a.Global < 15:
			r.Frame = tierColors["SILVER"][0]
		}
	} else {
		r.URL = a.IconGray
		r.Dim = true
	}
	return r, r.URL != ""
}

func avatarRequests(list []model.Friend, small, big int) []icons.Request {
	var reqs []icons.Request
	for _, f := range list {
		if f.Avatar != "" {
			reqs = append(reqs, icons.Request{Key: "f" + f.SteamID, URL: f.Avatar, Size: small},
				icons.Request{Key: "F" + f.SteamID, URL: f.Avatar, Size: big})
		}
	}
	return reqs
}

// loadIcons renders icons in batches of 24 so they appear progressively.
// Results are applied to dst on the UI goroutine through events.
func loadIcons(reqs []icons.Request, events chan<- func(), dst map[string]string, size int) {
	for start := 0; start < len(reqs); start += 24 {
		res := icons.RenderKeyed(reqs[start:min(start+24, len(reqs))], size)
		events <- func() {
			for k, v := range res {
				dst[k] = v
			}
		}
	}
}
