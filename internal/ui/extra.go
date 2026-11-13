package ui

// Views that make the CLI worth opening instead of the Steam client:
//
//	a  hunt     the easiest achievements you are still missing, library wide
//	w  deals    your wishlist on sale and the store's current specials
//	n  news     patch notes and announcements of the games you play
//	u  storage  what is installed, how big it is, and what you never play
//
// They share one layout: a title row with tabs, a summary line and a list
// of two-line entries with a blank row between them.

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tomikav/steam-achievements-cli-/internal/launcher"
	"github.com/tomikav/steam-achievements-cli-/internal/model"
)

// Installed is one installed game, for the storage view.
type Installed struct {
	AppID int
	Name  string
	Size  int64
	Dir   string
}

type panelKind int

const (
	panelHunt panelKind = iota
	panelDeals
	panelNews
	panelStorage
)

var panelTitles = [...]string{"ACHIEVEMENT HUNT", "DEALS", "NEWS", "STORAGE"}

var panelTabs = [...][]string{
	{"EASIEST FIRST", "RAREST FIRST"},
	{"WISHLIST", "STORE SPECIALS"},
	{"RECENT GAMES"},
	{"BIGGEST FIRST", "NOT PLAYED IN 90 DAYS"},
}

type panelState struct {
	loading, loaded bool
	err             error
	tab             int
	cursor, offset  int
}

// panelRow is one entry of a panel: icon, title with a right-aligned value,
// and a detail line.
type panelRow struct {
	appID  int
	icon   string
	title  string
	right  string
	detail string
	url    string
}

// extraState holds the data of the four views.
type extraState struct {
	kind      panelKind
	panels    [4]panelState
	back      browseMode
	globals   map[int]map[string]float64
	globalsN  int // games still loading global percentages
	deals     []model.Deal
	news      []model.News
	installed []Installed
	confirm   int // appID waiting for a second u (uninstall)

	wantGlobals []int
	wantDeals   bool
	wantNews    bool
	wantInstall bool
}

const entryH = 3 // two text rows and a blank one

func (b *browser) panelPerPage() int { return max(1, (b.h-6)/entryH) }

// openPanel switches to one of the views, loading its data the first time.
func (b *browser) openPanel(k panelKind) {
	x := &b.extra
	if b.mode != modePanel {
		x.back = b.mode
		if x.back != modeHome {
			x.back = modeList
		}
	}
	x.kind, x.confirm = k, 0
	b.mode, b.full = modePanel, true
	p := &x.panels[k]
	if p.loaded || p.loading {
		return
	}
	switch k {
	case panelHunt:
		p.loaded = true // built from the library; percentages arrive later
		if b.opt.LoadGlobal != nil {
			if x.globals == nil {
				x.globals = map[int]map[string]float64{}
			}
			for _, g := range b.huntGames() {
				if !b.hasGlobals(g) {
					x.wantGlobals = append(x.wantGlobals, g.AppID)
				}
			}
			x.globalsN = len(x.wantGlobals)
		}
	case panelDeals:
		if b.opt.LoadDeals == nil {
			p.err = fmt.Errorf("deals are not available here")
			return
		}
		p.loading, x.wantDeals = true, true
	case panelNews:
		if b.opt.LoadNews == nil {
			p.err = fmt.Errorf("news is not available here")
			return
		}
		p.loading, x.wantNews = true, true
	case panelStorage:
		if b.opt.LoadInstalled == nil {
			p.err = fmt.Errorf("installed games are not available here")
			return
		}
		p.loading, x.wantInstall = true, true
	}
}

// huntGames are games with locked achievements, most recently played first.
func (b *browser) huntGames() []model.Game {
	var out []model.Game
	for _, g := range b.games {
		if hasAch(g) && len(g.Locked) > 0 {
			out = append(out, g)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].LastPlayed > out[j].LastPlayed })
	return out
}

func (b *browser) hasGlobals(g model.Game) bool {
	if _, ok := b.extra.globals[g.AppID]; ok {
		return true
	}
	for _, a := range g.Locked {
		if a.Global > 0 {
			return true
		}
	}
	return false
}

func (b *browser) setGlobals(appID int, m map[string]float64) {
	x := &b.extra
	if x.globals == nil {
		x.globals = map[int]map[string]float64{}
	}
	if m == nil {
		m = map[string]float64{}
	}
	x.globals[appID] = m
	x.globalsN = max(x.globalsN-1, 0)
}

type huntItem struct {
	game   model.Game
	ach    model.Achievement
	global float64
}

func (b *browser) huntItems() []huntItem {
	var out []huntItem
	for _, g := range b.huntGames() {
		gl := b.extra.globals[g.AppID]
		for _, a := range g.Locked {
			p := a.Global
			if p <= 0 {
				p = gl[a.Name]
			}
			out = append(out, huntItem{g, a, p})
		}
	}
	rarest := b.extra.panels[panelHunt].tab == 1
	sort.SliceStable(out, func(i, j int) bool {
		a, c := out[i].global, out[j].global
		if (a > 0) != (c > 0) {
			return a > 0 // known percentages first
		}
		if rarest {
			return a < c
		}
		return a > c
	})
	return out
}

func (b *browser) dealItems() []model.Deal {
	var out []model.Deal
	wish := b.extra.panels[panelDeals].tab == 0
	for _, d := range b.extra.deals {
		if d.Wishlist == wish {
			out = append(out, d)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Discount != out[j].Discount {
			return out[i].Discount > out[j].Discount
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func (b *browser) gameByID(id int) (model.Game, bool) {
	for _, g := range b.games {
		if g.AppID == id {
			return g, true
		}
	}
	return model.Game{}, false
}

const unusedAfter = 90 * 24 * time.Hour

func (b *browser) storageItems() []Installed {
	var out []Installed
	unused := b.extra.panels[panelStorage].tab == 1
	for _, in := range b.extra.installed {
		if in.AppID == 228980 || isExtra(in.Name) && !strings.Contains(strings.ToLower(in.Name), "demo") {
			continue // Steamworks redistributables and tools
		}
		if unused {
			g, _ := b.gameByID(in.AppID)
			if g.LastPlayed > 0 && time.Since(time.Unix(g.LastPlayed, 0)) < unusedAfter {
				continue
			}
		}
		out = append(out, in)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Size > out[j].Size })
	return out
}

// rows turns the active view into display rows.
func (b *browser) panelRows() []panelRow {
	x := &b.extra
	var rows []panelRow
	switch x.kind {
	case panelHunt:
		for _, it := range b.huntItems() {
			right := paint(active.Faint, "loading")
			if it.global > 0 {
				right = bold(huntColor(it.global), fmt.Sprintf("%.1f%%", it.global)) + paint(active.Dim, " of players")
			} else if x.globalsN == 0 {
				right = paint(active.Faint, "no data")
			}
			detail := paint(active.Accent, Truncate(it.game.Name, 40))
			if it.ach.Description != "" {
				detail += paint(active.Dim, "   "+it.ach.Description)
			}
			rows = append(rows, panelRow{appID: it.game.AppID, icon: "g" + strconv.Itoa(it.game.AppID),
				title: achName(it.ach), right: right, detail: detail})
		}
	case panelDeals:
		for _, d := range b.dealItems() {
			right := bold(active.Text, d.Final)
			if d.Discount > 0 {
				right = chip(active.Good, active.Ink, fmt.Sprintf("-%d%%", d.Discount)) + "  " +
					paint(active.Faint, d.Original) + "  " + bold(active.Good, d.Final)
			}
			detail := paint(active.Dim, "full price")
			switch {
			case !d.Available:
				detail, right = paint(active.Dim, "not for sale in your region"), ""
			case d.Free:
				detail, right = paint(active.Dim, "free to play"), bold(active.Good, "FREE")
			case d.Discount >= 75:
				detail = paint(active.Good, "historic-level discount")
			case d.Discount > 0:
				detail = paint(active.Dim, "on sale now")
			}
			rows = append(rows, panelRow{appID: d.AppID, icon: "g" + strconv.Itoa(d.AppID), title: d.Name, right: right,
				detail: detail, url: fmt.Sprintf("https://store.steampowered.com/app/%d", d.AppID)})
		}
	case panelNews:
		for _, n := range x.news {
			right := paint(active.Dim, ago(time.Unix(n.Date, 0)))
			detail := paint(active.Accent, Truncate(n.Game, 32)) + paint(active.Dim, "   "+n.Summary)
			rows = append(rows, panelRow{appID: n.AppID, icon: "g" + strconv.Itoa(n.AppID), title: n.Title, right: right, detail: detail, url: n.URL})
		}
	case panelStorage:
		for _, in := range b.storageItems() {
			g, owned := b.gameByID(in.AppID)
			right := bold(active.Text, launcher.FormatSize(in.Size))
			var detail string
			switch {
			case !owned || g.LastPlayed == 0:
				detail = paint(active.Warn, "never played")
			case time.Since(time.Unix(g.LastPlayed, 0)) > unusedAfter:
				detail = paint(active.Warn, "last played "+ago(time.Unix(g.LastPlayed, 0)))
			default:
				detail = paint(active.Dim, "last played "+ago(time.Unix(g.LastPlayed, 0)))
			}
			if owned && g.Playtime > 0 {
				detail += paint(active.Dim, "   "+formatPlaytime(g.Playtime)+" played")
			}
			if x.confirm == in.AppID {
				detail = chip(active.Bad, active.Ink, "PRESS U AGAIN TO UNINSTALL") + "  " + paint(active.Dim, "Steam asks to confirm")
			}
			rows = append(rows, panelRow{appID: in.AppID, icon: "g" + strconv.Itoa(in.AppID), title: in.Name, right: right, detail: detail})
		}
	}
	return rows
}

func huntColor(global float64) string {
	switch {
	case global >= 50:
		return active.Good
	case global >= 15:
		return active.From
	case global >= 5:
		return active.Warn
	}
	return active.Bad
}

func (b *browser) panelKey(k string) {
	x := &b.extra
	p := &x.panels[x.kind]
	rows := b.panelRows()
	per := b.panelPerPage()
	if k != "u" {
		x.confirm = 0
	}
	switch k {
	case "esc", "backspace":
		b.mode, b.full = x.back, true
		return
	case "down", "j", "wheeldown":
		p.cursor++
	case "up", "k", "wheelup":
		p.cursor--
	case "pgdown":
		p.cursor += per
	case "pgup":
		p.cursor -= per
	case "home", "g":
		p.cursor = 0
	case "end", "G":
		p.cursor = len(rows) - 1
	case "right", "tab", "l", "left", "shift+tab", "h":
		n := len(panelTabs[x.kind])
		if k == "right" || k == "tab" || k == "l" {
			p.tab = (p.tab + 1) % n
		} else {
			p.tab = (p.tab + n - 1) % n
		}
		p.cursor, p.offset, b.full = 0, 0, true
		return
	case "enter", "o":
		if p.cursor < len(rows) {
			r := rows[p.cursor]
			if r.url == "" || k == "enter" && x.kind != panelNews && x.kind != panelDeals {
				if _, owned := b.gameByID(r.appID); owned {
					b.openPlay(r.appID)
					return
				}
			}
			if r.url == "" {
				r.url = fmt.Sprintf("https://store.steampowered.com/app/%d", r.appID)
			}
			if b.opt.Demo {
				b.status = "demo mode: nothing to open"
			} else if openTarget(r.url) == nil {
				b.status = "opened in your browser"
			}
		}
	case "u":
		if x.kind == panelStorage && p.cursor < len(rows) {
			id := rows[p.cursor].appID
			if x.confirm != id {
				x.confirm = id
				return
			}
			x.confirm = 0
			switch {
			case b.opt.Uninstall == nil:
				b.status = "demo mode: nothing was uninstalled"
			case b.opt.Uninstall(id) != nil:
				b.status = "could not ask Steam to uninstall"
			default:
				b.status = "Steam is asking to confirm the uninstall"
				p.loaded, p.loading = false, true // re-scan the libraries
				x.wantInstall = true
			}
		}
	case "R":
		if x.kind != panelHunt {
			p.loaded, p.err = false, nil
			b.openPanel(x.kind)
		}
	}
	clampScroll(&p.cursor, &p.offset, len(rows), per)
}

func (b *browser) buildPanel(f *frame) {
	x := &b.extra
	p := &x.panels[x.kind]
	rows := b.panelRows()

	b.header(f, b.panelSummary(len(rows)))
	line := " " + chip(active.Accent, active.Ink, panelTitles[x.kind]) + "   "
	for i, t := range panelTabs[x.kind] {
		if i == p.tab {
			line += bold(active.To, t)
		} else {
			line += paint(active.Dim, t)
		}
		line += "     "
	}
	f.put(1, 0, padRight(line, b.w))
	f.put(2, 0, padRight("  "+paint(active.Dim, b.panelHint()), b.w))
	f.put(3, 0, paint(active.Faint, strings.Repeat("─", b.w)))

	h := b.h - 6
	center := func(msg string) {
		for r := 0; r < h; r++ {
			f.put(4+r, 0, strings.Repeat(" ", b.w))
		}
		f.put(4+h/2, 0, padRight(strings.Repeat(" ", max((b.w-width(msg))/2, 0))+msg, b.w))
	}
	switch {
	case p.err != nil:
		center(chip(active.Bad, active.Ink, "ERROR") + " " + paint(active.Text, p.err.Error()) + paint(active.Dim, "   R to retry"))
		return
	case p.loading:
		center(paint(active.Accent, spinnerFrames[b.frameN%len(spinnerFrames)]) + " " + paint(active.Text, "Loading "+strings.ToLower(panelTitles[x.kind])+"..."))
		return
	case len(rows) == 0:
		center(paint(active.Dim, b.panelEmpty()))
		return
	}

	per := b.panelPerPage()
	clampScroll(&p.cursor, &p.offset, len(rows), per)
	for i := 0; i < per; i++ {
		top := 4 + i*entryH
		idx := p.offset + i
		if idx >= len(rows) {
			for k := 0; k < entryH; k++ {
				f.put(top+k, 0, strings.Repeat(" ", b.w))
			}
			continue
		}
		r := rows[idx]
		sel := idx == p.cursor
		marker := "   "
		if sel {
			marker = " " + paint(active.Accent, "▌") + " "
		}
		textCol := 3
		if img, ok := b.small[r.icon]; ok && b.icons {
			f.icons = append(f.icons, iconSpot{row: top, col: 3, w: 2, h: 1, key: r.icon, sixel: img})
		}
		textCol = 7
		rightW := width(r.right)
		titleW := max(b.w-textCol-rightW-6, 8)
		title := paint(active.Text, Truncate(r.title, titleW))
		if sel {
			title = bold(active.To, Truncate(r.title, titleW))
		}
		gap := b.w - textCol - width(title) - rightW - 3
		f.put(top, 0, marker)
		f.put(top, textCol, title+strings.Repeat(" ", max(gap, 1))+r.right+"   ")
		f.put(top+1, 0, padRight(marker+"    "+truncateANSI(r.detail, b.w-textCol-3), b.w))
		f.put(top+2, 0, strings.Repeat(" ", b.w))
	}
}

// truncateANSI cuts a styled string to w visible columns.
func truncateANSI(s string, w int) string {
	if width(s) <= w {
		return s
	}
	var out strings.Builder
	n := 0
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			j := i + 1
			for j < len(s) && (s[j] < '@' || s[j] > '~' || j == i+1) {
				j++
			}
			out.WriteString(s[i:min(j+1, len(s))])
			i = j + 1
			continue
		}
		r := []rune(s[i:])[0]
		rw := width(string(r))
		if n+rw > w-3 {
			out.WriteString("...\x1b[0m")
			break
		}
		out.WriteRune(r)
		n += rw
		i += len(string(r))
	}
	return out.String()
}

func (b *browser) panelSummary(n int) string {
	x := &b.extra
	switch x.kind {
	case panelHunt:
		s := paint(active.Text, fmt.Sprint(n)) + paint(active.Dim, plural(n, " locked achievement ", " locked achievements "))
		if x.globalsN > 0 {
			s += paint(active.Accent, spinnerFrames[b.frameN%len(spinnerFrames)]) + paint(active.Dim, fmt.Sprintf(" rarity of %d games ", x.globalsN))
		}
		return s
	case panelDeals:
		on := 0
		for _, d := range x.deals {
			if d.Wishlist && d.Discount > 0 {
				on++
			}
		}
		if x.panels[panelDeals].loaded {
			return paint(active.Good, fmt.Sprint(on)) + paint(active.Dim, plural(on, " wishlist game", " wishlist games")+" on sale ")
		}
	case panelStorage:
		var total, unused int64
		for _, in := range x.installed {
			total += in.Size
			g, _ := b.gameByID(in.AppID)
			if g.LastPlayed == 0 || time.Since(time.Unix(g.LastPlayed, 0)) > unusedAfter {
				unused += in.Size
			}
		}
		if x.panels[panelStorage].loaded {
			return paint(active.Text, launcher.FormatSize(total)) + paint(active.Dim, " installed   ") +
				paint(active.Warn, launcher.FormatSize(unused)) + paint(active.Dim, " in games you do not play ")
		}
	case panelNews:
		if x.panels[panelNews].loaded {
			return paint(active.Text, fmt.Sprint(n)) + paint(active.Dim, plural(n, " post ", " posts "))
		}
	}
	return ""
}

func (b *browser) panelHint() string {
	switch b.extra.kind {
	case panelHunt:
		return "Locked achievements across your library, by how many players have them. Enter opens the game."
	case panelDeals:
		return "Prices from the Steam store in your region. Enter opens the store page."
	case panelNews:
		return "Latest posts of the games you played recently. Enter opens the post."
	case panelStorage:
		return "Installed games in every Steam library. U twice to uninstall, enter for the game view."
	}
	return ""
}

func (b *browser) panelEmpty() string {
	switch b.extra.kind {
	case panelHunt:
		return "Nothing to hunt yet. Open the library once so the cache learns your locked achievements."
	case panelDeals:
		if b.extra.panels[panelDeals].tab == 0 {
			return "Your wishlist is empty or private."
		}
		return "No specials right now."
	case panelNews:
		return "No recent news for your games."
	case panelStorage:
		return "No installed games found."
	}
	return ""
}

// newsTargets are the games whose news is shown: the ten played last.
func (b *browser) newsTargets() []int {
	gs := append([]model.Game(nil), b.games...)
	sort.SliceStable(gs, func(i, j int) bool { return gs[i].LastPlayed > gs[j].LastPlayed })
	var ids []int
	for _, g := range gs {
		if g.LastPlayed > 0 && len(ids) < 10 {
			ids = append(ids, g.AppID)
		}
	}
	return ids
}

// perfState tracks how much memory the Steam client uses next to this CLI.
type perfState struct {
	want, busy bool
	polled     time.Time
	known      bool
	steam      int64 // bytes, 0 when Steam is not running
	confirm    bool  // x pressed once
	closing    bool
}

// extraIO starts the background loads the extra views asked for.
func (b *browser) extraIO(events chan<- func()) {
	x := &b.extra
	opt := b.opt
	if len(x.wantGlobals) > 0 && opt.LoadGlobal != nil {
		ids := x.wantGlobals
		x.wantGlobals = nil
		jobs := make(chan int, len(ids))
		for _, id := range ids {
			jobs <- id
		}
		close(jobs)
		for w := 0; w < 6; w++ {
			go func() {
				for id := range jobs {
					m, _ := opt.LoadGlobal(id)
					events <- func() { b.setGlobals(id, m) }
				}
			}()
		}
	}
	load := func(want *bool, kind panelKind, fn func() error) {
		if !*want {
			return
		}
		*want = false
		go func() {
			err := fn()
			events <- func() {
				p := &x.panels[kind]
				p.loading, p.loaded, p.err = false, err == nil, err
			}
		}()
	}
	load(&x.wantDeals, panelDeals, func() error {
		d, err := opt.LoadDeals()
		events <- func() { x.deals = d }
		return err
	})
	targets := b.newsTargets()
	load(&x.wantNews, panelNews, func() error {
		n, err := opt.LoadNews(targets)
		events <- func() { x.news = n }
		return err
	})
	load(&x.wantInstall, panelStorage, func() error {
		in, err := opt.LoadInstalled()
		events <- func() { x.installed = in }
		return err
	})
	if b.perf.want && !b.perf.busy && opt.SteamMemory != nil {
		b.perf.want, b.perf.busy = false, true
		go func() {
			n, ok := opt.SteamMemory()
			events <- func() {
				b.perf.busy, b.perf.known = false, true
				b.perf.steam = 0
				if ok {
					b.perf.steam = n
				}
				if b.perf.steam == 0 {
					b.perf.closing = false
				}
			}
		}()
	}
}
