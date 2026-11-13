package ui

// Game view (`p`): store details, header art, install info, launch and
// close, plus a live achievement tracker while the game is running.

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/tomikav/steam-achievements-cli-/internal/icons"
	"github.com/tomikav/steam-achievements-cli-/internal/launcher"
	"github.com/tomikav/steam-achievements-cli-/internal/model"
)

// refreshEvery is how often achievements are re-read while a game runs.
const refreshEvery = 45 * time.Second

type playState struct {
	appID        int
	store        *model.StoreInfo
	storeErr     error
	install      launcher.Install
	installKnown bool
	steamKnown   bool
	steamOn      bool // Steam client running (window or tray)
	running      bool
	launching    bool
	launchedAt   time.Time
	startedAt    time.Time
	confirmClose bool
	closing      bool
	refreshing   bool
	lastRefresh  time.Time
	session      []model.Achievement // unlocked while this view was open
}

func (b *browser) openPlay(appID int) {
	if b.mode == modeHome || b.mode == modeList || b.mode == modePanel {
		b.playBack = b.mode
	}
	b.mode, b.full = modePlay, true
	if b.play.appID != appID {
		b.play = playState{appID: appID}
	}
	if b.play.store == nil && b.play.storeErr == nil {
		b.wantStore = appID
	}
	if !b.play.installKnown {
		b.wantInstall = appID
	}
	// The achievement list on the right shares state with the full list.
	if b.ach.appID != appID {
		b.ach = achState{appID: appID}
		g, _ := b.playGame()
		if !hasAch(g) && !g.MayHaveAch {
			b.cache[appID] = nil // nothing to fetch
		}
		if list, ok := b.cache[appID]; ok {
			b.setAchievements(appID, list, nil)
		} else {
			b.ach.loading = true
			b.wantAch = appID
		}
	}
	if _, ok := b.cache[appID]; ok {
		b.wantAchIconsFor = appID
	}
	b.wantRunning = true
}

func (b *browser) playGame() (model.Game, bool) {
	for _, g := range b.games {
		if g.AppID == b.play.appID {
			return g, true
		}
	}
	return model.Game{}, false
}

func (b *browser) playKey(k string) {
	p := &b.play
	if k != "c" && k != "y" {
		p.confirmClose = false
	}
	if b.achNav(k, b.playListHeight()) {
		return
	}
	switch k {
	case "esc", "backspace":
		b.mode, b.full = b.playBack, true
	case "enter":
		b.gameBack = modePlay
		b.mode, b.full = modeGame, true
	case "p":
		switch {
		case b.opt.Demo:
			b.status = "demo mode: nothing to launch"
		case p.running:
			b.status = "already running"
		default:
			if err := launcher.Launch(p.appID); err != nil {
				b.status = "could not launch: " + err.Error()
				return
			}
			p.launching, p.launchedAt = true, time.Now()
			b.status = "launching through Steam..."
		}
	case "c", "y":
		if !p.running && !p.launching {
			b.status = "the game is not running"
			return
		}
		if !p.confirmClose && k == "c" {
			p.confirmClose = true
			b.status = "press c again to close the game"
			return
		}
		p.confirmClose = false
		p.closing = true
		b.wantClose = p.appID
		b.status = "closing..."
	case "r":
		if !p.refreshing {
			p.refreshing = true
			b.wantAch = p.appID
		}
	case "o":
		if err := launcher.Open(fmt.Sprintf("https://store.steampowered.com/app/%d", p.appID)); err == nil {
			b.status = "opened the store page"
		}
	case "f":
		if p.install.Installed && p.install.Dir != "" {
			if err := launcher.Open(p.install.Dir); err == nil {
				b.status = "opened the game folder"
			}
		}
	}
}

// selectApp moves the list cursor to a game, switching to the ALL tab if
// the current filter hides it.
func (b *browser) selectApp(appID int) {
	for i, gi := range b.view {
		if b.games[gi].AppID == appID {
			b.cursor = i
			b.clamp()
			return
		}
	}
	b.tab, b.query = 0, ""
	b.refilter()
	for i, gi := range b.view {
		if b.games[gi].AppID == appID {
			b.cursor = i
		}
	}
	b.clamp()
}

// applyRefresh records a fresh achievement list and reports new unlocks.
func (b *browser) applyRefresh(appID int, list []model.Achievement) {
	old := b.cache[appID]
	had := map[string]bool{}
	for _, a := range old {
		if a.Achieved {
			had[a.Name] = true
		}
	}
	var fresh []model.Achievement
	unlocked := 0
	for _, a := range list {
		if a.Achieved {
			unlocked++
			if old != nil && !had[a.Name] {
				fresh = append(fresh, a)
			}
		}
	}
	for i := range b.games {
		if b.games[i].AppID == appID && len(list) > 0 {
			b.games[i].Achieved, b.games[i].Total = unlocked, len(list)
			b.games[i].Percent = float64(unlocked) / float64(len(list)) * 100
		}
	}
	if b.play.appID == appID {
		b.play.session = append(b.play.session, fresh...)
		b.play.lastRefresh = time.Now()
		b.play.refreshing = false
	}
	if len(fresh) == 1 {
		b.status = "ACHIEVEMENT UNLOCKED: " + achName(fresh[0])
	} else if len(fresh) > 1 {
		b.status = fmt.Sprintf("%d ACHIEVEMENTS UNLOCKED", len(fresh))
	}
}

// --- view --------------------------------------------------------------------------------

// playListHeight is how many achievement rows fit in the game view.
func (b *browser) playListHeight() int { return max(b.h-4-9, 3) }

func (b *browser) buildPlay(f *frame) {
	p := &b.play
	g, _ := b.playGame()
	name := g.Name
	if p.store != nil && p.store.Name != "" {
		name = p.store.Name
	}

	// header: name left, run state and timer right
	var right string
	switch {
	case p.closing:
		right = chip(active.Warn, active.Ink, "CLOSING")
	case p.running:
		right = chip(active.Good, active.Ink, "RUNNING") + " " + bold(active.Text, clock(time.Since(p.startedAt)))
	case p.launching:
		right = chip(active.Warn, active.Ink, "STARTING") + " " + paint(active.Dim, spinnerFrames[b.frameN%len(spinnerFrames)])
	default:
		right = chip(lerp(active.Faint, active.Dim, 0.5), active.Text, "NOT RUNNING")
	}
	right += " "
	left := " " + paint(active.Dim, "esc ← ") + gradient(Truncate(name, b.w-width(right)-12))
	f.put(0, 0, left+strings.Repeat(" ", max(b.w-width(left)-width(right), 1))+right)
	f.put(1, 0, paint(active.Faint, strings.Repeat("─", b.w)))

	top, h := 2, b.h-4
	leftW := 0
	if b.w >= 110 {
		leftW = min(max(b.w*2/5, 46), 66)
	}
	if leftW > 0 {
		b.buildPlayInfo(f, top, h, leftW)
	}
	b.buildPlayAchievements(f, leftW, b.w-leftW, top, h)
}

// buildPlayInfo draws the left column: art, store details, install, progress.
func (b *browser) buildPlayInfo(f *frame, top, h, w int) {
	p := &b.play
	g, _ := b.playGame()
	inner := w - 4
	var lines []string
	artRows := 0
	if b.icons && p.store != nil && p.store.HeaderImage != "" {
		cols := inner
		pxW := cols * b.cellW
		pxH := pxW * 215 / 460
		rows := int(math.Ceil(float64(pxH) / float64(b.cellH)))
		if rows > h/3 {
			rows = h / 3
			pxH = rows * b.cellH
			pxW = pxH * 460 / 215
			cols = int(math.Ceil(float64(pxW) / float64(b.cellW)))
		}
		key := fmt.Sprintf("hdr%d@%dx%d", p.appID, pxW, pxH)
		if img, ok := b.big[key]; ok {
			if img != "" {
				f.icons = append(f.icons, iconSpot{row: top + 1, col: 2, w: cols, h: rows, key: key, sixel: img})
				artRows = rows + 1
			}
		} else {
			b.wantBig = &icons.Request{Key: key, URL: p.store.HeaderImage, W: pxW, H: pxH}
			artRows = rows + 1 // keep the space so the layout does not jump
		}
	}
	for i := 0; i < artRows; i++ {
		lines = append(lines, "")
	}
	if artRows == 0 {
		lines = append(lines, "")
	}
	kv := func(k, v string) {
		if v != "" {
			lines = append(lines, paint(active.Dim, padRight(k, 12))+paint(active.Text, Truncate(v, inner-12)))
		}
	}
	switch {
	case p.store != nil:
		for _, l := range wrap(p.store.Description, inner, 4) {
			lines = append(lines, paint(active.Text, l))
		}
		lines = append(lines, "")
		kv("developer", strings.Join(p.store.Developers, ", "))
		kv("publisher", strings.Join(p.store.Publishers, ", "))
		kv("released", p.store.Released)
		kv("genres", strings.Join(p.store.Genres, ", "))
		if p.store.Metacritic > 0 {
			c := active.Bad
			switch {
			case p.store.Metacritic >= 75:
				c = active.Good
			case p.store.Metacritic >= 50:
				c = active.Warn
			}
			lines = append(lines, paint(active.Dim, padRight("metacritic", 12))+chip(c, active.Ink, fmt.Sprint(p.store.Metacritic)))
		}
	case p.storeErr != nil:
		lines = append(lines, paint(active.Dim, "Store details unavailable: "+p.storeErr.Error()))
	default:
		lines = append(lines, paint(active.Accent, spinnerFrames[b.frameN%len(spinnerFrames)])+" "+paint(active.Dim, "Loading store details..."))
	}
	if p.steamKnown {
		if p.steamOn {
			lines = append(lines, paint(active.Dim, padRight("steam", 12))+paint(active.Good, "running in the background"))
		} else {
			lines = append(lines, paint(active.Dim, padRight("steam", 12))+paint(active.Dim, "closed, p starts it hidden in the tray"))
		}
	}
	switch {
	case !p.installKnown:
	case p.install.Installed:
		lines = append(lines, paint(active.Dim, padRight("installed", 12))+paint(active.Good, "yes")+"  "+
			paint(active.Text, launcher.FormatSize(p.install.SizeBytes)))
	default:
		lines = append(lines, paint(active.Dim, padRight("installed", 12))+paint(active.Warn, "no")+
			paint(active.Dim, "  p opens Steam's install dialog"))
	}
	lines = append(lines, "",
		gradient("YOUR PROGRESS"),
		Bar(g.Percent, inner),
		bold(pctColor(g.Percent), fmt.Sprintf("%.1f%%", g.Percent))+"  "+paint(active.Dim, fmt.Sprintf("%d / %d", g.Achieved, g.Total))+"  "+tierChip(g.Percent),
		paint(active.Dim, "played ")+paint(active.Text, formatPlaytime(g.Playtime))+paint(active.Dim, "   last ")+paint(active.Text, formatDate(g.LastPlayed)))
	if len(p.session) > 0 {
		lines = append(lines, "", gradient("THIS SESSION"))
		for _, a := range p.session {
			lines = append(lines, paint(active.Good, "■ ")+bold(active.Text, Truncate(achName(a), inner-2)))
		}
	}
	for r := 0; r < h; r++ {
		t := ""
		if r < len(lines) {
			t = lines[r]
		}
		if r >= 1 && r < artRows {
			continue // rows under the header art are left to the image
		}
		f.put(top+r, 0, "  "+padRight(t, w-2))
	}
}

// buildPlayAchievements draws the achievement box: tabs, list, and the
// description of the selected achievement.
func (b *browser) buildPlayAchievements(f *frame, x, w, top, h int) {
	a := &b.ach
	p := &b.play
	inner := w - 4
	border := func(s string) string { return paint(lerp(active.Faint, active.Dim, 0.45), s) }
	line := func(row int, t string) {
		f.put(row, x, border("│")+" "+padRight(t, inner)+" "+border("│"))
	}
	title := " ACHIEVEMENTS "
	f.put(top, x, border("╭─")+gradient(title)+border(strings.Repeat("─", max(w-3-width(title), 0))+"╮"))

	// tabs and sort
	unlocked := 0
	for _, x := range a.list {
		if x.Achieved {
			unlocked++
		}
	}
	counts := []int{len(a.list), len(a.list) - unlocked, unlocked}
	tabs := ""
	for i, t := range achTabs {
		label := fmt.Sprintf("%s %d", t, counts[i])
		if i == a.tab {
			tabs += chip(active.Accent, active.Ink, label) + " "
		} else {
			tabs += paint(active.Dim, " "+label+" ") + " "
		}
	}
	sortLabel := paint(active.Dim, "sort ") + paint(active.Text, achSorts[a.sort])
	line(top+1, tabs+strings.Repeat(" ", max(inner-width(tabs)-width(sortLabel), 1))+sortLabel)

	L := b.playListHeight()
	session := map[string]bool{}
	for _, s := range p.session {
		session[s.Name] = true
	}
	iconW := 0
	if b.icons {
		iconW = 3
	}
	nameW := inner - iconW - 2 - 8 - 6
	switch {
	case a.loading:
		for r := 0; r < L; r++ {
			t := ""
			if r == L/2 {
				t = paint(active.Accent, spinnerFrames[b.frameN%len(spinnerFrames)]) + " " + paint(active.Text, "Loading achievements...")
			}
			line(top+2+r, t)
		}
	case a.err != nil:
		for r := 0; r < L; r++ {
			t := ""
			if r == L/2 {
				t = chip(active.Bad, active.Ink, "ERROR") + " " + paint(active.Text, Truncate(a.err.Error(), inner-8))
			}
			line(top+2+r, t)
		}
	default:
		if len(a.list) == 0 {
			line(top+2, paint(active.Dim, "This game has no achievements."))
		}
		for r := 0; r < L; r++ {
			row := top + 2 + r
			i := a.offset + r
			if i >= len(a.view) {
				if !(len(a.list) == 0 && r == 0) {
					line(row, "")
				}
				continue
			}
			ac := a.list[a.view[i]]
			sel := i == a.cursor
			mark := paint(active.Faint, "□")
			nm := padRight(Truncate(achName(ac), nameW), nameW)
			name := paint(active.Dim, nm)
			if ac.Achieved {
				mark = paint(active.Good, "■")
				name = paint(active.Text, nm)
			}
			if sel {
				mark = paint(active.Accent, "▌")
				name = bold(active.To, nm)
			}
			extra := "      "
			if session[ac.Name] {
				extra = " " + chip(active.Good, active.Ink, "NEW")
			}
			rest := mark + " " + name + paint(active.Dim, padLeft(fmt.Sprintf("%.1f%%", ac.Global), 8)) + extra
			key := achIconKey(a.appID, ac)
			if img, ok := b.small[key]; ok && b.icons {
				f.put(row, x, border("│")+" ")
				f.icons = append(f.icons, iconSpot{row: row, col: x + 2, w: 2, h: 1, key: key, sixel: img})
				f.put(row, x+4, " "+padRight(rest, inner-3)+" "+border("│"))
			} else {
				line(row, strings.Repeat(" ", iconW)+rest)
			}
		}
	}

	// selected achievement details
	sep := top + 2 + L
	f.put(sep, x, border("├"+strings.Repeat("─", w-2)+"┤"))
	detail := make([]string, 5)
	if !a.loading && a.cursor < len(a.view) {
		ac := a.list[a.view[a.cursor]]
		head := bold(active.Text, Truncate(achName(ac), inner-14))
		if r := rarityChip(ac.Global); r != "" {
			head += "  " + r
		}
		detail[0] = head
		desc := ac.Description
		if desc == "" && ac.Hidden {
			desc = "Hidden achievement. Steam reveals the details once it is unlocked."
		}
		if desc == "" {
			desc = "No description."
		}
		for i, l := range wrap(desc, inner, 2) {
			detail[1+i] = paint(active.Text, l)
		}
		status := paint(active.Dim, "locked")
		if ac.Achieved {
			status = paint(active.Good, "unlocked")
			if ac.UnlockTime > 0 {
				status += paint(active.Dim, " "+time.Unix(ac.UnlockTime, 0).Format("2006-01-02 15:04"))
			}
		}
		if ac.Global > 0 {
			status += paint(active.Dim, fmt.Sprintf("   %.1f%% of players have it", ac.Global))
		}
		detail[4] = status
	}
	for i, t := range detail {
		line(sep+1+i, t)
	}
	f.put(top+h-1, x, border("╰"+strings.Repeat("─", w-2)+"╯"))
}

func sortByGlobalDesc(list []model.Achievement) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j].Global > list[j-1].Global; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}

func clock(d time.Duration) string {
	d = d.Round(time.Second)
	return fmt.Sprintf("%02d:%02d:%02d", int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60)
}
