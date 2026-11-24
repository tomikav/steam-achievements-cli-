package ui

// Versus (`v` on a friend): you and a friend side by side. Games you both
// own, hours each, achievement progress each, and who leads.

import (
	"fmt"
	"strings"

	"github.com/tomikav/steam-achievements-cli-/internal/model"
)

// VersusGame is one game both players own. Percentages are -1 when unknown.
type VersusGame struct {
	AppID    int
	Name     string
	Mine     int // minutes
	Theirs   int
	MyPct    float64
	TheirPct float64
}

// Versus is the comparison with one friend.
type Versus struct {
	Games      []VersusGame
	TheirCount int // games in their library
}

type versusState struct {
	friend  model.Friend
	loading bool
	err     error
	data    Versus
	cursor  int
	offset  int
	back    browseMode
	want    bool
}

func (b *browser) openVersus(f model.Friend) {
	vs := &b.versus
	if b.mode != modeVersus {
		vs.back = b.mode
	}
	b.mode, b.full = modeVersus, true
	if vs.friend.SteamID == f.SteamID && (vs.loading || vs.err == nil && len(vs.data.Games) > 0) {
		return
	}
	vs.friend, vs.data, vs.err, vs.cursor, vs.offset = f, Versus{}, nil, 0, 0
	if b.opt.LoadVersus == nil {
		vs.err = fmt.Errorf("comparing is not available here")
		return
	}
	vs.loading, vs.want = true, true
}

func (b *browser) versusIO(events chan<- func()) {
	vs := &b.versus
	if !vs.want || b.opt.LoadVersus == nil {
		return
	}
	vs.want = false
	f := vs.friend
	go func() {
		d, err := b.opt.LoadVersus(f)
		events <- func() {
			if vs.friend.SteamID == f.SteamID {
				vs.loading, vs.data, vs.err = false, d, err
			}
		}
	}()
}

func (b *browser) versusPer() int { return max(1, (b.h-12)/2) }

func (b *browser) versusKey(k string) {
	vs := &b.versus
	switch k {
	case "esc", "backspace", "v":
		b.mode, b.full = vs.back, true
		return
	case "down", "j", "wheeldown":
		vs.cursor++
	case "up", "k", "wheelup":
		vs.cursor--
	case "pgdown":
		vs.cursor += b.versusPer()
	case "pgup":
		vs.cursor -= b.versusPer()
	case "enter":
		if vs.cursor < len(vs.data.Games) {
			b.openPlay(vs.data.Games[vs.cursor].AppID)
			return
		}
	case "R":
		vs.data, vs.loading, vs.want, vs.err = Versus{}, true, true, nil
	}
	clampScroll(&vs.cursor, &vs.offset, len(vs.data.Games), b.versusPer())
}

// score counts the shared games where each side has the higher completion.
func (v Versus) score() (mine, theirs, hoursMine, hoursTheirs int) {
	for _, g := range v.Games {
		hoursMine += g.Mine
		hoursTheirs += g.Theirs
		if g.MyPct < 0 || g.TheirPct < 0 {
			continue
		}
		switch {
		case g.MyPct > g.TheirPct:
			mine++
		case g.TheirPct > g.MyPct:
			theirs++
		}
	}
	return mine, theirs, hoursMine / 60, hoursTheirs / 60
}

func (b *browser) buildVersus(f *frame) {
	vs := &b.versus
	b.header(f, paint(active.Dim, "VERSUS"))
	for r := 1; r < b.h-2; r++ {
		f.put(r, 0, strings.Repeat(" ", b.w))
	}
	me := b.opt.Owner
	if me == "" {
		me = "you"
	}
	// title: ME  VS  THEM
	left := bigText(fitBigText(me, b.w/2-12), active.From, active.From, animT)
	mid := bigText("VS", active.Dim, active.Dim, 1)
	right := bigText(fitBigText(vs.friend.Name, b.w/2-12), active.Good, active.Good, animT)
	for i := 0; i < 3; i++ {
		f.put(2+i, max(b.w/2-9-width(left[i]), 1), left[i])
		f.put(2+i, b.w/2-5, mid[i])
		f.put(2+i, b.w/2+9, right[i])
	}
	center := func(row int, msg string) { f.put(row, max((b.w-width(msg))/2, 0), msg) }
	switch {
	case vs.err != nil:
		center(b.h/2, chip(active.Bad, active.Ink, "ERROR")+" "+paint(active.Text, vs.err.Error()))
		center(b.h/2+2, paint(active.Dim, "Their game details must be public. R to retry."))
		return
	case vs.loading:
		center(b.h/2, paint(active.Accent, b.spin())+" "+paint(active.Text, "Comparing your libraries..."))
		return
	case len(vs.data.Games) == 0:
		center(b.h/2, paint(active.Dim, "You have no games in common yet."))
		return
	}
	d := vs.data
	m, t, hm, ht := d.score()
	sum := bold(active.From, fmt.Sprint(countUp(m))) + paint(active.Dim, " games ahead") + "      " +
		paint(active.Dim, fmt.Sprintf("%d games in common  ·  %d h vs %d h", len(d.Games), hm, ht)) + "      " +
		bold(active.Good, fmt.Sprint(countUp(t))) + paint(active.Dim, " games ahead")
	center(6, sum)

	barW := max((b.w-66)/2, 4)
	per := b.versusPer()
	clampScroll(&vs.cursor, &vs.offset, len(d.Games), per)
	peak := 1
	for _, g := range d.Games {
		peak = max(peak, max(g.Mine, g.Theirs))
	}
	row := 8
	for i := 0; i < per && vs.offset+i < len(d.Games); i++ {
		idx := vs.offset + i
		g := d.Games[idx]
		sel := idx == vs.cursor
		name := Truncate(g.Name, 30)
		nm := paint(active.Text, padRight(name, 30))
		if sel {
			nm = bold(active.To, padRight(name, 30))
		}
		// mirrored hour bars around the name
		lw := int(float64(g.Mine) / float64(peak) * float64(barW) * animT)
		rw := int(float64(g.Theirs) / float64(peak) * float64(barW) * animT)
		lbar := strings.Repeat(" ", barW-lw) + paint(active.From, strings.Repeat("█", lw))
		rbar := paint(active.Good, strings.Repeat("█", rw)) + strings.Repeat(" ", barW-rw)
		pct := func(p float64, win bool, c string) string {
			if p < 0 {
				return paint(active.Faint, "   --")
			}
			s := fmt.Sprintf("%4.0f%%", p)
			if win {
				return bold(c, s)
			}
			return paint(active.Dim, s)
		}
		lp := pct(g.MyPct, g.MyPct > g.TheirPct, active.From)
		rp := pct(g.TheirPct, g.TheirPct > g.MyPct, active.Good)
		marker := "  "
		if sel {
			marker = paint(pulse(active.Accent, active.To), "▌") + " "
		}
		line := marker + paint(active.Dim, padLeft(formatPlaytime(g.Mine), 7)) + " " + lbar + " " + lp + "  " + nm + "  " + rp + " " + rbar + " " +
			paint(active.Dim, formatPlaytime(g.Theirs))
		f.put(row+i*2, 0, truncateANSI(line, b.w))
	}
}
