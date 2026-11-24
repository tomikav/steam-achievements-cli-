package ui

// Small charts made of block characters, shared by the dashboard, the
// `steam stats` command and the interactive views.

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/tomikav/steam-achievements-cli-/internal/model"
)

var eighths = []rune(" ▁▂▃▄▅▆▇█")

// sparkline draws values as one row of bars.
func sparkline(vals []int, from, to string) string {
	peak := 1
	for _, v := range vals {
		peak = max(peak, v)
	}
	var sb strings.Builder
	for i, v := range vals {
		lvl := int(math.Ceil(float64(v) / float64(peak) * 8 * animT))
		if v == 0 {
			sb.WriteString(paint(active.Faint, "▁"))
			continue
		}
		sb.WriteString(paint(lerp(from, to, float64(i)/float64(max(len(vals)-1, 1))), string(eighths[max(min(lvl, 8), 1)])))
	}
	return sb.String()
}

// meter is a small solid bar: frac of w cells filled.
func meter(frac float64, w int, color string) string {
	frac = math.Min(math.Max(frac, 0), 1) * animT
	cells := frac * float64(w)
	full := int(cells)
	s := paint(color, strings.Repeat("█", full))
	if full < w {
		part := int((cells - float64(full)) * 8)
		if part > 0 {
			s += paint(color, string([]rune("▏▎▍▌▋▊▉")[part-1]))
			full++
		}
		s += paint(active.Faint, strings.Repeat("░", w-full))
	}
	return s
}

type slice struct {
	label string
	n     int
	color string
}

// stackedBar splits one bar of w cells between the slices, with a legend.
func stackedBar(slices []slice, w int) []string {
	total := 0
	for _, s := range slices {
		total += s.n
	}
	if total == 0 {
		return nil
	}
	shown := int(math.Round(float64(w) * animT))
	var bar strings.Builder
	legend := []string{""}
	used := 0
	for i, s := range slices {
		if s.n == 0 {
			continue
		}
		cells := int(math.Round(float64(s.n) / float64(total) * float64(w)))
		if cells == 0 {
			cells = 1
		}
		if i == len(slices)-1 || used+cells > w {
			cells = w - used
		}
		vis := max(min(cells, shown-used), 0)
		bar.WriteString(paint(s.color, strings.Repeat("█", vis)))
		used += cells
		item := paint(s.color, "■") + " " + paint(active.Text, fmt.Sprint(s.n)) + " " + paint(active.Dim, s.label)
		if last := len(legend) - 1; legend[last] == "" {
			legend[last] = item
		} else if width(legend[last])+4+width(item) <= w {
			legend[last] += "    " + item
		} else {
			legend = append(legend, item)
		}
	}
	bar.WriteString(paint(active.Faint, strings.Repeat("░", max(w-min(used, shown), 0))))
	return append([]string{bar.String()}, legend...)
}

// columns draws a vertical bar chart, h rows tall, each bar cw cells wide.
func columns(vals []int, labels []string, h, cw int) []string {
	peak := 1
	for _, v := range vals {
		peak = max(peak, v)
	}
	rows := make([]string, 0, h+2)
	for r := h; r >= 1; r-- {
		var sb strings.Builder
		for i, v := range vals {
			lvl := float64(v) / float64(peak) * float64(h) * 8 * animT // in eighths
			cell := int(lvl) - (r-1)*8
			c := lerp(active.From, active.To, float64(i)/float64(max(len(vals)-1, 1)))
			switch {
			case cell >= 8:
				sb.WriteString(paint(c, strings.Repeat("█", cw-1)) + " ")
			case cell > 0:
				sb.WriteString(paint(c, strings.Repeat(string(eighths[cell]), cw-1)) + " ")
			case r == 1 && v > 0:
				sb.WriteString(paint(c, strings.Repeat("▁", cw-1)) + " ")
			case r == 1:
				sb.WriteString(paint(active.Faint, strings.Repeat("▁", cw-1)) + " ")
			default:
				sb.WriteString(strings.Repeat(" ", cw))
			}
		}
		rows = append(rows, sb.String())
	}
	var nums, names strings.Builder
	for i, v := range vals {
		n := ""
		if v > 0 {
			n = fmt.Sprint(countUp(v))
		}
		if cw < 4 { // too narrow for numbers
			n = ""
		}
		nums.WriteString(paint(active.Text, padRight(cut(n, cw-1), cw)))
		names.WriteString(paint(active.Dim, padRight(cut(labels[i], cw-1), cw)))
	}
	return append(rows, nums.String(), names.String())
}

// Monthly returns unlock counts for the last n months, oldest first.
func (a Activity) Monthly(n int) (vals []int, labels []string) {
	vals, labels = make([]int, n), make([]string, n)
	first := time.Date(a.now.Year(), a.now.Month(), 1, 0, 0, 0, 0, a.now.Location())
	idx := map[string]int{}
	for i := 0; i < n; i++ {
		m := first.AddDate(0, -(n - 1 - i), 0)
		idx[m.Format("2006-01")] = i
		labels[i] = m.Format("Jan")
	}
	for day, c := range a.byDay {
		if i, ok := idx[day[:7]]; ok {
			vals[i] += c
		}
	}
	return vals, labels
}

// Weekdays returns unlock counts per weekday over the last year, Monday first.
func (a Activity) Weekdays() [7]int {
	var out [7]int
	for i := 0; i < 365; i++ {
		d := a.now.AddDate(0, 0, -i)
		out[(int(d.Weekday())+6)%7] += a.byDay[dayKey(d)]
	}
	return out
}

// Daily returns unlock counts of the last n days, oldest first.
func (a Activity) Daily(n int) []int {
	out := make([]int, n)
	for i := 0; i < n; i++ {
		out[n-1-i] = a.byDay[dayKey(a.now.AddDate(0, 0, -i))]
	}
	return out
}

// libraryBar is the whole library in one bar, by completion state.
func libraryBar(games []model.Game, w int) []string {
	var perfect, progress, untouched, none int
	for _, g := range games {
		switch {
		case !g.HasStats || g.Total == 0:
			none++
		case g.Achieved >= g.Total:
			perfect++
		case g.Achieved > 0:
			progress++
		default:
			untouched++
		}
	}
	return stackedBar([]slice{
		{"perfect", perfect, active.Good},
		{"in progress", progress, active.From},
		{"not started", untouched, lerp(active.Dim, active.Faint, 0.3)},
		{"no achievements", none, lerp(active.Faint, active.Dim, 0.25)},
	}, w)
}

// playtimeChart lists the most played games as bars.
func playtimeChart(games []model.Game, n, barW int) []string {
	gs := append([]model.Game(nil), games...)
	sort.SliceStable(gs, func(i, j int) bool { return gs[i].Playtime > gs[j].Playtime })
	total := 0
	for _, g := range gs {
		total += g.Playtime
	}
	var out []string
	for i, g := range gs[:min(n, len(gs))] {
		if g.Playtime == 0 {
			break
		}
		c := lerp(active.From, active.To, float64(i)/float64(max(n-1, 1)))
		out = append(out, paint(active.Text, padRight(Truncate(g.Name, 26), 26))+"  "+
			meter(float64(g.Playtime)/float64(max(gs[0].Playtime, 1)), barW, c)+"  "+
			bold(active.Text, padLeft(formatPlaytime(g.Playtime), 7))+
			paint(active.Dim, fmt.Sprintf("  %2.0f%%", float64(g.Playtime)/float64(max(total, 1))*100)))
	}
	return out
}

// cut shortens s to w columns without an ellipsis.
func cut(s string, w int) string {
	r := []rune(s)
	for len(r) > 0 && width(string(r)) > w {
		r = r[:len(r)-1]
	}
	return string(r)
}
