package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/tomikav/steam-achievements-cli-/internal/model"
)

// Activity counts achievements unlocked per day across the library.
type Activity struct {
	byDay map[string]int
	now   time.Time
}

func dayKey(t time.Time) string { return t.Format("2006-01-02") }

// NewActivity collects every unlock time of every game.
func NewActivity(games []model.Game) Activity {
	a := Activity{byDay: map[string]int{}, now: time.Now()}
	for _, g := range games {
		for _, u := range g.Unlocks {
			a.byDay[dayKey(time.Unix(u, 0))]++
		}
	}
	return a
}

// Empty reports whether there is no unlock data at all (old cache).
func (a Activity) Empty() bool { return len(a.byDay) == 0 }

// Since counts unlocks in the last n days, today included.
func (a Activity) Since(days int) int {
	n := 0
	for i := 0; i < days; i++ {
		n += a.byDay[dayKey(a.now.AddDate(0, 0, -i))]
	}
	return n
}

// Streaks returns the current streak (days in a row up to today or
// yesterday) and the longest streak in the last year.
func (a Activity) Streaks() (current, longest int) {
	run := 0
	for i := 365; i >= 0; i-- {
		if a.byDay[dayKey(a.now.AddDate(0, 0, -i))] > 0 {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	start := 0
	if a.byDay[dayKey(a.now)] == 0 {
		start = 1 // a streak is still alive until the day is over
	}
	for i := start; a.byDay[dayKey(a.now.AddDate(0, 0, -i))] > 0; i++ {
		current++
	}
	return current, longest
}

// Heatmap renders the last `weeks` weeks as a calendar: one column per
// week, one row per weekday, darker to brighter by unlock count.
func (a Activity) Heatmap(weeks int) []string {
	// Columns end on the current week; rows run Monday to Sunday.
	wd := (int(a.now.Weekday()) + 6) % 7 // Monday = 0
	start := a.now.AddDate(0, 0, -wd-7*(weeks-1))
	peak := 1
	for i := 0; i < weeks*7; i++ {
		peak = max(peak, a.byDay[dayKey(start.AddDate(0, 0, i))])
	}
	level := func(n int) string {
		if n == 0 {
			return lerp(active.Faint, active.Dim, 0.15)
		}
		t := float64(n) / float64(peak)
		switch {
		case t > 0.75:
			return active.To
		case t > 0.5:
			return lerp(active.From, active.To, 0.5)
		case t > 0.25:
			return active.From
		}
		return lerp(active.Faint, active.From, 0.55)
	}
	// month labels
	months := []rune(strings.Repeat(" ", weeks*2))
	last, free := int(start.Month()), 0
	for w := 1; w < weeks; w++ {
		d := start.AddDate(0, 0, w*7)
		if int(d.Month()) != last && w*2 >= free && w*2+3 <= len(months) {
			copy(months[w*2:], []rune(d.Format("Jan")))
			free = w*2 + 4
		}
		last = int(d.Month())
	}
	rows := []string{"     " + paint(active.Dim, string(months))}
	labels := []string{"Mon", "", "Wed", "", "Fri", "", "Sun"}
	for r := 0; r < 7; r++ {
		var b strings.Builder
		b.WriteString(paint(active.Dim, fmt.Sprintf("%-4s ", labels[r])))
		for w := 0; w < weeks; w++ {
			d := start.AddDate(0, 0, w*7+r)
			if d.After(a.now) {
				b.WriteString("  ")
				continue
			}
			b.WriteString(paint(level(a.byDay[dayKey(d)]), "■") + " ")
		}
		rows = append(rows, b.String())
	}
	legend := paint(active.Dim, "less ")
	for _, n := range []int{0, 1, peak / 3, peak * 2 / 3, peak} {
		if n == 0 {
			legend += paint(level(0), "■") + " "
		} else {
			legend += paint(level(max(n, 1)), "■") + " "
		}
	}
	rows = append(rows, "     "+legend+paint(active.Dim, "more"))
	return rows
}
