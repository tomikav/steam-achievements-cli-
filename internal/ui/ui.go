// Package ui renders everything the user sees: banner, tables, bars, spinners.
package ui

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/tomikav/steam-achievements-cli-/internal/model"
)

// --- palette ------------------------------------------------------------------

var (
	styleTitle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#61afef"))
	styleSubtitle  = lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("#abb2bf"))
	styleHeader    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#56b6c2"))
	styleName      = lipgloss.NewStyle().Foreground(lipgloss.Color("#e6e6e6"))
	styleDim       = lipgloss.NewStyle().Foreground(lipgloss.Color("#5c6370"))
	styleHint      = lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("#5c6370"))
	styleError     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#e06c75"))
	styleOk        = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#98c379"))
	styleWarn      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#e5c07b"))
	purple         = lipgloss.NewStyle().Foreground(lipgloss.Color("#c678dd"))
	bannerFg       = []lipgloss.Color{"#e06c75", "#e5c07b", "#98c379", "#56b6c2", "#61afef", "#c678dd"}
)

// --- banner --------------------------------------------------------------------

const bannerArt = `  ███████╗███████╗ █████╗ ███╗   ███╗ ███████╗
  ██╔════╝██╔════╝██╔══██╗████╗ ████║ ██╔════╝
  ███████╗█████╗  ███████║██╔████╔██║ ███████╗
  ╚════██║██╔══╝  ██╔══██║██║╚██╔╝██║ ╚════██║
  ███████║███████╗██║  ██║██║ ╚═╝ ██║ ███████║
  ╚══════╝╚══════╝╚═╝  ╚═╝╚═╝     ╚═╝ ╚══════╝`

// Banner prints the gradient title card.
func Banner() string {
	lines := strings.Split(bannerArt, "\n")
	var b strings.Builder
	for i, line := range lines {
		color := bannerFg[i%len(bannerFg)]
		b.WriteString(lipgloss.NewStyle().Foreground(color).Render(line))
		b.WriteString("\n")
	}
	b.WriteString(styleSubtitle.Render("        ✦ achievements edition — flex your trophy cabinet ✦") + "\n")
	return b.String()
}

// --- progress bar ----------------------------------------------------------------

// Bar renders a coloured progress bar like ▐██████░░░▌ 71%.
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
	var style lipgloss.Style
	switch {
	case pct >= 99.5:
		style = purple
	case pct >= 80:
		style = lipgloss.NewStyle().Foreground(lipgloss.Color("#98c379"))
	case pct >= 50:
		style = lipgloss.NewStyle().Foreground(lipgloss.Color("#e5c07b"))
	case pct >= 25:
		style = lipgloss.NewStyle().Foreground(lipgloss.Color("#d19a66"))
	default:
		style = lipgloss.NewStyle().Foreground(lipgloss.Color("#e06c75"))
	}
	filledStr := strings.Repeat("█", filled)
	emptyStr := styleDim.Render(strings.Repeat("░", width-filled))
	return style.Render("▐"+filledStr) + emptyStr + style.Render("▌")
}

// PercentStyle colours a percentage string by completion tier.
func PercentStyle(pct float64) string {
	s := fmt.Sprintf("%5.1f%%", pct)
	switch {
	case pct >= 99.5:
		return purple.Render(s + " 👑")
	case pct >= 80:
		return styleOk.Render(s)
	case pct >= 50:
		return styleWarn.Render(s)
	default:
		return styleError.Render(s)
	}
}

// Medal returns the podium symbol for a rank.
func Medal(rank int) string {
	switch rank {
	case 1:
		return "🥇"
	case 2:
		return "🥈"
	case 3:
		return "🥉"
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#5c6370")).Render(fmt.Sprintf("%2d.", rank))
	}
}

// --- table -----------------------------------------------------------------------

// TableOptions tunes the rendered library table.
type TableOptions struct {
	Min           float64
	TotalGames    int // games scanned before filtering
	FetchedFrom   string
}

// RenderTable draws the filtered library as a flashy table.
func RenderTable(games []model.Game, opt TableOptions) string {
	var b strings.Builder
	b.WriteString("\n")

	if len(games) == 0 {
		b.WriteString(styleWarn.Render("  🕹️  No games matched your filters.") + "\n")
		b.WriteString(styleHint.Render("     Try lowering the threshold:  steam list --min 20") + "\n")
		return b.String()
	}

	header := styleHeader.Render(fmt.Sprintf(
		"  %-4s %-36s %-*s %-9s %-9s %-10s %-10s",
		"", "GAME", 22, "PROGRESS", "COMPLETE", "ACHIEVED", "PLAYTIME", "LAST PLAYED",
	))
	b.WriteString(header + "\n")

	for i, g := range games {
		name := Truncate(g.Name, 36)
		pt := formatPlaytime(g.Playtime)
		last := formatDate(g.LastPlayed)
		count := fmt.Sprintf("%3d/%-3d", g.Achieved, g.Total)
		row := fmt.Sprintf("  %-4s %s %s %-9s %-9s %-10s %-10s",
			Medal(i+1),
			styleName.Render(padRight(name, 36)),
			Bar(g.Percent, 22),
			PercentStyle(g.Percent),
			styleDim.Render(padRight(count, 9)),
			styleDim.Render(padRight(pt, 10)),
			styleDim.Render(padRight(last, 10)),
		)
		b.WriteString(row + "\n")
	}

	b.WriteString("\n")
	b.WriteString(styleHint.Render(fmt.Sprintf(
		"  ⭐ %d of %d games with ≥ %.0f%% achievements  ·  fetched from %s  ·  tip: steam list --help",
		len(games), opt.TotalGames, opt.Min, opt.FetchedFrom,
	)) + "\n")
	return b.String()
}

// RenderDetail draws the achievement list for a single game.
func RenderDetail(game model.Game, ach []model.Achievement, showAll bool) string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(styleTitle.Render("  🎮 "+game.Name) + "\n")
	b.WriteString("  " + Bar(game.Percent, 40) + " " + PercentStyle(game.Percent) + "\n")
	b.WriteString(styleDim.Render(fmt.Sprintf("  %d/%d achievements · %s played", game.Achieved, game.Total, formatPlaytime(game.Playtime))) + "\n\n")

	sort.Slice(ach, func(i, j int) bool {
		return ach[i].Achieved && !ach[j].Achieved
	})
	shown := 0
	for _, a := range ach {
		if !showAll && !a.Achieved && shown >= 40 {
			break
		}
		mark := styleError.Render("  ✘")
		if a.Achieved {
			mark = styleOk.Render("  ✔")
		}
		name := Truncate(a.DisplayName, 52)
		line := fmt.Sprintf("%s %s %s",
			mark,
			styleName.Render(padRight(name, 52)),
			styleDim.Render(fmt.Sprintf("%5.1f%% global", a.Global)),
		)
		b.WriteString(line + "\n")
		shown++
	}
	if !showAll && shown < len(ach) {
		b.WriteString(styleHint.Render(fmt.Sprintf("  … %d more locked achievements. Use --all to see everything.", len(ach)-shown)) + "\n")
	}
	return b.String()
}

// RenderStats draws the library-wide summary.
func RenderStats(s Stats) string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(styleTitle.Render("  📊 Library stats") + "\n\n")
	row := func(icon, label string, value string) {
		b.WriteString(fmt.Sprintf("  %-2s %-28s %s\n", icon, styleDim.Render(label), value))
	}
	row("🕹️", "Total games", styleName.Render(strconv.Itoa(s.TotalGames)))
	row("🏆", "Games with achievements", styleName.Render(strconv.Itoa(s.WithAchievements)))
	row("✅", "Games ≥ 50%", styleWarn.Render(strconv.Itoa(s.OverHalf)))
	row("👑", "Perfect games (100%)", purple.Render(strconv.Itoa(s.Perfect)))
	row("📈", "Average completion", styleName.Render(fmt.Sprintf("%.1f%%", s.Average)))
	row("⏱️", "Total playtime", styleName.Render(formatPlaytime(s.TotalPlaytime)))

	// vibe check meter
	b.WriteString("\n  " + styleSubtitle.Render("Achievement hunter rank: ") + RankBadge(s.Perfect, s.OverHalf, s.WithAchievements) + "\n")
	return b.String()
}

// Stats aggregates library-wide numbers.
type Stats struct {
	TotalGames      int
	WithAchievements int
	OverHalf        int
	Perfect         int
	Average         float64
	TotalPlaytime   int
}

// RankBadge assigns a silly-but-honest rank based on completion habits.
func RankBadge(perfect, overHalf, tracked int) string {
	switch {
	case tracked == 0:
		return styleDim.Render("🫥 Ghost (no achievements yet)")
	case perfect > 0 && float64(perfect)/float64(tracked) > 0.3:
		return purple.Render("👑 Perfectionist")
	case overHalf >= 10:
		return styleOk.Render("🏆 Achievement Hunter")
	case overHalf >= 3:
		return styleWarn.Render("🔥 Trophy Starter")
	default:
		return styleError.Render("🫠 Backlog Enjoyer")
	}
}

// Errorf prints a styled error to stderr.
func Errorf(format string, args ...any) {
	fmt.Fprintln(os.Stderr, styleError.Render("✖ error: "+fmt.Sprintf(format, args...)))
}

// Hint prints a dim suggestion line to stderr.
func Hint(format string, args ...any) {
	fmt.Fprintln(os.Stderr, styleHint.Render("ℹ "+fmt.Sprintf(format, args...)))
}

// Ok prints a green success line.
func Ok(msg string) {
	fmt.Println(styleOk.Render("✔ " + msg))
}

// --- spinner ----------------------------------------------------------------------

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Spinner shows animated progress on stderr while data downloads.
type Spinner struct {
	active atomic.Bool
	msg    atomic.Value // string
	mu     sync.Mutex
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
			time.Sleep(80 * time.Millisecond)
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
	time.Sleep(90 * time.Millisecond)
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
	return string(r[:max-1]) + "…"
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
		return "—"
	}
	return time.Unix(unix, 0).Format("2006-01-02")
}
