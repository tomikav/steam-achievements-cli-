// Package ui renders everything the user sees: banner, tables, bars, spinners.
// It is theme-driven and deliberately emoji-free.
package ui

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/tomikav/steam-achievements-cli-/internal/model"
)

// Theme drives every colour the UI uses. Each theme is a single hue with a
// brightness ramp for the banner plus a few semantic accents.
type Theme struct {
	Name    string
	Banner  []lipgloss.Color // one shade per banner line
	Accent  lipgloss.Color
	Success lipgloss.Color
	Warn    lipgloss.Color
	Error   lipgloss.Color
	Dim     lipgloss.Color
	Text    lipgloss.Color
}

// Themes lists the built-in themes.
var Themes = map[string]Theme{
	"hacker": {
		Name:    "hacker",
		Banner:  []lipgloss.Color{"#0a3d1f", "#0c5b2b", "#0e7a37", "#11a345", "#17d457", "#2bff6f"},
		Accent:  "#2bff6f", Success: "#2bff6f", Warn: "#ffd75f", Error: "#ff5f5f",
		Dim: "#3d5c46", Text: "#d8e8dc",
	},
	"gamer": {
		Name:    "gamer",
		Banner:  []lipgloss.Color{"#3d0a14", "#5b0c1c", "#7a0e24", "#a31129", "#d41731", "#ff2e4d"},
		Accent:  "#ff2e4d", Success: "#ff2e4d", Warn: "#ffaa4d", Error: "#ff5f5f",
		Dim: "#5c4a52", Text: "#e8d8dc",
	},
	"steam": {
		Name:    "steam",
		Banner:  []lipgloss.Color{"#0a1a3d", "#0c2b5b", "#0e3d7a", "#1152a3", "#1768d4", "#2b8bff"},
		Accent:  "#2b8bff", Success: "#4dc3ff", Warn: "#ffd75f", Error: "#ff5f5f",
		Dim: "#3d4a5c", Text: "#d8e2ec",
	},
	"mono": {
		Name:    "mono",
		Banner:  []lipgloss.Color{"#2a2a2a", "#3a3a3a", "#4a4a4a", "#5a5a5a", "#7a7a7a", "#c8c8c8"},
		Accent:  "#e8e8e8", Success: "#e8e8e8", Warn: "#a8a8a8", Error: "#ffffff",
		Dim: "#5a5a5a", Text: "#e8e8e8",
	},
}

var active = Themes["hacker"]

var (
	styleTitle    lipgloss.Style
	styleSubtitle lipgloss.Style
	styleHeader   lipgloss.Style
	styleName     lipgloss.Style
	styleDim      lipgloss.Style
	styleHint     lipgloss.Style
	styleError    lipgloss.Style
	styleOk       lipgloss.Style
	styleWarn     lipgloss.Style
	styleRank     lipgloss.Style
)

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
	rebuild()
	return nil
}

// ActiveThemeName reports the current theme.
func ActiveThemeName() string { return active.Name }

func rebuild() {
	styleTitle = lipgloss.NewStyle().Bold(true).Foreground(active.Accent)
	styleSubtitle = lipgloss.NewStyle().Foreground(active.Dim)
	styleHeader = lipgloss.NewStyle().Bold(true).Foreground(active.Accent)
	styleName = lipgloss.NewStyle().Foreground(active.Text)
	styleDim = lipgloss.NewStyle().Foreground(active.Dim)
	styleHint = lipgloss.NewStyle().Foreground(active.Dim)
	styleError = lipgloss.NewStyle().Bold(true).Foreground(active.Error)
	styleOk = lipgloss.NewStyle().Bold(true).Foreground(active.Success)
	styleWarn = lipgloss.NewStyle().Bold(true).Foreground(active.Warn)
	styleRank = lipgloss.NewStyle().Bold(true).Foreground(active.Accent)
}

func init() { rebuild() }

// --- banner --------------------------------------------------------------------

// bannerArt is a hand-assembled ANSI-shadow "STEAM". Letters: S=8, T=9, E=8, A=8, M=11.
const bannerArt = `███████╗████████╗███████╗ █████╗ ███╗   ███╗
██╔════╝╚══██╔══╝██╔════╝██╔══██╗████╗ ████║
███████╗   ██╔══╝█████╗  ███████║██╔████╔██║
╚════██║   ██╔══╝██╔════╝██╔══██║██║╚██╔╝██║
███████║   ██║  ███████╗██║  ██║██║ ╚═╝ ██║
╚══════╝   ╚═╝  ╚══════╝╚═╝  ╚═╝╚═╝     ╚═╝`

// Banner prints the themed title card.
func Banner() string {
	lines := strings.Split(bannerArt, "\n")
	var b strings.Builder
	for i, line := range lines {
		color := active.Banner[i%len(active.Banner)]
		b.WriteString(lipgloss.NewStyle().Foreground(color).Render(line))
		b.WriteString("\n")
	}
	b.WriteString(styleSubtitle.Render("  [ achievements edition ]") + "\n")
	return b.String()
}

// --- progress bar ----------------------------------------------------------------

// Bar renders a progress bar whose colour follows the completion tier.
func Bar(pct float64, width int) string {
	if width < 3 {
		width = 3
	}
	filled := int(pct/100*float64(width) + 0.5)
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	var color lipgloss.Color
	switch {
	case pct >= 99.5:
		color = active.Accent
	case pct >= 80:
		color = active.Success
	case pct >= 50:
		color = active.Warn
	default:
		color = active.Error
	}
	style := lipgloss.NewStyle().Foreground(color)
	filledStr := strings.Repeat("#", filled)
	emptyStr := styleDim.Render(strings.Repeat(".", width-filled))
	return style.Render("["+filledStr) + emptyStr + style.Render("]")
}

// PercentStyle colours a percentage string by completion tier.
func PercentStyle(pct float64) string {
	s := fmt.Sprintf("%5.1f%%", pct)
	switch {
	case pct >= 99.5:
		return lipgloss.NewStyle().Bold(true).Foreground(active.Accent).Render(s + " DONE")
	case pct >= 80:
		return styleOk.Render(s)
	case pct >= 50:
		return styleWarn.Render(s)
	default:
		return styleError.Render(s)
	}
}

// Rank returns a right-aligned rank marker; the top three are accented.
func Rank(rank int) string {
	switch rank {
	case 1:
		return lipgloss.NewStyle().Bold(true).Foreground(active.Accent).Render(" 01.")
	case 2:
		return lipgloss.NewStyle().Bold(true).Foreground(active.Text).Render(" 02.")
	case 3:
		return lipgloss.NewStyle().Bold(true).Foreground(active.Text).Render(" 03.")
	default:
		return styleDim.Render(fmt.Sprintf(" %2d.", rank))
	}
}

// --- table -----------------------------------------------------------------------

// TableOptions tunes the rendered library table.
type TableOptions struct {
	Min         float64
	TotalGames  int
	FetchedFrom string
}

// RenderTable draws the filtered library.
func RenderTable(games []model.Game, opt TableOptions) string {
	var b strings.Builder
	b.WriteString("\n")

	if len(games) == 0 {
		b.WriteString(styleWarn.Render("  No games matched your filters.") + "\n")
		b.WriteString(styleHint.Render("  Try lowering the threshold: steam list --min 20") + "\n")
		return b.String()
	}

	header := styleHeader.Render(fmt.Sprintf(
		"  %-4s %-36s %-*s %-11s %-9s %-10s %-10s",
		"", "GAME", 22, "PROGRESS", "COMPLETE", "ACHIEVED", "PLAYTIME", "LAST PLAYED",
	))
	b.WriteString(header + "\n")

	for i, g := range games {
		name := Truncate(g.Name, 36)
		count := fmt.Sprintf("%3d/%-3d", g.Achieved, g.Total)
		row := fmt.Sprintf("  %s %s %s %-11s %-9s %-10s %-10s",
			Rank(i+1),
			styleName.Render(padRight(name, 36)),
			Bar(g.Percent, 22),
			PercentStyle(g.Percent),
			styleDim.Render(padRight(count, 9)),
			styleDim.Render(padRight(formatPlaytime(g.Playtime), 10)),
			styleDim.Render(padRight(formatDate(g.LastPlayed), 10)),
		)
		b.WriteString(row + "\n")
	}

	b.WriteString("\n")
	b.WriteString(styleHint.Render(fmt.Sprintf(
		"  %d of %d games with >= %.0f%% achievements  |  source: %s",
		len(games), opt.TotalGames, opt.Min, opt.FetchedFrom,
	)) + "\n")
	return b.String()
}

// RenderDetail draws the achievement list for a single game.
func RenderDetail(game model.Game, ach []model.Achievement, showAll bool) string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(styleTitle.Render("  "+game.Name) + "\n")
	b.WriteString("  " + Bar(game.Percent, 40) + " " + PercentStyle(game.Percent) + "\n")
	b.WriteString(styleDim.Render(fmt.Sprintf("  %d/%d achievements | %s played", game.Achieved, game.Total, formatPlaytime(game.Playtime))) + "\n\n")

	sort.Slice(ach, func(i, j int) bool {
		return ach[i].Achieved && !ach[j].Achieved
	})
	shown := 0
	for _, a := range ach {
		if !showAll && !a.Achieved && shown >= 40 {
			break
		}
		mark := styleDim.Render("[ ]")
		if a.Achieved {
			mark = styleOk.Render("[+]")
		}
		name := Truncate(a.DisplayName, 52)
		line := fmt.Sprintf("  %s %s %s",
			mark,
			styleName.Render(padRight(name, 52)),
			styleDim.Render(fmt.Sprintf("%5.1f%% global", a.Global)),
		)
		b.WriteString(line + "\n")
		shown++
	}
	if !showAll && shown < len(ach) {
		b.WriteString(styleHint.Render(fmt.Sprintf("  ... %d more locked achievements. Use --all to see everything.", len(ach)-shown)) + "\n")
	}
	return b.String()
}

// Stats aggregates library-wide numbers.
type Stats struct {
	TotalGames       int
	WithAchievements int
	OverHalf         int
	Perfect          int
	Average          float64
	TotalPlaytime    int
}

// RenderStats draws the library-wide summary.
func RenderStats(s Stats) string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(styleTitle.Render("  LIBRARY STATS") + "\n\n")
	row := func(label string, value string) {
		b.WriteString(fmt.Sprintf("  %-26s %s\n", styleDim.Render(label), value))
	}
	row("Total games", styleName.Render(strconv.Itoa(s.TotalGames)))
	row("With achievements", styleName.Render(strconv.Itoa(s.WithAchievements)))
	row("Over 50%", styleWarn.Render(strconv.Itoa(s.OverHalf)))
	row("Perfect (100%)", styleRank.Render(strconv.Itoa(s.Perfect)))
	row("Average completion", styleName.Render(fmt.Sprintf("%.1f%%", s.Average)))
	row("Total playtime", styleName.Render(formatPlaytime(s.TotalPlaytime)))

	b.WriteString("\n  " + styleSubtitle.Render("Rank: ") + RankBadge(s.Perfect, s.OverHalf, s.WithAchievements) + "\n")
	return b.String()
}

// RankBadge assigns an honest rank based on completion habits.
func RankBadge(perfect, overHalf, tracked int) string {
	switch {
	case tracked == 0:
		return styleDim.Render("NO DATA")
	case perfect > 0 && float64(perfect)/float64(tracked) > 0.3:
		return styleRank.Render("PERFECTIONIST")
	case overHalf >= 10:
		return styleRank.Render("ACHIEVEMENT HUNTER")
	case overHalf >= 3:
		return styleWarn.Render("TROPHY STARTER")
	default:
		return styleError.Render("BACKLOG ENJOYER")
	}
}

// Errorf prints a styled error to stderr.
func Errorf(format string, args ...any) {
	fmt.Fprintln(os.Stderr, styleError.Render("x "+fmt.Sprintf(format, args...)))
}

// Hint prints a dim note to stderr.
func Hint(format string, args ...any) {
	fmt.Fprintln(os.Stderr, styleHint.Render("- "+fmt.Sprintf(format, args...)))
}

// Ok prints a success line.
func Ok(msg string) {
	fmt.Println(styleOk.Render("+ " + msg))
}

// --- spinner ----------------------------------------------------------------------

var spinnerFrames = []string{"-", "\\", "|", "/"}

// Spinner shows animated progress on stderr while data downloads.
type Spinner struct {
	active atomic.Bool
	msg    atomic.Value // string
}

// StartSpinner begins animating unless stdout is not a terminal.
func StartSpinner(msg string) *Spinner {
	s := &Spinner{}
	s.msg.Store(msg)
	if !isTTY() {
		return s
	}
	s.active.Store(true)
	go func() {
		i := 0
		for s.active.Load() {
			fmt.Fprintf(os.Stderr, "\r\033[K%s %s", spinnerFrames[i%len(spinnerFrames)], s.msg.Load())
			i++
			time.Sleep(90 * time.Millisecond)
		}
	}()
	return s
}

// Update changes the spinner message.
func (s *Spinner) Update(msg string) {
	s.msg.Store(msg)
}

// Stop clears the spinner line.
func (s *Spinner) Stop() {
	s.active.Store(false)
	time.Sleep(100 * time.Millisecond)
	if isTTY() {
		fmt.Fprint(os.Stderr, "\r\033[K")
	}
}

func isTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// --- small helpers ------------------------------------------------------------------

// Truncate cuts s to max runes, appending an ellipsis when it does.
func Truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "..."
}

func padRight(s string, width int) string {
	r := []rune(s)
	if len(r) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(r))
}

func formatPlaytime(minutes int) string {
	return fmt.Sprintf("%.1f h", float64(minutes)/60)
}

func formatDate(unix int64) string {
	if unix <= 0 {
		return "-"
	}
	return time.Unix(unix, 0).Format("2006-01-02")
}
