// Package ui renders everything the user sees: banner, panels, tables,
// gradient bars and live progress. It is theme-driven and emoji-free: all
// decoration comes from colour, box drawing and block characters.
package ui

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"
	"github.com/mattn/go-runewidth"

	"github.com/tomikav/steam-achievements-cli-/internal/model"
)

// --- themes ---------------------------------------------------------------------

// Theme drives every colour the UI uses.
type Theme struct {
	Name   string
	From   string // gradient start (hex)
	To     string // gradient end (hex)
	Accent string
	Good   string
	Warn   string
	Bad    string
	Text   string
	Dim    string
	Faint  string // empty bar cells, borders
	Ink    string // text drawn on top of coloured chips
}

// Themes lists the built-in themes.
var Themes = map[string]Theme{
	"hacker": {Name: "hacker", From: "#00ff9c", To: "#00b3ff", Accent: "#00ff9c",
		Good: "#3dffa2", Warn: "#ffd166", Bad: "#ff5c7a", Text: "#e6fff4", Dim: "#6b8f80", Faint: "#23332c", Ink: "#04140d"},
	"gamer": {Name: "gamer", From: "#ff2e63", To: "#ffb000", Accent: "#ff2e63",
		Good: "#ffb000", Warn: "#ff8a3d", Bad: "#ff2e63", Text: "#fff0f3", Dim: "#9a7280", Faint: "#3a2229", Ink: "#1a0509"},
	// Steam's own palette: light blue on navy, blue-grey text, the green of
	// the client's Play button for good news. This is the default theme.
	"steam": {Name: "steam", From: "#66c0f4", To: "#1a9fff", Accent: "#66c0f4",
		Good: "#a4d007", Warn: "#e6b54a", Bad: "#e5534b", Text: "#c7d5e0", Dim: "#8f98a0", Faint: "#2a3f5a", Ink: "#0e141b"},
	"synthwave": {Name: "synthwave", From: "#ff4ecd", To: "#00e5ff", Accent: "#ff4ecd",
		Good: "#00e5ff", Warn: "#ffcc66", Bad: "#ff5c8a", Text: "#f5e9ff", Dim: "#9a86b8", Faint: "#2e2340", Ink: "#14081f"},
	"sunset": {Name: "sunset", From: "#ff6a5f", To: "#ffc47b", Accent: "#ff7e5f",
		Good: "#ffd166", Warn: "#ff9f43", Bad: "#ef476f", Text: "#fff1e6", Dim: "#b08b7a", Faint: "#3a2a24", Ink: "#1f0f08"},
	"frost": {Name: "frost", From: "#e0f7ff", To: "#6f9bff", Accent: "#8fd3ff",
		Good: "#a0f0ff", Warn: "#ffe08a", Bad: "#ff8fa3", Text: "#f0f8ff", Dim: "#8796a8", Faint: "#243041", Ink: "#0a1420"},
	"mono": {Name: "mono", From: "#ffffff", To: "#7a7a7a", Accent: "#ffffff",
		Good: "#e0e0e0", Warn: "#b0b0b0", Bad: "#909090", Text: "#eeeeee", Dim: "#8a8a8a", Faint: "#333333", Ink: "#000000"},
}

var active = Themes["steam"]

// Tier colours are shared by every theme except mono, so a "gold" game reads
// the same everywhere.
var tierColors = map[string][2]string{
	"PLATINUM": {"#e8f7ff", "#0b1a24"},
	"GOLD":     {"#ffc83d", "#241a00"},
	"SILVER":   {"#c3ccd6", "#14181d"},
	"BRONZE":   {"#d08a4e", "#1e1006"},
}

// SetTheme switches the active theme by name.
func SetTheme(name string) error {
	t, ok := Themes[name]
	if !ok {
		names := make([]string, 0, len(Themes))
		for n := range Themes {
			names = append(names, n)
		}
		sort.Strings(names)
		return fmt.Errorf("unknown theme %q (available: %s)", name, strings.Join(names, ", "))
	}
	active = t
	return nil
}

// ActiveThemeName reports the current theme.
func ActiveThemeName() string { return active.Name }

// --- colour primitives -------------------------------------------------------------

func fg(c string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(c)) }

func paint(c, s string) string { return fg(c).Render(s) }

func bold(c, s string) string { return fg(c).Bold(true).Render(s) }

// chip renders s as a coloured label with padding.
func chip(bg, ink, s string) string {
	return lipgloss.NewStyle().Background(lipgloss.Color(bg)).Foreground(lipgloss.Color(ink)).Bold(true).Render(" " + s + " ")
}

type rgb struct{ r, g, b float64 }

func parseHex(h string) rgb {
	var r, g, b int
	fmt.Sscanf(strings.TrimPrefix(h, "#"), "%02x%02x%02x", &r, &g, &b)
	return rgb{float64(r), float64(g), float64(b)}
}

func (c rgb) hex() string {
	cl := func(v float64) int { return int(math.Max(0, math.Min(255, math.Round(v)))) }
	return fmt.Sprintf("#%02x%02x%02x", cl(c.r), cl(c.g), cl(c.b))
}

func lerp(a, b string, t float64) string {
	x, y := parseHex(a), parseHex(b)
	return rgb{x.r + (y.r-x.r)*t, x.g + (y.g-x.g)*t, x.b + (y.b-x.b)*t}.hex()
}

// gradient colours each visible character of s along the theme gradient.
func gradient(s string) string { return gradientBetween(s, active.From, active.To) }

func gradientBetween(s, from, to string) string {
	runes := []rune(s)
	n := 0
	for _, r := range runes {
		if r != ' ' {
			n++
		}
	}
	var b strings.Builder
	i := 0
	for _, r := range runes {
		if r == ' ' {
			b.WriteRune(r)
			continue
		}
		t := 0.0
		if n > 1 {
			t = float64(i) / float64(n-1)
		}
		b.WriteString(fg(lerp(from, to, t)).Bold(true).Render(string(r)))
		i++
	}
	return b.String()
}

// width is the printed width of s, ignoring escape codes.
func width(s string) int { return lipgloss.Width(s) }

func padRight(s string, w int) string {
	if d := w - width(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

func padLeft(s string, w int) string {
	if d := w - width(s); d > 0 {
		return strings.Repeat(" ", d) + s
	}
	return s
}

// Truncate cuts s to at most max terminal cells, ending in "..." when it does.
func Truncate(s string, max int) string { return runewidth.Truncate(s, max, "...") }

// TermWidth returns the terminal width, or 120 when unknown.
func TermWidth() int {
	if w, _, err := term.GetSize(os.Stdout.Fd()); err == nil && w >= 40 {
		return w
	}
	return 120
}

// IsTTY reports whether stdout is a terminal.
func IsTTY() bool { return term.IsTerminal(os.Stdout.Fd()) }

// --- building blocks -----------------------------------------------------------------

// Bar draws a progress bar whose filled cells follow the theme gradient.
func Bar(pct float64, w int) string {
	if w < 4 {
		w = 4
	}
	cells := pct / 100 * float64(w)
	full := int(cells)
	if full > w {
		full = w
	}
	var b strings.Builder
	for i := 0; i < full; i++ {
		b.WriteString(paint(lerp(active.From, active.To, float64(i)/float64(max(w-1, 1))), "━"))
	}
	if full < w {
		// a half cell for partial progress makes small differences visible
		if cells-float64(full) >= 0.5 {
			b.WriteString(paint(lerp(active.From, active.To, float64(full)/float64(max(w-1, 1))), "╸"))
			full++
		}
		b.WriteString(paint(active.Faint, strings.Repeat("━", w-full)))
	}
	return b.String()
}

// tier names the completion bracket of a percentage.
func tier(pct float64) string {
	switch {
	case pct >= 99.95:
		return "PLATINUM"
	case pct >= 90:
		return "GOLD"
	case pct >= 75:
		return "SILVER"
	case pct >= 50:
		return "BRONZE"
	}
	return ""
}

func tierChip(pct float64) string {
	t := tier(pct)
	if t == "" {
		return ""
	}
	c := tierColors[t]
	if active.Name == "mono" {
		c = [2]string{active.Text, active.Ink}
	}
	return chip(c[0], c[1], padRight(t, 8))
}

func pctColor(pct float64) string {
	switch {
	case pct >= 99.95:
		return active.To
	case pct >= 75:
		return active.Good
	case pct >= 50:
		return active.Warn
	}
	return active.Bad
}

// box draws a rounded panel around lines, with an optional title in the top
// border. inner is the content width.
func box(title string, lines []string, inner int) string {
	border := func(s string) string { return paint(active.Faint, s) }
	var b strings.Builder
	top := "╭─"
	if title != "" {
		t := " " + title + " "
		b.WriteString(border(top) + gradient(t) + border(strings.Repeat("─", max(inner-width(t), 0))+"─╮") + "\n")
	} else {
		b.WriteString(border(top+strings.Repeat("─", inner)+"─╮") + "\n")
	}
	for _, l := range lines {
		b.WriteString(border("│") + " " + padRight(l, inner) + " " + border("│") + "\n")
	}
	b.WriteString(border("╰─"+strings.Repeat("─", inner)+"─╯") + "\n")
	return b.String()
}

// joinColumns places multi-line blocks side by side.
func joinColumns(gap int, blocks ...string) string {
	split := make([][]string, len(blocks))
	widths := make([]int, len(blocks))
	rows := 0
	for i, bl := range blocks {
		split[i] = strings.Split(strings.TrimRight(bl, "\n"), "\n")
		for _, l := range split[i] {
			widths[i] = max(widths[i], width(l))
		}
		rows = max(rows, len(split[i]))
	}
	var b strings.Builder
	for r := 0; r < rows; r++ {
		for i := range blocks {
			l := ""
			if r < len(split[i]) {
				l = split[i][r]
			}
			if i < len(blocks)-1 {
				l = padRight(l, widths[i]) + strings.Repeat(" ", gap)
			}
			b.WriteString(l)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func indent(s string, n int) string {
	pad := strings.Repeat(" ", n)
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i := range lines {
		lines[i] = pad + lines[i]
	}
	return strings.Join(lines, "\n") + "\n"
}

// --- banner --------------------------------------------------------------------------

const bannerArt = `███████╗████████╗███████╗ █████╗ ███╗   ███╗
██╔════╝╚══██╔══╝██╔════╝██╔══██╗████╗ ████║
███████╗   ██║   █████╗  ███████║██╔████╔██║
╚════██║   ██║   ██╔══╝  ██╔══██║██║╚██╔╝██║
███████║   ██║   ███████╗██║  ██║██║ ╚═╝ ██║
╚══════╝   ╚═╝   ╚══════╝╚═╝  ╚═╝╚═╝     ╚═╝`

// Banner renders the title card with a diagonal gradient.
func Banner() string {
	lines := strings.Split(bannerArt, "\n")
	var b strings.Builder
	b.WriteString("\n")
	for row, line := range lines {
		runes := []rune(line)
		b.WriteString("  ")
		for col, r := range runes {
			t := (float64(col)/float64(len(runes)) + float64(row)/float64(len(lines))*0.35) / 1.35
			c := lerp(active.From, active.To, t)
			if r == '█' {
				b.WriteString(paint(c, string(r)))
			} else if r != ' ' {
				b.WriteString(paint(lerp(c, active.Faint, 0.55), string(r)))
			} else {
				b.WriteRune(r)
			}
		}
		b.WriteString("\n")
	}
	b.WriteString("  " + paint(active.Dim, "A C H I E V E M E N T S") + "   " + paint(active.Faint, strings.Repeat("─", 18)) + "\n\n")
	return b.String()
}

// --- library table ----------------------------------------------------------------------

// TableOptions tunes the rendered library table.
type TableOptions struct {
	Min        float64
	Max        float64
	TotalGames int          // games considered (with achievements)
	Library    []model.Game // whole library, for the summary panel
	Owner      string
	SortBy     string
	Demo       bool
	// Icons maps AppID to a ready-to-print Sixel image (about 18 px tall).
	// When non-nil the table reserves an icon column.
	Icons map[int]string
}

// eol ends a table line. The explicit reset and erase-to-end-of-line stop
// a chip's background colour from bleeding across the rest of the row in
// terminals that apply the current background when scrolling.
func eol() string {
	if !IsTTY() || os.Getenv("NO_COLOR") != "" {
		return "\n"
	}
	return "\x1b[0m\x1b[K\n"
}

// Sixel images are drawn after the row text: save the cursor on the next
// line, step back up into the icon slot, draw, restore. The image never
// touches the bottom line, so drawing it cannot scroll the screen.
func placeIcon(img string, col int) string {
	return "\x1b7\x1b[1A\x1b[" + fmt.Sprint(col) + "G" + img + "\x1b8"
}

// RenderTable draws the summary panel and the filtered library.
func RenderTable(games []model.Game, opt TableOptions) string {
	var b strings.Builder
	tw := TermWidth()

	b.WriteString(summaryPanel(opt, len(games), tw))

	if len(games) == 0 {
		b.WriteString("\n  " + bold(active.Warn, "No games matched your filters.") + "\n")
		b.WriteString("  " + paint(active.Dim, "Try a lower threshold:") + " " + paint(active.Text, "steam list --min 20") + "\n\n")
		return b.String()
	}

	// Column layout adapts to the terminal width.
	showLast := tw >= 118
	showTime := tw >= 104
	barW := 24
	if tw < 96 {
		barW = 14
	}
	fixed := 2 + 3 + 2 + barW + 2 + 6 + 2 + 9 + 2 + 10
	iconW := 0
	if opt.Icons != nil {
		iconW = 4 // 2 cells for the image + 2 spacing
		fixed += iconW
	}
	if showTime {
		fixed += 9
	}
	if showLast {
		fixed += 12
	}
	nameW := min(max(tw-fixed-2, 18), 40)

	head := func(s string, w int) string { return bold(active.Dim, padRight(s, w)) }
	hdr := "  " + head("#", 3) + "  " + strings.Repeat(" ", iconW) + head("GAME", nameW) + "  " + head("PROGRESS", barW) + "  " +
		head("  DONE", 6) + "  " + head("UNLOCKED", 9)
	if showTime {
		hdr += "  " + head("PLAYTIME", 7)
	}
	if showLast {
		hdr += "  " + head("LAST PLAYED", 10)
	}
	hdr += "  " + head("TIER", 10)
	b.WriteString("\n" + hdr + "\n")
	b.WriteString("  " + paint(active.Faint, strings.Repeat("─", min(width(hdr)-2, tw-3))) + "\n")

	for i, g := range games {
		rank := fmt.Sprintf("%02d", i+1)
		if i < 3 {
			rank = gradientBetween(rank, active.From, active.To)
		} else {
			rank = paint(active.Dim, rank)
		}
		name := Truncate(g.Name, nameW)
		nameStyled := paint(active.Text, name)
		if g.Percent >= 99.95 {
			nameStyled = bold(active.Text, name)
		}
		row := "  " + padLeft(rank, 3) + "  " + strings.Repeat(" ", iconW) + padRight(nameStyled, nameW) + "  " + Bar(g.Percent, barW) + "  " +
			bold(pctColor(g.Percent), padLeft(fmt.Sprintf("%.0f%%", math.Floor(g.Percent)), 6)) + "  " +
			paint(active.Dim, padRight(fmt.Sprintf("%d/%d", g.Achieved, g.Total), 9))
		if showTime {
			row += "  " + paint(active.Dim, padLeft(formatPlaytime(g.Playtime), 7))
		}
		if showLast {
			row += "  " + paint(active.Dim, padRight(formatDate(g.LastPlayed), 10))
		}
		row += "  " + tierChip(g.Percent)
		b.WriteString(row + eol())
		if img, ok := opt.Icons[g.AppID]; ok {
			b.WriteString(placeIcon(img, 8)) // column after "  NN  "
		}
	}

	foot := fmt.Sprintf("%d of %d games at %.0f%% or more", len(games), opt.TotalGames, opt.Min)
	if opt.Max > 0 {
		foot = fmt.Sprintf("%d of %d games between %.0f%% and %.0f%%", len(games), opt.TotalGames, opt.Min, opt.Max)
	}
	if opt.SortBy != "" && opt.SortBy != "completion" {
		foot += ", sorted by " + opt.SortBy
	}
	b.WriteString("\n  " + paint(active.Dim, foot) + "\n\n")
	return b.String()
}

func summaryPanel(opt TableOptions, shown, tw int) string {
	lib := summarize(opt.Library)
	who := opt.Owner
	if opt.Demo {
		who = "demo library"
	}
	if who == "" {
		who = "your library"
	}
	stat := func(v, label string) string {
		return bold(active.Text, v) + " " + paint(active.Dim, label)
	}
	sep := paint(active.Faint, "  │  ")
	line1 := bold(active.Text, who) + paint(active.Dim, "  /  ") + paint(active.Dim, fmt.Sprintf("%s played", formatPlaytime(lib.playtime)))
	line2 := stat(fmt.Sprint(lib.total), "games") + sep +
		stat(fmt.Sprint(lib.tracked), "with achievements") + sep +
		stat(fmt.Sprint(lib.perfect), "perfect") + sep +
		stat(fmt.Sprintf("%.1f%%", lib.average), "average")
	inner := max(width(line1), width(line2))
	inner = min(max(inner, 60), tw-6)
	return indent(box("LIBRARY", []string{line1, line2}, inner), 2)
}

type libSummary struct {
	total, tracked, perfect, overHalf, playtime int
	average                                     float64
}

func summarize(games []model.Game) libSummary {
	var s libSummary
	sum := 0.0
	for _, g := range games {
		s.total++
		s.playtime += g.Playtime
		if !g.HasStats || g.Total == 0 {
			continue
		}
		s.tracked++
		p := float64(g.Achieved) / float64(g.Total) * 100
		sum += p
		if p >= 99.95 {
			s.perfect++
		}
		if p >= 50 {
			s.overHalf++
		}
	}
	if s.tracked > 0 {
		s.average = sum / float64(s.tracked)
	}
	return s
}

// --- detail view -------------------------------------------------------------------------

// RenderDetail draws the achievement checklist of one game.
func RenderDetail(game model.Game, ach []model.Achievement, showAll bool) string {
	var b strings.Builder
	tw := TermWidth()
	inner := min(max(70, width(game.Name)+4), tw-6)

	barW := inner - 22 // room for "  100.0%  " and a tier chip
	head := []string{
		bold(active.Text, Truncate(game.Name, inner)),
		Bar(game.Percent, barW) + "  " + bold(pctColor(game.Percent), padLeft(fmt.Sprintf("%.1f%%", game.Percent), 6)) + "  " + padRight(tierChip(game.Percent), 10),
		paint(active.Dim, fmt.Sprintf("%d of %d unlocked   %s played   last played %s   app %d",
			game.Achieved, game.Total, formatPlaytime(game.Playtime), formatDate(game.LastPlayed), game.AppID)),
	}
	b.WriteString("\n" + indent(box("GAME", head, inner), 2))

	var unlocked, locked []model.Achievement
	for _, a := range ach {
		if a.Achieved {
			unlocked = append(unlocked, a)
		} else {
			locked = append(locked, a)
		}
	}
	// Locked: easiest first (highest global unlock rate). Unlocked: rarest first.
	sort.SliceStable(locked, func(i, j int) bool { return locked[i].Global > locked[j].Global })
	sort.SliceStable(unlocked, func(i, j int) bool { return unlocked[i].Global < unlocked[j].Global })

	nameW := min(inner-24, 56)
	line := func(a model.Achievement) string {
		mark := paint(active.Faint, "□")
		name := paint(active.Dim, padRight(Truncate(a.DisplayName, nameW), nameW))
		if a.Achieved {
			mark = paint(active.Good, "■")
			name = paint(active.Text, padRight(Truncate(a.DisplayName, nameW), nameW))
		}
		return "    " + mark + "  " + name + "  " + rarity(a.Global)
	}

	if len(locked) > 0 {
		b.WriteString("\n  " + gradient("LOCKED") + paint(active.Dim, fmt.Sprintf("  %d left, easiest first", len(locked))) + "\n")
		limit := len(locked)
		if !showAll && limit > 15 {
			limit = 15
		}
		for _, a := range locked[:limit] {
			b.WriteString(line(a) + "\n")
		}
		if limit < len(locked) {
			b.WriteString("    " + paint(active.Dim, fmt.Sprintf("... %d more. Use --all to see everything.", len(locked)-limit)) + "\n")
		}
	}
	if len(unlocked) > 0 {
		b.WriteString("\n  " + gradient("UNLOCKED") + paint(active.Dim, fmt.Sprintf("  %d, rarest first", len(unlocked))) + "\n")
		limit := len(unlocked)
		if !showAll && limit > 10 {
			limit = 10
		}
		for _, a := range unlocked[:limit] {
			b.WriteString(line(a) + "\n")
		}
		if limit < len(unlocked) {
			b.WriteString("    " + paint(active.Dim, fmt.Sprintf("... %d more. Use --all to see everything.", len(unlocked)-limit)) + "\n")
		}
	}
	b.WriteString("\n")
	return b.String()
}

func rarity(global float64) string {
	if global <= 0 {
		return paint(active.Faint, "     -")
	}
	pct := paint(active.Dim, padLeft(fmt.Sprintf("%.1f%%", global), 6))
	switch {
	case global < 5:
		return pct + "  " + chip(tierColors["GOLD"][0], tierColors["GOLD"][1], padRight("ULTRA RARE", 10))
	case global < 15:
		return pct + "  " + chip(tierColors["SILVER"][0], tierColors["SILVER"][1], padRight("RARE", 10))
	}
	return pct
}

// --- stats view ------------------------------------------------------------------------------

// RenderStats draws the library-wide dashboard for the current terminal.
func RenderStats(games []model.Game, owner string) string {
	return renderStats(games, owner, TermWidth())
}

func renderStats(games []model.Game, owner string, tw int) string {
	s := summarize(games)
	var b strings.Builder

	card := func(label, value, sub string) string {
		return box("", []string{paint(active.Dim, label), gradient(value), paint(active.Dim, Truncate(sub, 20))}, 20)
	}
	cards := []string{
		card("GAMES", fmt.Sprint(s.total), formatPlaytime(s.playtime)+" played"),
		card("WITH ACHIEVEMENTS", fmt.Sprint(s.tracked), fmt.Sprintf("%d at 50%% or more", s.overHalf)),
		card("PERFECT", fmt.Sprint(s.perfect), "100% complete"),
		card("AVERAGE", fmt.Sprintf("%.1f%%", s.average), "completion"),
	}
	perRow := max(1, min(len(cards), (tw-2)/25))
	for i := 0; i < len(cards); i += perRow {
		b.WriteString(indent(joinColumns(1, cards[i:min(i+perRow, len(cards))]...), 2))
	}

	// Activity map: achievements unlocked per day.
	if act := NewActivity(games); !act.Empty() {
		cur, long := act.Streaks()
		b.WriteString("\n  " + gradient("ACTIVITY") + paint(active.Dim, fmt.Sprintf("  %d achievements in the last year   %d this month   streak %d %s, best %d",
			act.Since(365), act.Since(30), cur, plural(cur, "day", "days"), long)) + "\n")
		for _, l := range act.Heatmap(min(52, max((tw-12)/2, 8))) {
			b.WriteString("  " + l + "\n")
		}
	}

	// Completion distribution.
	var buckets [11]int
	for _, g := range games {
		if !g.HasStats || g.Total == 0 {
			continue
		}
		p := float64(g.Achieved) / float64(g.Total) * 100
		i := int(p / 10)
		if p >= 99.95 {
			i = 10
		} else if i > 9 {
			i = 9
		}
		buckets[i]++
	}
	peak := 1
	for _, n := range buckets {
		peak = max(peak, n)
	}
	b.WriteString("\n  " + gradient("COMPLETION") + paint(active.Dim, "  games per range") + "\n")
	for i := 10; i >= 0; i-- {
		label := fmt.Sprintf("%3d-%d%%", i*10, i*10+9)
		if i == 10 {
			label = "   100%"
		}
		w := int(math.Round(float64(buckets[i]) / float64(peak) * 40))
		if buckets[i] > 0 && w == 0 {
			w = 1
		}
		bar := ""
		for j := 0; j < w; j++ {
			bar += paint(lerp(active.From, active.To, float64(i)/10), "█")
		}
		b.WriteString("  " + paint(active.Dim, padLeft(label, 8)) + " " + paint(active.Faint, "│") + bar + " " + paint(active.Text, fmt.Sprint(buckets[i])) + "\n")
	}

	// Closest to 100%.
	var near []model.Game
	for _, g := range games {
		if g.HasStats && g.Total > 0 && g.Achieved < g.Total && g.Achieved > 0 {
			g.Percent = float64(g.Achieved) / float64(g.Total) * 100
			near = append(near, g)
		}
	}
	sort.SliceStable(near, func(i, j int) bool {
		li, lj := near[i].Total-near[i].Achieved, near[j].Total-near[j].Achieved
		if near[i].Percent != near[j].Percent {
			return near[i].Percent > near[j].Percent
		}
		return li < lj
	})
	if len(near) > 0 {
		b.WriteString("\n  " + gradient("CLOSEST TO 100%") + "\n")
		for _, g := range near[:min(5, len(near))] {
			left := g.Total - g.Achieved
			b.WriteString("  " + paint(active.Text, padRight(Truncate(g.Name, 34), 34)) + "  " + Bar(g.Percent, 20) + "  " +
				bold(pctColor(g.Percent), padLeft(fmt.Sprintf("%.0f%%", math.Floor(g.Percent)), 4)) + "  " +
				paint(active.Dim, fmt.Sprintf("%d left", left)) + "\n")
		}
	}

	b.WriteString("\n  " + paint(active.Dim, "RANK  ") + rankChip(s) + "\n\n")
	return b.String()
}

func rankChip(s libSummary) string {
	name := "BACKLOG ENJOYER"
	switch {
	case s.tracked == 0:
		name = "NO DATA"
	case s.perfect > 0 && float64(s.perfect)/float64(s.tracked) > 0.3:
		name = "PERFECTIONIST"
	case s.overHalf >= 10:
		name = "ACHIEVEMENT HUNTER"
	case s.overHalf >= 3:
		name = "TROPHY STARTER"
	}
	return chip(active.Accent, active.Ink, name)
}

// --- messages ----------------------------------------------------------------------------------

// Errorf prints a styled error to stderr.
func Errorf(format string, args ...any) {
	fmt.Fprintln(os.Stderr, "\n"+chip(active.Bad, active.Ink, "ERROR")+" "+bold(active.Text, fmt.Sprintf(format, args...)))
}

// Fix prints one indented line of remediation advice to stderr.
func Fix(format string, args ...any) {
	fmt.Fprintln(os.Stderr, "        "+paint(active.Dim, fmt.Sprintf(format, args...)))
}

// Hint prints a dim note to stderr.
func Hint(format string, args ...any) {
	fmt.Fprintln(os.Stderr, "  "+paint(active.Dim, fmt.Sprintf(format, args...)))
}

// Warnf prints a warning to stderr.
func Warnf(format string, args ...any) {
	fmt.Fprintln(os.Stderr, chip(active.Warn, active.Ink, "WARN ")+" "+paint(active.Text, fmt.Sprintf(format, args...)))
}

// Ok prints a success line.
func Ok(msg string) {
	fmt.Println(chip(active.Good, active.Ink, " OK  ") + " " + paint(active.Text, msg))
}

// Check prints one row of the doctor report. status: ok, fail, warn, skip.
func Check(label, status, detail string) {
	var tag string
	switch status {
	case "ok":
		tag = chip(active.Good, active.Ink, " OK ")
	case "fail":
		tag = chip(active.Bad, active.Ink, "FAIL")
	case "warn":
		tag = chip(active.Warn, active.Ink, "WARN")
	default:
		tag = paint(active.Dim, " SKIP ")
	}
	fmt.Printf("  %s  %s  %s\n", paint(active.Text, padRight(label, 14)), tag, paint(active.Dim, detail))
}

// Dim renders text in the theme's muted colour.
func Dim(s string) string { return paint(active.Dim, s) }

// Title renders text as a gradient heading.
func Title(s string) string { return gradient(s) }

// --- live progress -------------------------------------------------------------------------------

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Spinner shows animated progress on stderr. With Progress it becomes a
// gradient progress bar with rate and ETA.
type Spinner struct {
	running atomic.Bool
	msg     atomic.Value // string
	done    atomic.Int64
	total   atomic.Int64
	start   time.Time
	wg      sync.WaitGroup
}

// StartSpinner begins animating unless stderr is not a terminal.
func StartSpinner(msg string) *Spinner {
	s := &Spinner{start: time.Now()}
	s.msg.Store(msg)
	if !term.IsTerminal(os.Stderr.Fd()) {
		return s
	}
	s.running.Store(true)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for i := 0; s.running.Load(); i++ {
			fmt.Fprint(os.Stderr, "\r\033[K"+s.frame(i))
			time.Sleep(80 * time.Millisecond)
		}
	}()
	return s
}

func (s *Spinner) frame(i int) string {
	spin := paint(lerp(active.From, active.To, float64(i%len(spinnerFrames))/float64(len(spinnerFrames)-1)), spinnerFrames[i%len(spinnerFrames)])
	msg := s.msg.Load().(string)
	total := s.total.Load()
	if total <= 0 {
		return "  " + spin + " " + paint(active.Text, msg)
	}
	done := s.done.Load()
	pct := float64(done) / float64(total) * 100
	line := "  " + spin + " " + paint(active.Text, msg) + "  " + Bar(pct, 28) + "  " +
		bold(active.Text, fmt.Sprintf("%d/%d", done, total))
	if el := time.Since(s.start).Seconds(); el > 0.5 && done > 0 {
		rate := float64(done) / el
		eta := time.Duration(float64(total-done)/rate) * time.Second
		line += paint(active.Dim, fmt.Sprintf("  %.0f/s  eta %s", rate, eta.Round(time.Second)))
	}
	return line
}

// Update changes the spinner message.
func (s *Spinner) Update(msg string) { s.msg.Store(msg) }

// Progress switches the spinner to a progress bar.
func (s *Spinner) Progress(done, total int) {
	s.done.Store(int64(done))
	s.total.Store(int64(total))
}

// Stop clears the spinner line.
func (s *Spinner) Stop() {
	if s.running.Swap(false) {
		s.wg.Wait()
		fmt.Fprint(os.Stderr, "\r\033[K")
	}
}

// --- formatting helpers ------------------------------------------------------------------------

func formatPlaytime(minutes int) string {
	h := float64(minutes) / 60
	switch {
	case minutes == 0:
		return "0 h"
	case h < 10:
		return fmt.Sprintf("%.1f h", h)
	case h < 10000:
		return fmt.Sprintf("%.0f h", h)
	}
	return fmt.Sprintf("%.1fk h", h/1000)
}

func formatDate(unix int64) string {
	if unix <= 0 {
		return "never"
	}
	return time.Unix(unix, 0).Format("2006-01-02")
}
