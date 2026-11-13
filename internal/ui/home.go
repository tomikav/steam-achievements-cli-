package ui

// Home screen: the first thing `steam` shows. Banner, a greeting with the
// library summary, and three cards to jump straight into something:
// recently played games, games closest to 100%, and friends online.

import (
	"fmt"
	"math"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tomikav/steam-achievements-cli-/internal/model"
)

var homeCards = []string{"CONTINUE PLAYING", "ALMOST THERE", "FRIENDS ONLINE"}

type homeState struct {
	col int
	row [3]int
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

// Layout of the home screen. Generous spacing on purpose: one blank row of
// margin at the top, the banner, a blank row, the menu, a blank row, then
// cards with inner padding and a blank row between entries.
const (
	homeMargin = 2 // columns of margin left and right
	cardGap    = 3 // columns between cards
	cardPad    = 2 // spaces inside the card border
)

// entryRows is the height of one card entry: name, detail and one or two
// blank rows, depending on how tall the terminal is.
func (b *browser) entryRows() int {
	if b.h >= 46 {
		return 4
	}
	return 3
}

// homePerCard is how many entries fit in a card.
func (b *browser) homePerCard() int {
	inside := b.h - 2 - b.homeCardsTop() - 2 - 2 // footer, borders, top/bottom padding
	return max(1, (inside+1)/b.entryRows())
}

func (b *browser) homeCardsTop() int {
	if b.h >= 34 {
		return 13
	}
	return 4
}

func (b *browser) homeMenuRow() int { return b.homeCardsTop() - 2 }

func (b *browser) homeKey(k string) {
	hs := &b.home
	if k != "x" {
		b.perf.confirm = false
	}
	switch k {
	case "x":
		b.closeSteamKey()
		return
	case "right", "tab", "l":
		if k == "l" {
			b.mode, b.full = modeList, true
			return
		}
		hs.col = (hs.col + 1) % 3
	case "left", "shift+tab", "h":
		hs.col = (hs.col + 2) % 3
	case "down", "j", "wheeldown":
		hs.row[hs.col]++
	case "up", "k", "wheelup":
		hs.row[hs.col]--
	case "enter", "p":
		n := min(b.homeLen(hs.col), b.homePerCard())
		if n == 0 {
			return
		}
		i := hs.row[hs.col]
		if hs.col == 2 {
			f := b.homeFriends()[i]
			if f.ProfileURL != "" && openTarget(f.ProfileURL) == nil {
				b.status = "opened " + f.Name + "'s profile"
			}
			return
		}
		g := b.homeGames(hs.col)[i]
		b.openPlay(g.AppID)
		if k == "p" {
			b.playKey("p")
		}
		return
	case "d":
		b.back = modeHome
		b.mode, b.scroll, b.full = modeDashboard, 0, true
		return
	case "esc":
		return
	}
	n := min(b.homeLen(hs.col), b.homePerCard())
	if hs.row[hs.col] >= n {
		hs.row[hs.col] = n - 1
	}
	if hs.row[hs.col] < 0 {
		hs.row[hs.col] = 0
	}
}

func (b *browser) buildHome(f *frame) {
	s := summarize(b.opt.Games)
	top := b.homeCardsTop()

	if top > 4 {
		banner := strings.Split(strings.TrimRight(Banner(), "\n"), "\n")
		if len(banner) > 0 && banner[0] == "" {
			banner = banner[1:]
		}
		// banner from row 2 (one blank row of margin), indented like the cards
		for i := 0; i < 8 && i < len(banner); i++ {
			f.put(2+i, homeMargin-2, banner[i])
		}
		who := b.opt.Owner
		if b.opt.Demo {
			who = "demo player"
		}
		if who == "" {
			who = "player"
		}
		online := ""
		switch {
		case b.friends.loaded:
			online = paint(active.Good, "●") + "  " + paint(active.Text, fmt.Sprintf("%d friends online", len(b.homeFriends())))
		case b.friends.err != nil:
			online = paint(active.Faint, "●") + "  " + paint(active.Dim, "friends list is private")
		default:
			online = paint(active.Dim, spinnerFrames[b.frameN%len(spinnerFrames)]+"  checking friends")
		}
		stat := func(v, label string) string { return bold(active.Text, v) + " " + paint(active.Dim, label) }
		gap := "     "
		act := NewActivity(b.games)
		activity := paint(active.Dim, "activity map fills in after the next refresh")
		if !act.Empty() {
			cur, _ := act.Streaks()
			activity = stat(fmt.Sprint(act.Since(30)), "unlocked this month") + gap + stat(fmt.Sprint(cur), plural(cur, "day", "days")+" streak")
		}
		greet := []string{
			paint(active.Dim, "welcome back,"),
			gradient(who),
			stat(fmt.Sprint(len(b.games)), "games") + gap + stat(fmt.Sprint(s.tracked), "with achievements"),
			stat(fmt.Sprint(s.perfect), "perfect") + gap + stat(fmt.Sprintf("%.1f%%", s.average), "average") + gap + stat(formatPlaytime(s.playtime), "played"),
			activity,
			online,
			"",
			b.perfLine(),
		}
		x := homeMargin + 46 + 8 // banner width plus breathing room
		if b.w >= x+50 {
			for i, l := range greet {
				f.put(2+i, x, padRight(l, b.w-x-homeMargin))
			}
		}
	} else {
		f.put(1, homeMargin, gradient("STEAM ACHIEVEMENTS")+"   "+paint(active.Dim, b.opt.Owner))
	}

	// menu
	menu := []struct{ k, v string }{{"enter", "open"}, {"l", "library"}, {"a", "hunt"}, {"w", "deals"}, {"n", "news"}, {"u", "storage"},
		{"f", "friends"}, {"d", "stats"}, {"t", "theme"}, {"?", "help"}, {"q", "quit"}}
	line := ""
	for i, m := range menu {
		item := chip(lerp(active.Faint, active.Dim, 0.35), active.Text, m.k) + "  " + paint(active.Dim, m.v)
		if i > 0 {
			item = "    " + item
		}
		if width(line)+width(item) > b.w-2*homeMargin {
			break
		}
		line += item
	}
	f.put(b.homeMenuRow(), homeMargin, padRight(line, b.w-2*homeMargin))

	// cards
	cardW := (b.w - 2*homeMargin - 2*cardGap) / 3
	per := b.homePerCard()
	for c := 0; c < 3; c++ {
		b.buildHomeCard(f, c, homeMargin+c*(cardW+cardGap), top, cardW, per)
	}
}

func (b *browser) buildHomeCard(f *frame, card, x, top, w, per int) {
	hs := &b.home
	focused := hs.col == card
	bc := lerp(active.Faint, active.Dim, 0.45)
	if focused {
		bc = active.Accent
	}
	border := func(s string) string { return paint(bc, s) }
	title := " " + homeCards[card] + " "
	t := paint(active.Dim, title)
	if focused {
		t = gradient(title)
	}
	if width(title) > w-3 {
		title = " " + Truncate(homeCards[card], max(w-5, 3)) + " "
		t = paint(active.Dim, title)
		if focused {
			t = gradient(title)
		}
	}
	f.put(top, x, border("╭─")+t+border(strings.Repeat("─", max(w-3-width(title), 0))+"╮"))
	inner := w - 2 - 2*cardPad
	pad := strings.Repeat(" ", cardPad)
	rows := per*b.entryRows() + 1 // +1: blank row of padding at the top
	put := func(r int, s string) {
		f.put(top+1+r, x, border("│")+pad+padRight(s, inner)+pad+border("│"))
	}
	// entry r starts at inner row 1 + r*entryRows
	iconRow := func(r int, key, text string) {
		if img, ok := b.small[key]; ok && b.icons {
			col := x + 1 + cardPad
			f.put(top+1+r, x, border("│")+pad)
			f.icons = append(f.icons, iconSpot{row: top + 1 + r, col: col, w: 2, h: 1, key: key, sixel: img})
			f.put(top+1+r, col+2, "  "+padRight(text, inner-4)+pad+border("│"))
			return
		}
		put(r, "    "+text)
	}
	blankEntry := func(i int) {
		for k := 0; k < b.entryRows(); k++ {
			put(1+i*b.entryRows()+k, "")
		}
	}
	message := func(msgs ...string) {
		for r := 1; r < rows; r++ {
			if r-1 < len(msgs) {
				put(r, paint(active.Dim, msgs[r-1]))
			} else {
				put(r, "")
			}
		}
	}
	put(0, "")

	switch card {
	case 0, 1:
		games := b.homeGames(card)
		if len(games) == 0 {
			if card == 0 {
				message("Nothing played yet.")
			} else {
				message("No games in progress.")
			}
			break
		}
		for i := 0; i < per; i++ {
			if i >= len(games) {
				blankEntry(i)
				continue
			}
			g := games[i]
			sel := focused && hs.row[card] == i
			nameW := inner - 4
			name := paint(active.Text, Truncate(g.Name, nameW))
			if sel {
				name = bold(active.To, Truncate(g.Name, nameW))
			}
			base := 1 + i*b.entryRows()
			iconRow(base, "g"+strconv.Itoa(g.AppID), name)
			var detail string
			room := inner - 4 // after the marker
			pct := bold(pctColor(g.Percent), fmt.Sprintf("%.0f%%", math.Floor(g.Percent)))
			if card == 0 {
				played := Truncate("played "+ago(time.Unix(g.LastPlayed, 0)), room)
				detail = paint(active.Dim, played)
				if hasAch(g) && width(played)+9 <= room {
					detail += "     " + pct
				}
			} else {
				left := fmt.Sprintf("   %d left", g.Total-g.Achieved)
				text := width(pct) + len(left)
				switch {
				case room >= text+3+8:
					detail = Bar(g.Percent, min(16, room-text-3)) + "   " + pct + paint(active.Dim, left)
				case room >= text:
					detail = pct + paint(active.Dim, left)
				default:
					detail = pct
				}
			}
			marker := "    "
			if sel {
				marker = paint(active.Accent, "▌") + "   "
			}
			put(base+1, marker+detail)
			for k := 2; k < b.entryRows(); k++ {
				put(base+k, "")
			}
		}
	case 2:
		fr := b.homeFriends()
		switch {
		case b.friends.err != nil:
			message("Friends list is private.", "", "Set it to Public in Steam's", "privacy settings to see it.")
		case !b.friends.loaded:
			message(spinnerFrames[b.frameN%len(spinnerFrames)] + "  loading friends...")
		case len(fr) == 0:
			message("Nobody online right now.")
		default:
			for i := 0; i < per; i++ {
				if i >= len(fr) {
					blankEntry(i)
					continue
				}
				x := fr[i]
				sel := focused && hs.row[2] == i
				name := paint(active.Text, Truncate(x.Name, inner-4))
				if sel {
					name = bold(active.To, Truncate(x.Name, inner-4))
				}
				base := 1 + i*b.entryRows()
				iconRow(base, "f"+x.SteamID, name)
				detail := friendState(x)
				if x.Game != "" {
					detail = paint(active.Accent, "●") + "  " + bold(active.Text, Truncate(x.Game, max(inner-8, 4)))
				}
				marker := "    "
				if sel {
					marker = paint(active.Accent, "▌") + "   "
				}
				put(base+1, marker+detail)
				for k := 2; k < b.entryRows(); k++ {
					put(base+k, "")
				}
			}
		}
	}
	f.put(top+1+rows, x, border("╰"+strings.Repeat("─", w-2)+"╯"))
}

// perfLine compares the Steam client's memory with this CLI's.
func (b *browser) perfLine() string {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	cli := paint(active.Dim, "this CLI ") + bold(active.Good, mb(int64(m.Sys)))
	p := &b.perf
	switch {
	case p.confirm:
		return chip(active.Bad, active.Ink, "PRESS X AGAIN TO CLOSE STEAM") + "  " + paint(active.Dim, "games still launch from here")
	case p.closing:
		return paint(active.Accent, spinnerFrames[b.frameN%len(spinnerFrames)]) + paint(active.Dim, "  closing the Steam client...")
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
